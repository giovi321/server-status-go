package update

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// releaseServer serves a minimal latest-release document and counts how many
// times the release endpoint was hit.
func releaseServer(t *testing.T, tag string, hits *int32) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/me/repo/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(hits, 1)
		base := "http://" + r.Host
		_ = json.NewEncoder(w).Encode(map[string]any{
			"tag_name": tag,
			"html_url": "https://github.com/me/repo/releases/tag/" + tag,
			"assets": []map[string]string{
				{"name": "asset", "browser_download_url": base + "/dl/bin"},
				{"name": "asset.sha256", "browser_download_url": base + "/dl/sum"},
			},
		})
	})
	mux.HandleFunc("/dl/sum", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "%s  asset\n", "abc123")
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestLatestCarriesReleaseURL(t *testing.T) {
	var hits int32
	srv := releaseServer(t, "v2.0.0", &hits)
	rel, err := Latest(context.Background(), srv.URL, "me/repo", "asset")
	if err != nil {
		t.Fatal(err)
	}
	// The Home Assistant update entity links to this from its release notes.
	if rel.HTMLURL != "https://github.com/me/repo/releases/tag/v2.0.0" {
		t.Fatalf("html_url: %q", rel.HTMLURL)
	}
}

// The cache is what keeps a fleet inside GitHub's unauthenticated rate limit.
func TestCheckerCachesWithinInterval(t *testing.T) {
	var hits int32
	srv := releaseServer(t, "v2.0.0", &hits)
	c := NewChecker(srv.URL, "me/repo", "asset", time.Hour)

	for i := 0; i < 5; i++ {
		rel, ok := c.Latest(context.Background())
		if !ok || rel.Version != "v2.0.0" {
			t.Fatalf("call %d: %+v ok=%v", i, rel, ok)
		}
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Fatalf("expected 1 API call within the interval, got %d", got)
	}
}

func TestCheckerRefreshBypassesCache(t *testing.T) {
	var hits int32
	srv := releaseServer(t, "v2.0.0", &hits)
	c := NewChecker(srv.URL, "me/repo", "asset", time.Hour)

	if _, ok := c.Latest(context.Background()); !ok {
		t.Fatal("first lookup failed")
	}
	// Pressing Install must act on the release that exists now.
	if _, err := c.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := atomic.LoadInt32(&hits); got != 2 {
		t.Fatalf("Refresh should always hit the API, got %d calls", got)
	}
}

func TestCheckerCachedNeedsNoNetwork(t *testing.T) {
	// Point at a dead address: Cached must not dial it.
	c := NewChecker("http://127.0.0.1:1", "me/repo", "asset", time.Hour)
	if _, ok := c.Cached(); ok {
		t.Fatal("Cached must report not-ok before any successful lookup")
	}
}

// A failed refresh must not blank a version Home Assistant is already showing.
func TestCheckerKeepsLastGoodOnFailure(t *testing.T) {
	var hits int32
	fail := make(chan struct{})
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/me/repo/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-fail:
			w.WriteHeader(http.StatusInternalServerError)
			return
		default:
		}
		atomic.AddInt32(&hits, 1)
		base := "http://" + r.Host
		_ = json.NewEncoder(w).Encode(map[string]any{
			"tag_name": "v2.0.0",
			"assets": []map[string]string{
				{"name": "asset", "browser_download_url": base + "/dl/bin"},
				{"name": "asset.sha256", "browser_download_url": base + "/dl/sum"},
			},
		})
	})
	mux.HandleFunc("/dl/sum", func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "abc123  asset\n") })
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := NewChecker(srv.URL, "me/repo", "asset", time.Hour)
	if _, ok := c.Latest(context.Background()); !ok {
		t.Fatal("first lookup failed")
	}
	close(fail)
	if _, err := c.Refresh(context.Background()); err == nil {
		t.Fatal("expected the refresh to fail")
	}
	rel, ok := c.Cached()
	if !ok || rel.Version != "v2.0.0" {
		t.Fatalf("a failed refresh must keep the last good release, got %+v ok=%v", rel, ok)
	}
}
