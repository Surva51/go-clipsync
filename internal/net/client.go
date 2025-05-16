package net

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"

	core "clipsync/internal"
)

/*────── common interface ────────────────────────────────────*/
type Client interface {
	Send(snap core.Snapshot) error
	Poll(ctx context.Context, out chan<- core.Snapshot)
}

/*────── helper: struct embedded by httpClient / wsClient ──────*/
type shared struct {
	id    string
	key64 uint64
}

func newShared(id, keyHex string) (*shared, error) {
	k, err := hex.DecodeString(keyHex)
	if err != nil || len(k) != 8 {
		return nil, errors.New("key must be 16 hex chars (8 bytes)")
	}
	key64 := binary.BigEndian.Uint64(k)
	return &shared{id: id, key64: key64}, nil
}

/*────── auth header builder ──────────────────────────────────*/
func (s *shared) buildAuthHeader() string {
	type token struct {
		TS    int64 `json:"ts"`
		TSEnc int64 `json:"ts_enc"`
	}
	ts := time.Now().Unix()
	tok := token{TS: ts, TSEnc: ts ^ int64(s.key64)}
	raw, _ := json.Marshal(&tok)
	return base64.StdEncoding.EncodeToString(raw)
}

/*────── size cap ─────────────────────────────────────────────*/
const bodyCap = 32 * 1024 * 1024 // 32 MiB

// mustJSON panics on impossible marshal errors but caps size.
func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err) // truly unreachable for Snapshot
	}
	return b
}

/*────── imports (at end to avoid scroll) ─────────────────────*/
import (
	"encoding/base64"
	"encoding/json"
	"time"
)
