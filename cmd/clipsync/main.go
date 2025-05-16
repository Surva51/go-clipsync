package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"time"

	"clipsync/internal"
	"clipsync/internal/clip"
	netw "clipsync/internal/net"

	"github.com/google/uuid"
)

/*──────── pretty helpers ───────────────────────────────────────*/
var (
	icSend  = "↗"
	icRecv  = "🛰 "
	icLocal = "🖳"
)

func ts() string { return time.Now().Format("15:04:05.000") }

/*──────────────────────── main ─────────────────────────────────*/
func main() {
	/* CLI flags */
	srv := flag.String("http", "http://localhost:5002/clip", "endpoint")
	key := flag.String("key", "your-secret-key-here", "shared secret")
	poll := flag.Int("interval", 200, "poll interval ms")
	trans := flag.String("transport", "poll", "poll | ws")
	postTO := flag.Duration("timeout", 15*time.Second, "HTTP POST timeout")
	flag.Parse()

	myID := uuid.NewString()[:8]

	/* network client */
	var cli netw.Client
	var err error
	if *trans == "ws" {
		cli, err = netw.NewWS(*srv, myID, *key)
	} else {
		cli, err = netw.NewHTTP(*srv, myID, *key, *postTO)
	}
	if err != nil {
		log.Fatalf("net client: %v", err)
	}

	log.Printf("🎬 clipsync id=%s  srv=%s  %s  poll=%d ms",
		myID, *srv, *trans, *poll)

	/* clipboard goroutine */
	cbCh := clip.StartThread()

	/* channels */
	toUp := make(chan internal.Snapshot, 8)
	fromSrv := make(chan internal.Snapshot, 8)

	/* watcher */
	go watcher(cbCh, toUp, time.Duration(*poll)*time.Millisecond, myID)

	/* uploader */
	go func() {
		for s := range toUp {
			start := time.Now()
			if err := cli.Send(s); err != nil {
				log.Printf("%s %s send error: %v", ts(), icSend, err)
			} else {
				el := time.Since(start).Milliseconds()
				log.Printf("%s %s sent snapshot  %d items (%d ms)",
					ts(), icSend, len(s.Items), el)
			}
		}
	}()

	/* poller */
	ctx, cancel := context.WithCancel(context.Background())
	go cli.Poll(ctx, fromSrv)
	go poller(cbCh, fromSrv, myID)

	/* Ctrl-C shutdown */
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	<-sig
	log.Println("⏻  shutting down…")
	cancel()
	time.Sleep(300 * time.Millisecond)
}

/*──────── watcher (local → send, seq-based) ───────────────────*/
func watcher(cbCh chan<- clip.Req,
	out chan<- internal.Snapshot,
	interval time.Duration, myID string) {

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	lastSeq := clip.GetSeq() // cheap kernel counter
	var lastQuick string

	for range ticker.C {
		seq := clip.GetSeq()
		if seq == lastSeq {
			continue // clipboard unchanged
		}
		lastSeq = seq

		items, err := askClipboard(cbCh) // opens clipboard only now
		if err != nil || len(items) == 0 {
			continue // sentinel / unsupported
		}

		qk := internal.QuickKey(items)
		if qk == lastQuick { // duplicate user copy
			continue
		}
		lastQuick = qk

		log.Printf("%s %s local → %s (%d items)",
			ts(), icLocal, items[0].Fmt, len(items))

		out <- internal.Snapshot{
			Origin: myID,
			TS:     time.Now().Unix(),
			Items:  items,
		}
	}
}

/*──────── poller (recv → clipboard) ───────────────────────────*/
func poller(cbCh chan<- clip.Req, in <-chan internal.Snapshot, myID string) {
	var lastRemoteQuick string

	for snap := range in {
		qk := internal.QuickKey(snap.Items)
		if qk == lastRemoteQuick {
			continue
		}
		lastRemoteQuick = qk

		reply := make(chan clip.Resp, 1)
		cbCh <- clip.Req{Kind: clip.ReqWrite, WriteData: snap.Items, Resp: reply}
		if err := (<-reply).Err; err != nil {
			log.Printf("%s clipboard write: %v", ts(), err)
		} else {
			log.Printf("%s %s remote ← %s (%d items)",
				ts(), icRecv, snap.Items[0].Fmt, len(snap.Items))
		}
	}
}

/*──────── helper: ask clipboard thread ─────────────────────────*/
func askClipboard(cbCh chan<- clip.Req) ([]internal.Item, error) {
	reply := make(chan clip.Resp, 1)
	cbCh <- clip.Req{Kind: clip.ReqRead, Resp: reply}
	r := <-reply
	return r.Items, r.Err
}
