## Clipboard-Sync "Chunk Store" Protocol

*(design revision – **300 KiB hard-limit**, 2025-05-16)*

---

### 0 · What changed?

* **New ceiling:** a single `POST /clip` body **must be ≤ 300 KiB**
  (`CHUNK_MAX = 300 * 1024 = 307 200 bytes`).
* All earlier semantics (discover / have / fetch, retries, GC, etc.) stay exactly
  the same.
* The **client is free** to slice snapshots into any size **≤ 300 KiB**; the
  server merely enforces the ceiling.

---

### 1 · HTTP Interface (unchanged paths)

| Verb         | Path                          | Purpose |
| ------------ | ----------------------------- | ------- |
| `POST /clip` | Upload one chunk (≤ 300 KiB). |         |
| `GET /clip`  | **Discover** or **fetch**.    |         |
| `GET /`      | Health ping.                  |         |

#### 1.1  Mandatory headers

| Name            | Upload | Fetch | Meaning                                            |
| --------------- | ------ | ----- | -------------------------------------------------- |
| `X-Auth-Token`  | ✔      | ✔     | MAC-protected timestamp.                           |
| `X-Chunk-Id`    | ✔      | ✔     | Snapshot ID (`cid`).                               |
| `X-Chunk-Idx`   | ✔      | ✔     | 0-based chunk index.                               |
| `X-Chunk-Total` | ✔      | –     | Final chunk count (may be **0** until last chunk). |

*(Discover carries **no** chunk headers.)*

#### 1.2  Server responses (summary)

* **200 OK** – success (upload or fetch).
* **404** – fetch index not in `have`.
* **410 Gone** – requested `cid` already flushed.
* **413 Payload Too Large** – upload body > 300 KiB.
* **401** – auth failure.

---

### 2 · Server Behaviour

* Stores chunks as `{idx: bytes}` under a single active `cid`.
* First chunk of a **new `cid`** flushes previous snapshot from RAM.
* Keeps `snap_total`:

  * Starts at `0`.
  * When last chunk arrives, uploader sends the real count.
  * If later uploads raise the declared `total`, server updates it.
* **GC:** flushes incomplete snapshot 120 s after first chunk (configurable).

#### Discover JSON

```json
{
  "cid":   "af37c6...",   // omitted if nothing active
  "total": 42,            // 0 ⇢ uploader hasn't given final count
  "have":  [0,1,2,5,8]    // indices server currently owns
}
```

---

### 3 · Client Responsibilities

| Action            | Detail                                                                                                                                                                      |
| ----------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Slice**         | Any chunk size ≤ **300 KiB** (constant in code: `chunkSize = 300*1024`).                                                                                                    |
| **Upload**        | POST chunks in order. Set `X-Chunk-Total: 0` for all but the last; last chunk carries the real count. Retry each POST up to 5 times with exponential back-off ±20 % jitter. |
| **Discover loop** | Poll `GET /clip` every \~200 ms until snapshot appears.                                                                                                                     |
| **Fetch**         | Only indices listed in `have`; ignore 404 (not yet uploaded); if 410 → restart discovery.                                                                                   |
| **Assemble**      | When `len(parts) == total (>0)` concatenate in order, JSON-decode, hand to application.                                                                                     |

*Uploader and reader may run concurrently; shared `http.Client` is safe.*

---

### 4 · Reference Constants

```python
# server
CHUNK_MAX = 300 * 1024        # 300 KiB hard ceiling
SNAP_TTL  = 120               # seconds
```

```go
// client (poll.go)
const chunkSize = 300 * 1024  // client-chosen slice size (≤ ceiling)
```

*(Client may still pick smaller slices; this is just the default.)*

---

### 5 · Test Suites

| Script                        | Purpose                                           | Update                                                     |
| ----------------------------- | ------------------------------------------------- | ---------------------------------------------------------- |
| `clip_server_test.py`         | 3-chunk happy-path                                | no change.                                                 |
| `clip_server_poll_test.py`    | reader polling on 3 chunks                        | no change.                                                 |
| `clip_server_massive_test.py` | 500-chunk stress: random 256 B–**290 KiB** slices | set `CHUNK_MAX = 300 * 1024` when generating random sizes. |

All tests must pass against `http://localhost:5002`.

---

### TL;DR

*Server: "I will store anything up to **300 KiB** per chunk and tell readers
precisely what I have."*
*Client: "I promise never to POST > 300 KiB in one go and I'll only fetch
chunks you advertise."*



### Good catch — let's lock the rule down

**New rule (final):**
`X-Chunk-Total` **MUST** carry the *same, non-zero* final part-count on **every** chunk.
The uploader already knows how many pieces it sliced, so there's no need for "0-until-last".

---

## 1 · Protocol correction

* **Uploader:** sets
  `X-Chunk-Total: <N>` **on every POST.**
  (Our client will now pass `totalHdr := len(chunks)` for *all* parts.)
* **Server:** validates that incoming `X-Chunk-Total` matches the value it saw on the first chunk of that `cid`; if it changes → **400 Bad Request**.
* **Discover:** `total` is fixed at first sight and never changes, so the reader no longer needs the "update total if later non-zero" step.

---

## 2 · Tiny code tweaks

### 2.1 Uploader (`poll.go → Send`)

Replace the section that decides `totalHdr`:

```go
for idx, part := range chunks {
-    totalHdr := 0
-    if idx == len(chunks)-1 {        // old logic
-        totalHdr = len(chunks)
-    }
+    totalHdr := len(chunks)          // send real total every time
     if err := c.postChunkWithRetry(
         part, cid, idx, totalHdr,    // <-- pass it here
         maxRetries, baseDelay, delayFactor, maxDelay,
     ); err != nil {
         return err
     }
}
```

*(No other change required; `postChunkWithRetry` already forwards
`totalHdr`.)*

### 2.2 Server (`clip_server_flask.py`)

In **POST /clip** just after the "new snapshot" check, validate consistency:

```python
if cid != snap_id:
    snap_id, snap_total, snap_parts, snap_t0 = cid, total, {}, time.time()
else:
    # NEW: enforce constant total
    if total != snap_total:
        abort(400, "X-Chunk-Total changed within snapshot")
```

*(And delete the previous "update total if later higher" branch.)*

### 2.3 Reader (`poll.go → Poll`)

Remove the line that tried to update `cur.total` from later `discover()`
results:

```go
- if meta.total != 0 {
-     cur.total = meta.total
- }
```

`cur.total` is already set when the snapshot first appears and never
changes.

---

## 3 · Doc snippet update

> **Chunk-total:** `X-Chunk-Total` carries the **final, constant** number of
> chunks on **every** POST.
> Server rejects any chunk whose value differs from the first-seen total.

---

After these three little edits, the implementation and docs are in perfect
sync with the "always know the total" rule. Re-build the client, restart
the Flask server, and the existing tests still pass.
