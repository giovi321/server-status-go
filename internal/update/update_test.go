package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestLatestAndApply(t *testing.T) {
	binary := []byte("#!/bin/true\nnew-binary-v2\n")
	sum := sha256.Sum256(binary)
	sumHex := hex.EncodeToString(sum[:])

	mux := http.NewServeMux()
	mux.HandleFunc("/repos/me/repo/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		base := "http://" + r.Host
		_ = json.NewEncoder(w).Encode(map[string]any{
			"tag_name": "v2.0.0",
			"assets": []map[string]string{
				{"name": "server-status-linux-amd64", "browser_download_url": base + "/dl/bin"},
				{"name": "server-status-linux-amd64.sha256", "browser_download_url": base + "/dl/sum"},
			},
		})
	})
	mux.HandleFunc("/dl/bin", func(w http.ResponseWriter, r *http.Request) { w.Write(binary) })
	mux.HandleFunc("/dl/sum", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "%s  server-status-linux-amd64\n", sumHex)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	rel, err := Latest(context.Background(), srv.URL, "me/repo", "server-status-linux-amd64")
	if err != nil {
		t.Fatal(err)
	}
	if rel.Version != "v2.0.0" || rel.Sha256 != sumHex {
		t.Fatalf("release: %+v", rel)
	}

	dir := t.TempDir()
	dest := filepath.Join(dir, "server-status")
	if err := os.WriteFile(dest, []byte("old-binary-v1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Apply(context.Background(), srv.Client(), rel, dest); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(dest)
	if string(got) != string(binary) {
		t.Fatalf("dest not swapped: %q", got)
	}
	bak, _ := os.ReadFile(dest + ".bak")
	if string(bak) != "old-binary-v1\n" {
		t.Fatalf("backup missing/wrong: %q", bak)
	}
}

func TestApplyRejectsBadChecksum(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("tampered")) }))
	defer srv.Close()
	dir := t.TempDir()
	dest := filepath.Join(dir, "server-status")
	os.WriteFile(dest, []byte("original"), 0o755)
	rel := Release{Version: "v2", AssetURL: srv.URL, Sha256: "deadbeef"}
	if err := Apply(context.Background(), srv.Client(), rel, dest); err == nil {
		t.Fatal("expected checksum mismatch error")
	}
	got, _ := os.ReadFile(dest)
	if string(got) != "original" {
		t.Fatalf("bad download must not replace binary; got %q", got)
	}
}

func TestLatestRequiresChecksum(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/me/repo/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		base := "http://" + r.Host
		_ = json.NewEncoder(w).Encode(map[string]any{
			"tag_name": "v2.0.0",
			"assets": []map[string]string{
				{"name": "server-status-linux-amd64", "browser_download_url": base + "/dl/bin"},
			}, // no .sha256 sibling
		})
	})
	mux.HandleFunc("/dl/bin", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("x")) })
	srv := httptest.NewServer(mux)
	defer srv.Close()
	if _, err := Latest(context.Background(), srv.URL, "me/repo", "server-status-linux-amd64"); err == nil {
		t.Fatal("Latest must fail closed when no .sha256 asset is published")
	}
}

func TestApplyRefusesWithoutChecksum(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("new")) }))
	defer srv.Close()
	dir := t.TempDir()
	dest := filepath.Join(dir, "server-status")
	os.WriteFile(dest, []byte("original"), 0o755)
	rel := Release{Version: "v2", AssetURL: srv.URL, Sha256: ""}
	if err := Apply(context.Background(), srv.Client(), rel, dest); err == nil {
		t.Fatal("Apply must refuse an empty checksum (fail closed)")
	}
	if got, _ := os.ReadFile(dest); string(got) != "original" {
		t.Fatalf("binary must be untouched: %q", got)
	}
}
