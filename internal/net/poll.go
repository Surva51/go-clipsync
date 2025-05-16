// poll.go — HTTP chunked-poll transport implementing the Client interface.
package net

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"strconv"
	"time"

	core "clipsync/internal"
)

// httpClient does polling against /clip.
type httpClient struct {
	url    string
	client *http.Client
	*shared
}

var _ Client = (*httpClient)(nil)

// NewHTTP builds an HTTP poll client.
func NewHTTP(url string, id string, keyHex string, timeout time.Duration) (*httpClient, error) {
	sh, err := newShared(id, keyHex)
	if err != nil {
		return nil, err
	}
	return &httpClient{
		url:    url,
		client: &http.Client{Timeout: timeout},
		shared: sh,
	}, nil
}

/*──────── Send (upload chunked snapshot) ──────────────────────*/
func (c *httpClient) Send(snap core.Snapshot) error {
	snap.Quick = core.QuickKey(snap.Items)

	body := mustJSON(&snap)

	// size check
	if len(body) > bodyCap {
		return errors.New("snapshot >32 MiB, dropped")
	}

	// slice into chunks
	const chunkSize = 300 * 1024
	var chunks [][]byte
	for i := 0; i < len(body); i += chunkSize {
		end := i + chunkSize
		if end > len(body) {
			end = len(body)
		}
		chunks = append(chunks, body[i:end])
	}

	// generate chunk ID
	cid := randomID(8)

	// upload each chunk
	for idx, part := range chunks {
		totalHdr := len(chunks)          // send real total every time
		if err := c.postChunkWithRetry(
			part, cid, idx, totalHdr,    // <-- pass it here
			maxRetries, baseDelay, delayFactor, maxDelay,
		); err != nil {
			return err
		}
	}
	return nil
}

// postChunkWithRetry uploads one chunk with exponential backoff.
func (c *httpClient) postChunkWithRetry(
	chunkData []byte, cid string, idx, total int,
	maxRetries int, baseDelay, delayFactor, maxDelay time.Duration,
) error {
	var lastErr error
	delay := baseDelay

	for retry := 0; retry <= maxRetries; retry++ {
		req, err := http.NewRequest("POST", c.url, bytes.NewReader(chunkData))
		if err != nil {
			return err
		}

		req.Header.Set("X-Auth-Token", c.buildAuthHeader())
		req.Header.Set("X-Device-Id", c.id)
		req.Header.Set("X-Chunk-Id", cid)
		req.Header.Set("X-Chunk-Idx", strconv.Itoa(idx))
		req.Header.Set("X-Chunk-Total", strconv.Itoa(total))
		req.Header.Set("Content-Type", "application/octet-stream")

		resp, err := c.client.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("POST chunk %d: %w", idx, err)
		} else {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()

			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				return nil // Success
			}
			lastErr = fmt.Errorf("chunk %d: status %d: %s", idx, resp.StatusCode, body)
		}

		if retry < maxRetries {
			// Add jitter: +/- 20%
			jitter := time.Duration(float64(delay) * (0.8 + 0.4*rand.Float64()))
			time.Sleep(jitter)
			delay = time.Duration(float64(delay) * delayFactor)
			if delay > maxDelay {
				delay = maxDelay
			}
		}
	}

	return lastErr
}

// Constants for retry behavior
const (
	maxRetries  = 5
	baseDelay   = 100 * time.Millisecond
	delayFactor = 1.5
	maxDelay    = 2 * time.Second
)

/*──────── Poll (discover + fetch loop) ────────────────────────*/
func (c *httpClient) Poll(ctx context.Context, out chan<- core.Snapshot) {
	var current state // tracks the current in-progress download

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		// discover
		meta, err := c.discover(ctx)
		if err != nil {
			time.Sleep(200 * time.Millisecond)
			continue
		}

		// new snapshot?
		if meta.cid != "" && meta.cid != current.cid {
			current = state{
				cid:   meta.cid,
				total: meta.total,
				parts: make(map[int][]byte),
			}
		}

		// fetch missing parts
		if current.cid != "" {
			for _, idx := range meta.have {
				if _, exists := current.parts[idx]; !exists {
					data, err := c.fetchChunk(ctx, current.cid, idx)
					if err == nil {
						current.parts[idx] = data
					}
				}
			}

			// assemble if complete
			if current.total > 0 && len(current.parts) == current.total {
				if snap := current.assemble(); snap != nil && snap.Origin != c.id {
					out <- *snap
				}
				current = state{} // reset
			}
		}

		time.Sleep(200 * time.Millisecond)
	}
}

// discover fetches metadata from server.
func (c *httpClient) discover(ctx context.Context) (discoverResp, error) {
	req, _ := http.NewRequestWithContext(ctx, "GET", c.url, nil)
	req.Header.Set("X-Auth-Token", c.buildAuthHeader())
	req.Header.Set("X-Device-Id", c.id)

	resp, err := c.client.Do(req)
	if err != nil {
		return discoverResp{}, err
	}
	defer resp.Body.Close()

	var meta discoverResp
	if err := json.NewDecoder(resp.Body).Decode(&meta); err != nil {
		return discoverResp{}, err
	}
	return meta, nil
}

// fetchChunk downloads one part.
func (c *httpClient) fetchChunk(ctx context.Context, cid string, idx int) ([]byte, error) {
	req, _ := http.NewRequestWithContext(ctx, "GET", c.url, nil)
	req.Header.Set("X-Auth-Token", c.buildAuthHeader())
	req.Header.Set("X-Device-Id", c.id)
	req.Header.Set("X-Chunk-Id", cid)
	req.Header.Set("X-Chunk-Idx", strconv.Itoa(idx))

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, errors.New(resp.Status)
	}

	data, _ := io.ReadAll(io.LimitReader(resp.Body, chunkSize+1024))
	return data, nil
}

/*──────── internal types ──────────────────────────────────────*/

// Response from discover endpoint
type discoverResp struct {
	cid   string   `json:"cid"`
	total int      `json:"total"`
	have  []int    `json:"have"`
}

// Tracks current download state
type state struct {
	cid   string
	total int
	parts map[int][]byte
}

// assemble merges chunks into a Snapshot.
func (s *state) assemble() *core.Snapshot {
	if s.total == 0 || len(s.parts) != s.total {
		return nil
	}

	var full []byte
	for i := 0; i < s.total; i++ {
		full = append(full, s.parts[i]...)
	}

	var snap core.Snapshot
	if err := json.Unmarshal(full, &snap); err != nil {
		return nil
	}
	return &snap
}

// randomID generates a random hex string.
func randomID(length int) string {
	b := make([]byte, length)
	rand.Read(b)
	return fmt.Sprintf("%x", b)
}

// Constants for chunking
const chunkSize = 300 * 1024
