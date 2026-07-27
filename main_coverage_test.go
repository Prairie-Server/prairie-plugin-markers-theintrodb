package main

import (
	"context"
	"net/http"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/structpb"

	pluginv1 "github.com/prairie-server/prairie-plugin-sdk/pkg/pluginproto/prairie/plugin/v1"

	"github.com/prairie-server/prairie-plugin-markers-theintrodb/provider"
)

func TestGetManifestAndLoadManifest(t *testing.T) {
	prev := version
	version = "1.2.3-test"
	t.Cleanup(func() { version = prev })

	m, err := loadManifest()
	if err != nil {
		t.Fatalf("loadManifest: %v", err)
	}
	if m.GetVersion() != "1.2.3-test" {
		t.Fatalf("version = %q", m.GetVersion())
	}
	if m.GetChecksum() == "" {
		t.Fatal("expected checksum")
	}
	rt := &runtimeServer{manifest: m}
	resp, err := rt.GetManifest(context.Background(), &pluginv1.GetManifestRequest{})
	if err != nil || resp.GetManifest() != m {
		t.Fatalf("GetManifest: %v %#v", err, resp)
	}
}

func TestAPIKeyFromConfigAndHelpers(t *testing.T) {
	if apiKeyFromConfig(nil) != "" {
		t.Fatal("nil entries")
	}
	account, err := structpb.NewStruct(map[string]any{"api_key": "  k  "})
	if err != nil {
		t.Fatal(err)
	}
	if got := apiKeyFromConfig([]*pluginv1.ConfigEntry{
		nil,
		{Key: "other", Value: account},
		{Key: "account", Value: nil},
		{Key: "account", Value: account},
	}); got != "k" {
		t.Fatalf("api key = %q", got)
	}

	if itemKind("EPISODE") != provider.ItemKindEpisode || itemKind("movie") != provider.ItemKindMovie || itemKind("x") != 0 {
		t.Fatal("itemKind")
	}
	if markerKind("intro") == 0 || markerKind("credits") == 0 || markerKind("recap") == 0 || markerKind("preview") == 0 || markerKind("x") != 0 {
		t.Fatal("markerKind")
	}
	if markerKindName(provider.MarkerKindIntro) != "intro" || markerKindName(0) != "" {
		t.Fatal("markerKindName")
	}
	if firstNonEmpty("", "  ", "a") != "a" || firstNonEmpty("", " ") != "" {
		t.Fatal("firstNonEmpty")
	}

	ids := externalIDs(nil)
	if len(ids) != 0 {
		t.Fatal("nil ids")
	}
	extra, err := structpb.NewStruct(map[string]any{"Custom": "  v ", "n": 1})
	if err != nil {
		t.Fatal(err)
	}
	ids = externalIDs(&pluginv1.MarkerExternalIDs{
		TmdbId:      "1",
		ImdbId:      "tt2",
		TvdbId:      "3",
		ProviderIds: extra,
	})
	if ids[provider.ExternalIDKeyTMDB] != "1" || ids["custom"] != "v" {
		t.Fatalf("%#v", ids)
	}

	start := 1.5
	end := 2.5
	sub := submissionFromProto(&pluginv1.SubmitMarkerRequest{
		ItemType:        "movie",
		ExternalIds:     &pluginv1.MarkerExternalIDs{TmdbId: "9"},
		Segment:         "credits",
		DurationSeconds: 100,
		StartSeconds:    &start,
		EndSeconds:      &end,
	})
	if sub.Kind != provider.ItemKindMovie || sub.Start == nil || sub.End == nil {
		t.Fatalf("%#v", sub)
	}

	if err := providerError(nil); err != nil {
		t.Fatal(err)
	}
	plain := providerError(errString("x"))
	if plain.Error() != "x" {
		t.Fatalf("%v", plain)
	}
	mapped := providerError(&provider.RetryAfterError{RetryAfter: 0, Message: "limited"})
	if mapped == nil {
		t.Fatal("expected mapped error")
	}
	mapped = providerError(&provider.RetryAfterError{RetryAfter: 2 * time.Second, Message: "limited"})
	if mapped == nil {
		t.Fatal("expected retry detail error")
	}
}

func TestFetchMarkersErrorAndSubmitSuccess(t *testing.T) {
	server := testMarkerServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			_, _ = w.Write([]byte(`{"submissions":[{"id":"s1","status":"pending","weight":1.5}]}`))
			return
		}
		http.Error(w, "nope", http.StatusBadRequest)
	}, "key")

	_, err := server.FetchMarkers(context.Background(), &pluginv1.FetchMarkersRequest{
		ItemType:     "episode",
		ExternalIds:  &pluginv1.MarkerExternalIDs{TmdbId: "1"},
		SeasonNumber: 1, EpisodeNumber: 1,
	})
	if err == nil {
		t.Fatal("expected fetch error")
	}

	resp, err := server.SubmitMarker(context.Background(), &pluginv1.SubmitMarkerRequest{
		ItemType:        "episode",
		ExternalIds:     &pluginv1.MarkerExternalIDs{TmdbId: "123"},
		SeasonNumber:    1,
		EpisodeNumber:   2,
		Segment:         "intro",
		DurationSeconds: 1800,
		EndSeconds:      floatPtr(60),
	})
	if err != nil {
		t.Fatalf("SubmitMarker: %v", err)
	}
	if resp.GetSubmissionId() != "s1" {
		t.Fatalf("%#v", resp)
	}
}

func TestGetMarkerProviderStatsError(t *testing.T) {
	server := testMarkerServer(t, func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "bad", http.StatusUnauthorized)
	}, "key")
	if _, err := server.GetMarkerProviderStats(context.Background(), &pluginv1.GetMarkerProviderStatsRequest{}); err == nil {
		t.Fatal("expected stats error")
	}
}

func TestFetchMarkersMovie(t *testing.T) {
	server := testMarkerServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"type":"movie","intro":[{"end_ms":90000,"confidence":0.9,"submission_count":2}]}`))
	}, "")
	resp, err := server.FetchMarkers(context.Background(), &pluginv1.FetchMarkersRequest{
		ItemType:        "movie",
		ExternalIds:     &pluginv1.MarkerExternalIDs{ImdbId: "tt1"},
		DurationSeconds: 7200,
	})
	if err != nil {
		t.Fatalf("FetchMarkers: %v", err)
	}
	if len(resp.GetMarkers()) != 1 || resp.GetMarkers()[0].GetSegment() != "intro" {
		t.Fatalf("%#v", resp.GetMarkers())
	}
}

type errString string

func (e errString) Error() string { return string(e) }
