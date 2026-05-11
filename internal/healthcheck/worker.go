package healthcheck

import (
	"context"
	"database/sql"
	"log/slog"
	"sync"
	"time"
)

// Worker periodically probes all services and pushes status maps to a Hub.
type Worker struct {
	DB       *sql.DB
	Hub      *Hub
	Interval time.Duration

	mu       sync.Mutex
	override time.Duration
}

// SetInterval changes the probe cadence at runtime. Zero or negative
// values are ignored.
func (w *Worker) SetInterval(d time.Duration) {
	if d <= 0 {
		return
	}
	w.mu.Lock()
	w.override = d
	w.mu.Unlock()
}

func (w *Worker) currentInterval() time.Duration {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.override > 0 {
		return w.override
	}
	return w.Interval
}

// Run blocks until ctx is cancelled, ticking once per interval and
// publishing fresh status snapshots through the Hub. The first tick runs
// immediately so the dashboard does not display "n/a" for an entire
// interval after boot.
func (w *Worker) Run(ctx context.Context) {
	w.tick(ctx)
	for {
		timer := time.NewTimer(w.currentInterval())
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
			w.tick(ctx)
		}
	}
}

func (w *Worker) tick(ctx context.Context) {
	services, err := loadServices(w.DB)
	if err != nil {
		slog.Warn("healthcheck: load services", "err", err)
		return
	}
	tickCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	status := probeAll(tickCtx, services)
	w.Hub.Publish(status)
}

// probeAll runs primary and alt probes for each service with bounded
// concurrency so a large dashboard can not spawn thousands of goroutines.
func probeAll(ctx context.Context, services []Service) StatusMap {
	sem := make(chan struct{}, 16)
	out := make(StatusMap, len(services))
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, svc := range services {
		svc := svc
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			ss := probeOne(ctx, svc)
			mu.Lock()
			out[svc.ID] = ss
			mu.Unlock()
		}()
	}
	wg.Wait()
	return out
}

func probeOne(ctx context.Context, svc Service) ServiceStatus {
	var ss ServiceStatus
	ss.Primary = probeHost(ctx, svc.HostPrimary, svc.PortPrimary, svc.PingPrimary, svc.HCPrimaryEnabled, svc.HCPrimaryURL)
	if svc.HostAlt != nil && *svc.HostAlt != "" {
		ss.Alt = probeHost(ctx, *svc.HostAlt, svc.PortAlt, svc.PingAlt, svc.HCAltEnabled, svc.HCAltURL)
	} else {
		ss.Alt = StatusNA
	}
	return ss
}

func probeHost(ctx context.Context, host string, port *int, ping, hc bool, hcURL *string) HostStatus {
	if !ping && !hc {
		return StatusNA
	}
	if hc {
		target := ""
		if hcURL != nil && *hcURL != "" {
			target = *hcURL
		} else {
			target = DeriveHealthcheckURL(host, port)
		}
		if HTTPProbe(ctx, target) == StatusUp {
			return StatusUp
		}
		if !ping {
			return StatusDown
		}
	}
	p := 0
	if port != nil {
		p = *port
	}
	return TCPProbe(ctx, host, p)
}

func loadServices(db *sql.DB) ([]Service, error) {
	rows, err := db.Query(`SELECT
		id, host_primary, port_primary, host_alt, port_alt,
		ping_primary, ping_alt,
		hc_primary_enabled, hc_primary_url, hc_alt_enabled, hc_alt_url
		FROM services`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Service
	for rows.Next() {
		var s Service
		var pingP, pingA, hcPE, hcAE int
		if err := rows.Scan(
			&s.ID, &s.HostPrimary, &s.PortPrimary, &s.HostAlt, &s.PortAlt,
			&pingP, &pingA, &hcPE, &s.HCPrimaryURL, &hcAE, &s.HCAltURL,
		); err != nil {
			return nil, err
		}
		s.PingPrimary = pingP == 1
		s.PingAlt = pingA == 1
		s.HCPrimaryEnabled = hcPE == 1
		s.HCAltEnabled = hcAE == 1
		out = append(out, s)
	}
	return out, rows.Err()
}
