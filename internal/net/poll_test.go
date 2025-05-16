package net

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	core "clipsync/internal"
)

func TestSendAddsAuthHeader(t *testing.T) {
	// fake server records the auth header
	var gotHeader string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get("X-Auth-Token")
		w.WriteHeader(200)
	}))
	defer ts.Close()

	cli, _ := NewHTTP(ts.URL, "deadbeef", "test-secret-key", 5*time.Second)
	err := cli.Send(core.Snapshot{}) // empty fine for this test
	if err != nil {
		t.Fatalf("Send: %v", err)
	}
	if gotHeader == "" {
		t.Fatalf("missing X-Auth-Token header")
	}
	// sanity-check that it's valid base64
	if _, err := base64.StdEncoding.DecodeString(gotHeader); err != nil {
		t.Fatalf("header not base64: %v", err)
	}
}

func TestPollPassesSnapshot(t *testing.T) {
	want := core.Snapshot{Origin: "other"}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(&want)
	}))
	defer ts.Close()

	cli, _ := NewHTTP(ts.URL, "deadbeef", "test-secret-key", 5*time.Second)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	out := make(chan core.Snapshot, 1)
	go cli.Poll(ctx, out)

	select {
	case got := <-out:
		if got.Origin != want.Origin {
			t.Fatalf("got origin %q, want %q", got.Origin, want.Origin)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("timeout waiting for snapshot")
	}
}

func TestChunking(t *testing.T) {
	// create a large fake snapshot
	largePay := make([]byte, 400*1024) // 400 KB will split into 2 chunks
	for i := range largePay {
		largePay[i] = byte(i % 256)
	}

	item := core.Item{
		Fmt:     8,
		Payload: base64.StdEncoding.EncodeToString(largePay),
		ByteLen: len(largePay),
	}
	snap := core.Snapshot{
		Origin: "me",
		Items:  []core.Item{item},
	}

	var gotChunks int
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Chunk-Id") != "" {
			gotChunks++
		}
		w.WriteHeader(200)
	}))
	defer ts.Close()

	cli, _ := NewHTTP(ts.URL, "deadbeef", "test-secret-key", 5*time.Second)
	err := cli.Send(snap)
	if err != nil {
		t.Fatalf("Send: %v", err)
	}

	// should have split into 2 chunks
	if gotChunks != 2 {
		t.Fatalf("expected 2 chunks, got %d", gotChunks)
	}
}
