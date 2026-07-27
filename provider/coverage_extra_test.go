package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestClientSetAPIKeyCloseAndCacheHelpers(t *testing.T) {
	c := NewClient("  initial  ")
	defer c.Close()
	c.SetAPIKey("  rotated  ")
	c.mu.RLock()
	if c.apiKey != "rotated" {
		t.Fatalf("apiKey = %q", c.apiKey)
	}
	c.mu.RUnlock()

	if cacheKeyEpisode("", "", "tt1", 1, 2, 3) == "" {
		t.Fatal("imdb episode key")
	}
	if cacheKeyMovie("1", "", "", 0) == "" || cacheKeyMovie("", "2", "", 0) == "" || cacheKeyMovie("", "", "tt3", 9) == "" {
		t.Fatal("movie keys")
	}
	resp := &http.Response{Header: http.Header{}}
	if retryAfterOrDefault(resp, 0) != time.Second {
		t.Fatal("default backoff")
	}
	resp.Header.Set("Retry-After", "7")
	if retryAfterOrDefault(resp, 0) != 7*time.Second {
		t.Fatal("retry-after header")
	}
	resp.Header.Set("Retry-After", "nope")
	if retryAfterOrDefault(resp, 1) != 2*time.Second {
		t.Fatal("invalid retry-after")
	}
}

func TestFetchEpisodeValidationAndMovieIMDB(t *testing.T) {
	c := NewClient("")
	defer c.Close()
	if _, err := c.FetchEpisode(context.Background(), "", "", "", 1, 1, 0); err == nil {
		t.Fatal("expected missing id error")
	}
	if _, err := c.FetchEpisode(context.Background(), "1", "", "", 0, 1, 0); err == nil {
		t.Fatal("expected season/episode error")
	}
	if _, err := c.FetchMovie(context.Background(), "", "", "", 0); err == nil {
		t.Fatal("expected missing id error")
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("imdb_id") != "tt9" {
			t.Errorf("query = %v", r.URL.Query())
		}
		_, _ = w.Write([]byte(`{"type":"movie","intro":[{"end_ms":1000}]}`))
	}))
	defer srv.Close()
	c.SetBaseURL(srv.URL)
	if _, err := c.FetchMovie(context.Background(), "", "", "tt9", 1000); err != nil {
		t.Fatalf("FetchMovie: %v", err)
	}
}

func TestFetchHTTPBranches(t *testing.T) {
	t.Run("not found", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}))
		defer srv.Close()
		c := NewClient("")
		defer c.Close()
		c.SetBaseURL(srv.URL)
		got, err := c.FetchEpisode(context.Background(), "1", "", "", 1, 1, 0)
		if err != nil || got != nil {
			t.Fatalf("got=%v err=%v", got, err)
		}
	})

	t.Run("bad request", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "bad", http.StatusBadRequest)
		}))
		defer srv.Close()
		c := NewClient("k")
		defer c.Close()
		c.SetBaseURL(srv.URL)
		if _, err := c.FetchEpisode(context.Background(), "1", "", "", 1, 1, 0); err == nil {
			t.Fatal("expected http error")
		}
	})

	t.Run("decode error", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{bad`))
		}))
		defer srv.Close()
		c := NewClient("")
		defer c.Close()
		c.SetBaseURL(srv.URL)
		if _, err := c.FetchEpisode(context.Background(), "1", "", "", 1, 1, 0); err == nil {
			t.Fatal("expected decode error")
		}
	})

	t.Run("rate limit then success", func(t *testing.T) {
		var hits int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			if atomic.AddInt32(&hits, 1) == 1 {
				w.Header().Set("Retry-After", "1")
				w.WriteHeader(http.StatusTooManyRequests)
				return
			}
			_, _ = w.Write([]byte(`{"type":"episode"}`))
		}))
		defer srv.Close()
		c := NewClient("")
		defer c.Close()
		c.SetBaseURL(srv.URL)
		if _, err := c.FetchEpisode(context.Background(), "1", "", "", 1, 1, 0); err != nil {
			t.Fatalf("FetchEpisode: %v", err)
		}
	})

	t.Run("server error then success", func(t *testing.T) {
		var hits int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			if atomic.AddInt32(&hits, 1) == 1 {
				w.WriteHeader(http.StatusBadGateway)
				return
			}
			_, _ = w.Write([]byte(`{"type":"episode"}`))
		}))
		defer srv.Close()
		c := NewClient("")
		defer c.Close()
		c.SetBaseURL(srv.URL)
		if _, err := c.FetchEpisode(context.Background(), "2", "", "", 1, 1, 0); err != nil {
			t.Fatalf("FetchEpisode: %v", err)
		}
	})
}

func TestProviderBranches(t *testing.T) {
	if NewProvider(nil).ID() != ProviderID {
		t.Fatal("ID")
	}
	var nilP *Provider
	if _, err := nilP.FetchMarkers(context.Background(), Request{}); err != nil {
		t.Fatal(err)
	}
	p := NewProvider(NewClient(""))
	defer p.client.Close()
	if res, err := p.FetchMarkers(context.Background(), Request{Kind: ItemKindEpisode}); err != nil || len(res.Markers) != 0 {
		t.Fatalf("%v %#v", err, res)
	}
	if res, err := p.FetchMarkers(context.Background(), Request{
		Kind: ItemKindEpisode, ExternalIDs: map[string]string{ExternalIDKeyTMDB: "1"},
	}); err != nil || len(res.Markers) != 0 {
		t.Fatalf("missing season: %v %#v", err, res)
	}
	if res, err := p.FetchMarkers(context.Background(), Request{Kind: 99, ExternalIDs: map[string]string{ExternalIDKeyTMDB: "1"}}); err != nil || len(res.Markers) != 0 {
		t.Fatalf("bad kind: %v %#v", err, res)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost:
			_, _ = w.Write([]byte(`{"submissions":[]}`))
		case r.URL.Path == "/user/stats":
			_, _ = w.Write([]byte(`{"total":1,"accepted":1,"pending":0,"rejected":0,"acceptance_rate":1,"current_streak":1,"best_streak":1}`))
		default:
			_, _ = w.Write([]byte(`{
				"type":"movie",
				"intro":[{"end_ms":1000,"submission_count":1}],
				"credits":[{"start_ms":2000,"submission_count":1}],
				"recap":[{"end_ms":500,"submission_count":1}],
				"preview":[{"start_ms":3000,"end_ms":4000,"submission_count":1}]
			}`))
		}
	}))
	defer srv.Close()
	c := NewClient("k")
	defer c.Close()
	c.SetBaseURL(srv.URL)
	p = NewProvider(c)

	res, err := p.FetchMarkers(context.Background(), Request{
		Kind: ItemKindMovie, ExternalIDs: map[string]string{ExternalIDKeyTMDB: "1"}, Duration: time.Hour,
	})
	if err != nil || len(res.Markers) < 1 {
		t.Fatalf("movie markers: %v %#v", err, res)
	}

	if _, err := (*Provider)(nil).SubmitMarker(context.Background(), SubmissionRequest{}); err == nil {
		t.Fatal("nil provider submit")
	}
	if _, err := p.SubmitMarker(context.Background(), SubmissionRequest{ExternalIDs: map[string]string{ExternalIDKeyTMDB: "abc"}}); err == nil {
		t.Fatal("bad tmdb")
	}
	if _, err := p.SubmitMarker(context.Background(), SubmissionRequest{
		ExternalIDs: map[string]string{ExternalIDKeyTMDB: "1"}, Segment: 99,
	}); err == nil {
		t.Fatal("bad segment")
	}
	if _, err := p.SubmitMarker(context.Background(), SubmissionRequest{
		Kind: ItemKindEpisode, ExternalIDs: map[string]string{ExternalIDKeyTMDB: "1"}, Segment: MarkerKindIntro,
	}); err == nil {
		t.Fatal("missing season")
	}
	if _, err := p.SubmitMarker(context.Background(), SubmissionRequest{
		Kind: 99, ExternalIDs: map[string]string{ExternalIDKeyTMDB: "1"}, Segment: MarkerKindIntro,
	}); err == nil {
		t.Fatal("bad kind")
	}

	start := time.Duration(0)
	end := 2 * time.Hour
	sub, err := p.SubmitMarker(context.Background(), SubmissionRequest{
		Kind: ItemKindMovie, ExternalIDs: map[string]string{ExternalIDKeyTMDB: "12", ExternalIDKeyIMDB: "tt1"},
		Segment: MarkerKindCredits, Duration: time.Hour, Start: &start, End: &end,
	})
	if err != nil {
		t.Fatalf("submit movie: %v", err)
	}
	if sub.Status != SubmissionStatusPending {
		t.Fatalf("%#v", sub)
	}

	stats, err := p.FetchUserStats(context.Background())
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if stats.Total != 1 {
		t.Fatalf("%#v", stats)
	}
	if _, err := (*Provider)(nil).FetchUserStats(context.Background()); err == nil {
		t.Fatal("nil stats")
	}

	if name, err := segmentName(MarkerKindIntro); err != nil || name != "intro" {
		t.Fatal(name, err)
	}
	if _, err := segmentName(0); err == nil {
		t.Fatal("unknown segment")
	}
	if (&RetryAfterError{Message: "m"}).Error() != "m" {
		t.Fatal("retry message")
	}
	if (&RetryAfterError{RetryAfter: time.Second}).Error() == "" {
		t.Fatal("retry default message")
	}
	var nilErr *RetryAfterError
	if nilErr.Error() != "" {
		t.Fatal("nil retry error")
	}
}

func TestPickMarkerEdgeCases(t *testing.T) {
	end := int64(1000)
	start := int64(2000)
	if _, ok := pickMarker([]segmentTimestamps{{}}, MarkerKindIntro, time.Minute, true); ok {
		t.Fatal("require end")
	}
	if _, ok := pickMarker([]segmentTimestamps{{EndMs: &end}}, MarkerKindCredits, time.Minute, false); ok {
		t.Fatal("require start")
	}
	if _, ok := pickMarker([]segmentTimestamps{{StartMs: &start, EndMs: &end}}, MarkerKindIntro, time.Minute, true); ok {
		t.Fatal("end <= start")
	}
}

func TestCacheExpiryAndClose(t *testing.T) {
	c := newTTLCache[*mediaResponse]()
	defer c.Close()
	c.Set("k", &mediaResponse{Type: "episode"}, time.Millisecond)
	time.Sleep(5 * time.Millisecond)
	if _, ok := c.Get("k"); ok {
		t.Fatal("expected expired")
	}
	c.Set("k2", nil, time.Hour)
	if v, ok := c.Get("k2"); !ok || v != nil {
		t.Fatalf("nil value cache: %v %v", v, ok)
	}
}

func TestFetchExhaustedRetriesAndSubmitErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			w.Header().Set("X-UsageLimit-Reset", "3")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()
	c := NewClient("k")
	defer c.Close()
	c.SetBaseURL(srv.URL)
	if _, err := c.FetchEpisode(context.Background(), "1", "", "", 1, 1, 0); err == nil {
		t.Fatal("expected rate limit exhaustion")
	}
	if _, err := c.submitSegment(context.Background(), submitRequest{TmdbID: 1, Type: "movie", Segment: "intro"}); err == nil {
		t.Fatal("expected submit rate limit")
	}

	c2 := NewClient("")
	defer c2.Close()
	c2.SetBaseURL(srv.URL)
	if _, err := c2.submitSegment(context.Background(), submitRequest{TmdbID: 1}); err == nil {
		t.Fatal("expected missing api key")
	}
	if _, err := c2.fetchUserStats(context.Background()); err == nil {
		t.Fatal("expected missing api key stats")
	}
}

func TestFetchServerErrorExhaustedAndStatsErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/user/stats" {
			_, _ = w.Write([]byte(`{"error":"bad key"}`))
			return
		}
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()
	c := NewClient("k")
	defer c.Close()
	c.SetBaseURL(srv.URL)
	if _, err := c.FetchMovie(context.Background(), "1", "", "", 0); err == nil {
		t.Fatal("expected 500 exhaustion")
	}
	if _, err := c.fetchUserStats(context.Background()); err == nil {
		t.Fatal("expected stats error field")
	}

	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "no", http.StatusUnauthorized)
	}))
	defer bad.Close()
	c.SetBaseURL(bad.URL)
	if _, err := c.fetchUserStats(context.Background()); err == nil {
		t.Fatal("expected stats http error")
	}
	if _, err := c.submitSegment(context.Background(), submitRequest{TmdbID: 1, Type: "movie", Segment: "intro"}); err == nil {
		t.Fatal("expected submit http error")
	}

	dec := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{bad`))
	}))
	defer dec.Close()
	c.SetBaseURL(dec.URL)
	if _, err := c.fetchUserStats(context.Background()); err == nil {
		t.Fatal("expected stats decode error")
	}
	if _, err := c.submitSegment(context.Background(), submitRequest{TmdbID: 1, Type: "movie", Segment: "intro"}); err == nil {
		t.Fatal("expected submit decode error")
	}
}

func TestCanceledLimiterWait(t *testing.T) {
	c := NewClient("")
	defer c.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.FetchEpisode(ctx, "1", "", "", 1, 1, 0); err == nil {
		t.Fatal("expected cancel")
	}
}

func TestNilCacheAndUsageReset(t *testing.T) {
	var c *ttlCache[*mediaResponse]
	if _, ok := c.Get("x"); ok {
		t.Fatal("nil get")
	}
	c.Set("x", nil, time.Second)
	c.Close()

	resp := &http.Response{Header: http.Header{}}
	if usageResetSeconds(resp) != 0 {
		t.Fatal("default reset")
	}
	resp.Header.Set("Retry-After", "0")
	if usageResetSeconds(resp) != 0 {
		t.Fatal("zero reset")
	}
}

func TestProviderFetchErrorAndNilResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "bad", http.StatusBadRequest)
	}))
	defer srv.Close()
	c := NewClient("")
	defer c.Close()
	c.SetBaseURL(srv.URL)
	p := NewProvider(c)
	if _, err := p.FetchMarkers(context.Background(), Request{
		Kind: ItemKindEpisode, ExternalIDs: map[string]string{ExternalIDKeyTMDB: "1"},
		SeasonNumber: 1, EpisodeNumber: 1,
	}); err == nil {
		t.Fatal("expected error")
	}

	nf := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer nf.Close()
	c.SetBaseURL(nf.URL)
	res, err := p.FetchMarkers(context.Background(), Request{
		Kind: ItemKindMovie, ExternalIDs: map[string]string{ExternalIDKeyTMDB: "1"},
	})
	if err != nil || len(res.Markers) != 0 {
		t.Fatalf("%v %#v", err, res)
	}

	statsErr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "x", http.StatusUnauthorized)
	}))
	defer statsErr.Close()
	c.SetAPIKey("k")
	c.SetBaseURL(statsErr.URL)
	if _, err := p.FetchUserStats(context.Background()); err == nil {
		t.Fatal("expected stats error")
	}
}
