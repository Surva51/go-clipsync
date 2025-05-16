// ws.go — WebSocket transport implementing the Client interface.
// Uses nhooyr.io/websocket.  All writes serialised via a sync.Mutex.
package net

import (
    "context"
    "encoding/json"
    "errors"
    "sync"
    "time"

    core "clipsync/internal"

    "nhooyr.io/websocket"
)

// wsClient keeps one persistent socket; reconnects with back‑off.
type wsClient struct {
    url string
    *shared
    conn *websocket.Conn
    mu   sync.Mutex // serialises all writes (Ping + Send)
}

var _ Client = (*wsClient)(nil)

func NewWS(url, id, keyHex string) (*wsClient, error) {
    sh, err := newShared(id, keyHex)
    if err != nil {
        return nil, err
    }
    return &wsClient{url: url, shared: sh}, nil
}

/*──────────── dial / close helpers ───────────*/
func (c *wsClient) dial(ctx context.Context) error {
    hdr := map[string][]string{"X-Auth-Token": {c.buildAuthHeader()}}
    ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
    defer cancel()
    conn, _, err := websocket.Dial(ctx, c.url, &websocket.DialOptions{HTTPHeader: hdr})
    if err != nil {
        return err
    }
    c.conn = conn
    return nil
}

func (c *wsClient) close() {
    if c.conn != nil {
        _ = c.conn.Close(websocket.StatusNormalClosure, "bye")
        c.conn = nil
    }
}

/*──────────── Client.Send ───────────*/
func (c *wsClient) Send(snap core.Snapshot) error {
    if c.conn == nil {
        return errors.New("ws: not connected")
    }
    snap.Quick = core.QuickKey(snap.Items)
    msg := mustJSON(snap)
    if len(msg) > bodyCap {
        return errors.New("body >32 MiB, dropped")
    }
    ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()

    c.mu.Lock()
    err := c.conn.Write(ctx, websocket.MessageText, msg)
    c.mu.Unlock()
    return err
}

/*──────────── Client.Poll ───────────*/
func (c *wsClient) Poll(ctx context.Context, out chan<- core.Snapshot) {
    backoff := 500 * time.Millisecond
reconnect:
    if err := c.dial(ctx); err != nil {
        select {
        case <-ctx.Done():
            return
        case <-time.After(backoff):
            backoff = minDuration(backoff*2, 8*time.Second)
            goto reconnect
        }
    }
    backoff = 500 * time.Millisecond // reset on success

    ping := time.NewTicker(25 * time.Second)
    defer ping.Stop()

    for {
        select {
        case <-ctx.Done():
            c.close()
            return
        case <-ping.C:
            c.mu.Lock()
            _ = c.conn.Ping(context.Background())
            c.mu.Unlock()
        default:
            _, data, err := c.conn.Read(ctx)
            if err != nil {
                c.close()
                goto reconnect
            }
            if len(data) > bodyCap {
                continue
            }
            var snap core.Snapshot
            if json.Unmarshal(data, &snap) != nil {
                continue
            }
            if snap.Origin != c.id {
                out <- snap
            }
        }
    }
}

func minDuration(a, b time.Duration) time.Duration {
    if a < b {
        return a
    }
    return b
}
