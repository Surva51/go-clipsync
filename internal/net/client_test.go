package net

import (
    "encoding/base64"
    "encoding/json"
    "testing"
    "time"
)

type token struct {
    TS    int64 `json:"ts"`
    TSEnc int64 `json:"ts_enc"`
}

func TestBuildAuthHeader(t *testing.T) {
    s, err := newShared("deadbeef", "test-secret-key")
    if err != nil {
        t.Fatalf("newShared: %v", err)
    }
    hdr := s.buildAuthHeader()

    raw, err := base64.StdEncoding.DecodeString(hdr)
    if err != nil {
        t.Fatalf("base64 decode: %v", err)
    }
    var tok token
    if err := json.Unmarshal(raw, &tok); err != nil {
        t.Fatalf("json: %v", err)
    }

    if tok.TSEnc != tok.TS^int64(s.key64) {
        t.Fatalf("ts_enc mismatch: got %d exp %d", tok.TSEnc, tok.TS^int64(s.key64))
    }
    if delta := time.Now().Unix() - tok.TS; delta > 2 || delta < -2 {
        t.Fatalf("timestamp skew: %d s", delta)
    }
}
