package net

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	core "clipsync/internal"

	"nhooyr.io/websocket"
)

// TestWSHandshake verifies WebSocket client sends auth header.
func TestWSHandshake(t *testing.T) {
	var gotAuth string

	// WebSocket server that captures the auth header
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("X-Auth-Token")
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Fatalf("accept: %v", err)
		}
		defer c.Close(websocket.StatusNormalClosure, "bye")
	}))
	defer ts.Close()

	// convert http:// to ws://
	wsURL := "ws" + ts.URL[4:]

	cli, err := NewWS(wsURL, "deadbeef", "test-secret-key")
	if err != nil {
		t.Fatalf("NewWS: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	out := make(chan core.Snapshot)
	go cli.Poll(ctx, out)

	// wait for connection
	time.Sleep(100 * time.Millisecond)

	if gotAuth == "" {
		t.Fatalf("no auth header received")
	}
}

// TestWSEcho verifies send/receive through WebSocket.
func TestWSEcho(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close(websocket.StatusNormalClosure, "")

		// echo server
		for {
			_, msg, err := c.Read(context.Background())
			if err != nil {
				return
			}
			// echo back
			if err := c.Write(context.Background(), websocket.MessageText, msg); err != nil {
				return
			}
		}
	}))
	defer ts.Close()

	wsURL := "ws" + ts.URL[4:]
	cli, _ := NewWS(wsURL, "me", "test-secret-key")

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	out := make(chan core.Snapshot, 1)
	go cli.Poll(ctx, out)

	// wait for connection
	time.Sleep(100 * time.Millisecond)

	// send a snapshot
	want := core.Snapshot{
		Origin: "other",
		Items:  []core.Item{{Fmt: 1, Payload: "dGVzdA=="}},
	}

	if err := cli.Send(want); err != nil {
		t.Fatalf("Send: %v", err)
	}

	// should receive echo (but filtered since Origin != "me")
	select {
	case got := <-out:
		if got.Origin != want.Origin {
			t.Fatalf("origin mismatch")
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timeout waiting for echo")
	}
}

// TestWSReconnect verifies reconnection behavior.
func TestWSReconnect(t *testing.T) {
	var connCount int

	// server that accepts only first connection
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		connCount++
		if connCount == 1 {
			c, _ := websocket.Accept(w, r, nil)
			// immediately close to trigger reconnect
			c.Close(websocket.StatusNormalClosure, "test")
		} else {
			c, _ := websocket.Accept(w, r, nil)
			// keep second connection alive
			time.Sleep(1 * time.Second)
			c.Close(websocket.StatusNormalClosure, "")
		}
	}))
	defer ts.Close()

	wsURL := "ws" + ts.URL[4:]
	cli, _ := NewWS(wsURL, "me", "test-secret-key")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	out := make(chan core.Snapshot)
	go cli.Poll(ctx, out)

	// wait for reconnect
	time.Sleep(1500 * time.Millisecond)

	if connCount < 2 {
		t.Fatalf("expected at least 2 connections, got %d", connCount)
	}
}
