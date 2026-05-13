package ddns

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// Worker periodically iterates over the auto-update list, fetches the
// current public IP for each record's address family, and pushes an
// update to Cloudflare when the IP has changed.
//
// The worker keeps the most-recently fetched IPs in memory across ticks
// so a passive sync (IP unchanged) doesn't call the provider at all.
type Worker struct {
	Store    *Store
	Interval time.Duration

	mu       sync.Mutex
	cancel   context.CancelFunc
	stopped  chan struct{}
}

func NewWorker(s *Store, interval time.Duration) *Worker {
	if interval < time.Minute {
		interval = time.Minute
	}
	return &Worker{Store: s, Interval: interval}
}

func (w *Worker) Run(ctx context.Context) {
	w.mu.Lock()
	ctx, w.cancel = context.WithCancel(ctx)
	w.stopped = make(chan struct{})
	w.mu.Unlock()

	defer close(w.stopped)
	t := time.NewTicker(w.Interval)
	defer t.Stop()
	// Run once immediately so the user gets fast feedback after enabling
	// the first auto-update.
	w.runOnce(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			w.runOnce(ctx)
		}
	}
}

// SetInterval restarts the worker loop with a new tick interval. Called
// when the user changes the ddns_check_interval setting.
func (w *Worker) SetInterval(d time.Duration) {
	if d < time.Minute {
		d = time.Minute
	}
	w.mu.Lock()
	w.Interval = d
	cancel := w.cancel
	w.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

func (w *Worker) runOnce(ctx context.Context) {
	updates, err := w.Store.ListAutoUpdates()
	if err != nil {
		slog.Error("ddns: list auto-updates", "err", err)
		return
	}
	if len(updates) == 0 {
		return
	}

	// Cache providers + public IPs across this tick so we don't re-fetch
	// them per record.
	providerCache := map[int64]string{} // providerID -> decrypted token
	var ipv4, ipv6 string
	var ipv4Err, ipv6Err error
	ipv4Loaded, ipv6Loaded := false, false

	for _, u := range updates {
		card, err := w.Store.GetCard(u.CardID)
		if err != nil {
			_ = w.Store.RecordSyncError(u.ID, "card not found")
			continue
		}
		token, ok := providerCache[card.ProviderID]
		if !ok {
			_, tok, err := w.Store.GetProviderToken(card.ProviderID)
			if err != nil {
				_ = w.Store.RecordSyncError(u.ID, "provider not found or undecryptable")
				continue
			}
			token = tok
			providerCache[card.ProviderID] = token
		}

		var wantIP string
		switch u.RecordType {
		case "A":
			if !ipv4Loaded {
				ipv4, ipv4Err = PublicIPv4(ctx)
				ipv4Loaded = true
			}
			if ipv4Err != nil {
				_ = w.Store.RecordSyncError(u.ID, "public ipv4: "+ipv4Err.Error())
				continue
			}
			wantIP = ipv4
		case "AAAA":
			if !ipv6Loaded {
				ipv6, ipv6Err = PublicIPv6(ctx)
				ipv6Loaded = true
			}
			if ipv6Err != nil {
				_ = w.Store.RecordSyncError(u.ID, "public ipv6: "+ipv6Err.Error())
				continue
			}
			wantIP = ipv6
		default:
			_ = w.Store.RecordSyncError(u.ID, "unsupported record type")
			continue
		}

		if u.LastSyncedIP != nil && *u.LastSyncedIP == wantIP {
			// Skip: nothing to do.
			continue
		}

		cf := NewCloudflare(token)
		if err := cf.PatchRecordContent(ctx, card.RemoteID, u.RecordRemoteID, wantIP); err != nil {
			_ = w.Store.RecordSyncError(u.ID, err.Error())
			slog.Warn("ddns: record patch failed", "card", card.Name, "record", u.RecordName, "err", err)
			continue
		}
		_ = w.Store.RecordSyncSuccess(u.ID, wantIP)
		slog.Info("ddns: record updated", "card", card.Name, "record", u.RecordName, "ip", wantIP)
	}
}
