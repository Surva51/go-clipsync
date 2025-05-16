## **Design Plan for `internal/net` – Dual Transport (HTTP-poll + WebSocket)**

> *Goal:* add a low-latency WebSocket path **without breaking** the existing
> polling clients or any caller code (`main.go`, watcher, poller).

---

### 1 High-level architecture

```
internal/net/
├─ client.go      ← common types + helpers
├─ poll.go        ← HTTP long-poll implementation  (current logic, renamed)
└─ ws.go          ← new WebSocket implementation
```

```
          ┌──────────────┐               ┌───────────────┐
          │  clip loop   │               │    server     │
          └──────┬───────┘               │   /clip + /ws │
                 │                       └────────┬──────┘
                 ▼                                │
          ┌──────────────┐         JSON snapshots │
          │   watcher    │<──────────────────────────┘
          └──────┬───────┘
                 │ Send/Recv core.Snapshot
                 ▼
          ┌──────────────┐      one of two concrete clients
          │  net.Client  │◄────────────────┐
          └──────────────┘             │
             ▲         ▲               │
      httpClient   wsClient    (share auth helper + size guard)
```

* `net.Client` **interface** never changes:

  ```go
  type Client interface {
      Send(snap core.Snapshot) error
      Poll(ctx context.Context, out chan<- core.Snapshot)
  }
  ```
* `poll.go` holds the existing code (struct `httpClient`).
* `ws.go` adds struct `wsClient` that satisfies the same interface.

---

### 2 File-level responsibilities

| File            | Contains                                                                                              | Never does         |
| --------------- | ----------------------------------------------------------------------------------------------------- | ------------------ |
| **`client.go`** | • `Client` interface<br>• auth-token helper<br>• `bodyCap` const<br>• tiny reconnection back-off util | actual network I/O |
| **`poll.go`**   | Current `Client` struct → renamed **`httpClient`** plus `NewHTTP(…)`.                                 | WebSocket code     |
| **`ws.go`**     | `wsClient` + `NewWS(…)`<br>Handles connect, send, read loop, ping/pong, auto-reconnect with back-off. | HTTP-poll logic    |

---

### 3 Constructor table

| Function                             | Transport          | Typical URL example              |
| ------------------------------------ | ------------------ | -------------------------------- |
| `net.NewHTTP(url, id, key, timeout)` | existing long-poll | `http://host:5002/clip`          |
| `net.NewWS(url, id, key)`            | new WebSocket      | `ws://host:5003/ws` or `wss://…` |

Both return a value whose concrete type implements `net.Client`.

---

### 4 Behaviour specification for `wsClient`

| Aspect            | Design                                                                                                                                                                    |
| ----------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Dial**          | `websocket.Dial(ctx, url, requestHeader)` with the same `X-Auth-Token`.                                                                                                   |
| **Send**          | `Write(ctx, MessageText, snapshotJSON)` – 10 s per-write timeout. On error → close & return error.                                                                        |
| **Poll loop**     | A goroutine inside `Poll`:<br>`\nfor {\n  _, data, err := conn.Read(ctx)\n  if err != nil { reconnect() }\n  unmarshal → snap; if snap.Origin != ID { out <- snap }\n}\n` |
| **Reconnect**     | exponential back-off 0.5 s → 8 s; re-dial until `ctx` cancels.                                                                                                            |
| **Keep-alive**    | send `Ping` every 25 s; drop connection on 2 missed `Pong`s.                                                                                                              |
| **Payload guard** | If a received JSON blob > `bodyCap` (32 MiB) → ignore & log.                                                                                                              |
| **Shutdown**      | When `ctx.Done()` fires, send WebSocket **Close** with code 1000, wait 1 s, then return.                                                                                  |

---

### 5 Changes outside `internal/net`

| File                                   | Edit                                                                |                                                                                                                                                                                                                                           |
| -------------------------------------- | ------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| **`cmd/clipsync/main.go`**             | Add flag: <br>\`transport := flag.String("transport", "poll", "poll | ws")\`<br><br>Constructor selection: <br>`go\nvar cli net.Client\nif *transport == \"ws\" {\n    cli, err = netw.NewWS(*serverURL, myID, *sharedKey)\n} else {\n    cli, err = netw.NewHTTP(*serverURL, myID, *sharedKey, *timeout)\n}\n` |
| **Docker / systemd deploy** *(if any)* | open port **5003**; ensure proxy allows `Upgrade: websocket`.       |                                                                                                                                                                                                                                           |

---

### 6 Failure handling table

| Failure                            | httpClient reaction                                        | wsClient reaction                                |
| ---------------------------------- | ---------------------------------------------------------- | ------------------------------------------------ |
| Server not reachable               | log POST / read error, retry on next watcher tick.         | Dial failure → back-off reconnect.               |
| Idle corporate proxy closes socket | N/A                                                        | `Read` returns; reconnect immediately.           |
| Oversized snapshot                 | `Send` drops with ">32 MiB" error.                         | same.                                            |
| Wrong token / 4401 close           | returns error to caller; watcher logs and stops uploading. | reconnect will repeat and fail; eventually/same. |

---

### 7 Estimated code addition

* `ws.go` \~ 120 lines including reconnect loop.
* Flag parsing in `main.go` \~ 8 lines.
* No other files touched.

Time to implement and unit-test ≈ **2–3 hours**.

---

### 8 Roll-out plan

1. Deploy **`ws_server.py`** on port 5003 (already running).
2. Ship updated binary with default `--transport poll`; advanced users opt into WebSocket via flag.
3. Monitor logs for `WS connect`/`disconnect` counts and error rates.
4. If stable for a week, flip default to `ws`.

---

With this design, the transport swap is localised, low-risk, and fully backward-compatible.
