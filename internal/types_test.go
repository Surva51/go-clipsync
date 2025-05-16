package internal

import "testing"

/*──────── test the QuickKey deduplication key ─────────────────*/
func TestQuickKey(t *testing.T) {
	items1 := []Item{{Payload: "aGVsbG8="}, {Payload: "d29ybGQ="}}
	items2 := []Item{{Payload: "aGVsbG8="}, {Payload: "d29ybGQ="}}
	items3 := []Item{{Payload: "aGVsbG8="}}

	k1 := QuickKey(items1)
	k2 := QuickKey(items2)
	k3 := QuickKey(items3)

	if k1 != k2 {
		t.Fatalf("identical items, different keys: %q vs %q", k1, k2)
	}
	if k1 == k3 {
		t.Fatalf("different items, same key: %q", k1)
	}
	if len(k1) != 8 || len(k3) != 8 {
		t.Fatalf("expected 8-byte keys, got %d, %d", len(k1), len(k3))
	}
}

func TestQuickKeyEmpty(t *testing.T) {
	k := QuickKey(nil)
	if k != "empty" {
		t.Fatalf("expected 'empty' for nil items, got %q", k)
	}
}
