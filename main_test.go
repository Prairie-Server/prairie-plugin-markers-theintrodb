package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/structpb"

	pluginv1 "github.com/Silo-Server/silo-plugin-sdk/pkg/pluginproto/silo/plugin/v1"
	"github.com/Silo-Server/silo-plugin-markers-introdb/provider"
)

func testMarkerServer(t *testing.T, handler http.HandlerFunc, apiKey string) *markerServer {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	client := provider.NewClient(apiKey)
	client.SetBaseURL(srv.URL)
	t.Cleanup(client.Close)
	return &markerServer{runtime: &runtimeServer{client: client, provider: provider.NewProvider(client)}}
}

func TestMarkerServerFetchMarkersMapsSegments(t *testing.T) {
	server := testMarkerServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{
			"type":"episode",
			"intro":[{"end_ms":60000,"confidence":0.8,"submission_count":4}],
			"credits":[{"start_ms":1500000,"confidence":0.7,"submission_count":3}],
			"recap":[{"end_ms":45000,"confidence":0.6,"submission_count":2}],
			"preview":[{"start_ms":300000,"end_ms":330000,"confidence":0.5,"submission_count":1}]
		}`))
	}, "")

	resp, err := server.FetchMarkers(context.Background(), &pluginv1.FetchMarkersRequest{
		ItemType: "episode",
		ExternalIds: &pluginv1.MarkerExternalIDs{
			TmdbId: "123",
		},
		SeasonNumber:    1,
		EpisodeNumber:   2,
		DurationSeconds: 1800,
	})
	if err != nil {
		t.Fatalf("FetchMarkers: %v", err)
	}
	if len(resp.GetMarkers()) != 4 {
		t.Fatalf("markers = %d, want 4", len(resp.GetMarkers()))
	}
	got := resp.GetMarkers()[0]
	if got.GetSegment() != "intro" || got.GetEndSeconds() != 60 || got.GetConfidence() != 0.8 || got.GetSubmissionCount() != 4 {
		t.Fatalf("intro marker = %+v", got)
	}
	if got.GetAlgorithm() != provider.Algorithm {
		t.Fatalf("algorithm = %q, want %q", got.GetAlgorithm(), provider.Algorithm)
	}
}

func TestRuntimeConfigureSetsAPIKeyForStats(t *testing.T) {
	var gotAuth string
	server := testMarkerServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"total":1}`))
	}, "")
	account, err := structpb.NewStruct(map[string]any{"api_key": "secret-key"})
	if err != nil {
		t.Fatalf("NewStruct: %v", err)
	}
	if _, err := server.runtime.Configure(context.Background(), &pluginv1.ConfigureRequest{
		Config: []*pluginv1.ConfigEntry{{Key: "account", Value: account}},
	}); err != nil {
		t.Fatalf("Configure: %v", err)
	}
	if _, err := server.GetMarkerProviderStats(context.Background(), &pluginv1.GetMarkerProviderStatsRequest{}); err != nil {
		t.Fatalf("GetMarkerProviderStats: %v", err)
	}
	if gotAuth != "Bearer secret-key" {
		t.Fatalf("Authorization = %q, want Bearer secret-key", gotAuth)
	}
}

func TestMarkerServerSubmitRateLimitMapsRetryInfo(t *testing.T) {
	server := testMarkerServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-UsageLimit-Reset", "90")
		w.WriteHeader(http.StatusTooManyRequests)
	}, "secret-key")

	_, err := server.SubmitMarker(context.Background(), &pluginv1.SubmitMarkerRequest{
		ItemType:        "episode",
		ExternalIds:     &pluginv1.MarkerExternalIDs{TmdbId: "123"},
		SeasonNumber:    1,
		EpisodeNumber:   2,
		Segment:         "intro",
		DurationSeconds: 1800,
		EndSeconds:      floatPtr(60),
	})
	if err == nil {
		t.Fatal("expected rate limit error")
	}
	st, ok := status.FromError(err)
	if !ok || st.Code() != codes.ResourceExhausted {
		t.Fatalf("status = %v/%v, want ResourceExhausted", st.Code(), err)
	}
	for _, detail := range st.Details() {
		if retry, ok := detail.(*errdetails.RetryInfo); ok {
			if retry.GetRetryDelay().AsDuration().Seconds() != 90 {
				t.Fatalf("retry delay = %v, want 90s", retry.GetRetryDelay().AsDuration())
			}
			return
		}
	}
	t.Fatal("missing RetryInfo detail")
}

func floatPtr(v float64) *float64 { return &v }
