package internal

import "crypto/sha256"

/*──────── data types shared by everything ─────────────────────*/
type Item struct {
	Fmt      uint32 `json:"fmt"`      // numeric clipboard format
	Payload  string `json:"payload"`  // base64-encoded data
	ByteLen  int    `json:"byte_len"`
	FmtName  string `json:"fmt_name"`  // opt (PNG, image/png)
	MimeType string `json:"mime_type"` // opt (image/png)
}

/*──────── a batch of clipboard items ─────────────────────────*/
type Snapshot struct {
	Origin string `json:"origin"` // 8-char client ID
	TS     int64  `json:"ts"`     // Unix timestamp
	Items  []Item `json:"items"`
	Quick  string `json:"qkey"` // for filtering dupes
}

/*──────── helper: dedupe key ──────────────────────────────────*/
func QuickKey(items []Item) string {
	if len(items) == 0 {
		return "empty"
	}
	h := sha256.New()
	for _, it := range items {
		h.Write([]byte(it.Payload)) // base64 string good enough
	}
	return string(h.Sum(nil)[:8])
}
