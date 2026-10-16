package delivery

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestResumedUploadDoesNotLeakToken(t *testing.T) {
	cache := t.TempDir()
	hash := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	os.WriteFile(filepath.Join(cache, hash), []byte("abcdef"), 0600)
	var objectURL string
	finalized := false
	objects := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" {
			t.Error("API token leaked")
		}
		if r.Header.Get("Upload-Offset") != "2" {
			t.Error("wrong offset")
		}
		b, _ := io.ReadAll(r.Body)
		if string(b) != "cdef" {
			t.Errorf("wrong resumed body %q", b)
		}
		w.WriteHeader(200)
	}))
	defer objects.Close()
	objectURL = objects.URL
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fixture" {
			t.Error("missing API auth")
		}
		if r.URL.Path == "/projects/game/uploads" {
			json.NewEncoder(w).Encode(Session{ID: "session", Missing: []Missing{{Hash: hash, URL: objectURL, Offset: 2}}})
		} else if r.URL.Path == "/projects/game/uploads/session/finalize" {
			finalized = true
			w.WriteHeader(200)
		} else {
			t.Error("unexpected endpoint")
			w.WriteHeader(404)
		}
	}))
	defer api.Close()
	m := Manifest{Schema: 1, Version: "v1", Files: []File{{Path: "index.html", Size: 6, Chunks: []Chunk{{Hash: hash, Size: 6}}}}}
	n, e := Upload(context.Background(), api.URL, "game", "fixture", cache, m)
	if e != nil {
		t.Fatal(e)
	}
	if n != 4 || !finalized {
		t.Fatal("resume/finalize mismatch")
	}
}
func TestInvalidResumeStopsFinalize(t *testing.T) {
	finalized := false
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/projects/game/uploads" {
			json.NewEncoder(w).Encode(Session{ID: "s", Missing: []Missing{{Hash: "unknown", Offset: -1}}})
		} else {
			finalized = true
		}
	}))
	defer s.Close()
	if _, e := Upload(context.Background(), s.URL, "game", "", t.TempDir(), Manifest{}); e == nil {
		t.Fatal("invalid response accepted")
	}
	if finalized {
		t.Fatal("finalized invalid upload")
	}
}
