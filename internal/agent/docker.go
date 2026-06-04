// Minimal Docker Engine API client over the unix socket. Only the endpoints
// the monitor needs:
//   - GET /containers/json?all=1
//   - GET /containers/{id}/json
//   - GET /containers/{id}/stats?stream=0
//   - GET /containers/{id}/logs?follow=1&stdout=1&stderr=1&since=...&timestamps=1
//
// We avoid the official docker/docker SDK because it pulls in ~1MB of
// dependencies (containerd/Docker buildkit), which makes for a heavier binary
// and longer compile. The endpoints we need are stable and trivially small.

package agent

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// dockerClient talks to the Docker Engine API over a unix socket.
type dockerClient struct {
	socketPath string
	http       *http.Client
}

// newDockerClient binds to a socket path. Inside the bpm container the host
// socket is bind-mounted at /host/var/run/docker.sock (see docker-stack.yml);
// outside the container it's typically /var/run/docker.sock.
func newDockerClient(socketPath string) *dockerClient {
	tr := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			d := &net.Dialer{Timeout: 3 * time.Second}
			return d.DialContext(ctx, "unix", socketPath)
		},
	}
	return &dockerClient{
		socketPath: socketPath,
		http: &http.Client{
			Transport: tr,
			Timeout:   0, // log streams are long-lived; per-request timeout via ctx
		},
	}
}

// SocketPath returns the configured socket path (for logging).
func (c *dockerClient) SocketPath() string { return c.socketPath }

// Available pings the engine. Returns true if reachable.
func (c *dockerClient) Available(ctx context.Context) bool {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://docker/_ping", nil)
	resp, err := c.http.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// containerSummary is the slim shape we need from /containers/json.
type containerSummary struct {
	ID      string            `json:"Id"`
	Names   []string          `json:"Names"`
	Image   string            `json:"Image"`
	State   string            `json:"State"`  // running | exited | restarting | etc
	Status  string            `json:"Status"` // human "Up 3 hours", "Exited (0) 5 mins ago"
	Labels  map[string]string `json:"Labels"`
	Created int64             `json:"Created"`
}

// inspect is the slim shape we need from /containers/{id}/json.
type inspect struct {
	ID    string `json:"Id"`
	Name  string `json:"Name"`
	State struct {
		Status     string `json:"Status"`
		StartedAt  string `json:"StartedAt"`
		FinishedAt string `json:"FinishedAt"`
		ExitCode   int    `json:"ExitCode"`
		Restarting bool   `json:"Restarting"`
		Health     *struct {
			Status string `json:"Status"`
		} `json:"Health,omitempty"`
	} `json:"State"`
	Config struct {
		Tty    bool              `json:"Tty"`
		Labels map[string]string `json:"Labels"`
		Image  string            `json:"Image"`
	} `json:"Config"`
	HostConfig struct {
		Memory int64 `json:"Memory"`
	} `json:"HostConfig"`
	RestartCount int    `json:"RestartCount"`
	Image        string `json:"Image"`
}

// stats is the slim shape we need from /containers/{id}/stats?stream=0.
// Field set matches https://docs.docker.com/engine/api/v1.41/#tag/Container/operation/ContainerStats
type stats struct {
	CPUStats    statsCPU      `json:"cpu_stats"`
	PreCPUStats statsCPU      `json:"precpu_stats"`
	MemoryStats statsMemory   `json:"memory_stats"`
	Networks    map[string]netIO `json:"networks"`
	BlkIOStats  statsBlkIO    `json:"blkio_stats"`
}

type statsCPU struct {
	CPUUsage struct {
		TotalUsage uint64 `json:"total_usage"`
	} `json:"cpu_usage"`
	SystemCPUUsage uint64 `json:"system_cpu_usage"`
	OnlineCPUs     uint32 `json:"online_cpus"`
}

type statsMemory struct {
	Usage uint64 `json:"usage"`
	Limit uint64 `json:"limit"`
}

type netIO struct {
	RxBytes uint64 `json:"rx_bytes"`
	TxBytes uint64 `json:"tx_bytes"`
}

type statsBlkIO struct {
	IOServiceBytesRecursive []blkIOEntry `json:"io_service_bytes_recursive"`
}

type blkIOEntry struct {
	Op    string `json:"op"`
	Value uint64 `json:"value"`
}

// ListContainers returns all containers (running + stopped).
func (c *dockerClient) ListContainers(ctx context.Context) ([]containerSummary, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://docker/containers/json?all=1", nil)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("list containers: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("list containers: status %d", resp.StatusCode)
	}
	var out []containerSummary
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode containers: %w", err)
	}
	return out, nil
}

// Inspect returns detail for one container.
func (c *dockerClient) Inspect(ctx context.Context, id string) (*inspect, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://docker/containers/"+url.PathEscape(id)+"/json", nil)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("inspect: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("inspect: status %d", resp.StatusCode)
	}
	var out inspect
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode inspect: %w", err)
	}
	return &out, nil
}

// Stats returns a single (non-streaming) stats snapshot.
func (c *dockerClient) Stats(ctx context.Context, id string) (*stats, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://docker/containers/"+url.PathEscape(id)+"/stats?stream=0", nil)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("stats: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("stats: status %d", resp.StatusCode)
	}
	var out stats
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode stats: %w", err)
	}
	return &out, nil
}

// Logs opens a follow=1 log stream starting from `since` (zero = from beginning,
// last hour cap to avoid replaying old containers). Caller MUST Close the body
// when done. The body is Docker's multiplex stream when Tty=false, or raw
// bytes when Tty=true; use openLogsReader to consume it.
func (c *dockerClient) Logs(ctx context.Context, id string, since time.Time) (io.ReadCloser, error) {
	q := url.Values{}
	q.Set("follow", "1")
	q.Set("stdout", "1")
	q.Set("stderr", "1")
	q.Set("timestamps", "1")
	if !since.IsZero() {
		q.Set("since", strconv.FormatInt(since.Unix(), 10))
	}
	u := "http://docker/containers/" + url.PathEscape(id) + "/logs?" + q.Encode()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("logs: %w", err)
	}
	if resp.StatusCode/100 != 2 {
		resp.Body.Close()
		return nil, fmt.Errorf("logs: status %d", resp.StatusCode)
	}
	return resp.Body, nil
}

// ─── stats math helpers ─────────────────────────────────────────────────────

// CPUPercent computes the CPU percentage from a pair of (cpu, precpu) samples.
// Matches Docker's `docker stats` formula.
func (s *stats) CPUPercent() float64 {
	cpuDelta := float64(s.CPUStats.CPUUsage.TotalUsage) - float64(s.PreCPUStats.CPUUsage.TotalUsage)
	sysDelta := float64(s.CPUStats.SystemCPUUsage) - float64(s.PreCPUStats.SystemCPUUsage)
	if sysDelta <= 0 || cpuDelta < 0 {
		return 0
	}
	cores := s.CPUStats.OnlineCPUs
	if cores == 0 {
		cores = 1
	}
	return (cpuDelta / sysDelta) * float64(cores) * 100.0
}

// NetTotals sums all interfaces.
func (s *stats) NetTotals() (rx uint64, tx uint64) {
	for _, n := range s.Networks {
		rx += n.RxBytes
		tx += n.TxBytes
	}
	return
}

// BlockTotals sums Read+Write bytes from all devices.
func (s *stats) BlockTotals() (read uint64, write uint64) {
	for _, e := range s.BlkIOStats.IOServiceBytesRecursive {
		switch strings.ToLower(e.Op) {
		case "read":
			read += e.Value
		case "write":
			write += e.Value
		}
	}
	return
}

// ─── log demux ──────────────────────────────────────────────────────────────

// dockerLogHeader is the 8-byte frame header used when Tty=false.
//
//	byte 0: stream type (0=stdin,1=stdout,2=stderr)
//	bytes 1-3: reserved (zero)
//	bytes 4-7: payload length (big endian uint32)
type dockerLogHeader struct {
	Stream byte
	Length uint32
}

// readLogFrame reads exactly one demuxed frame from r. Returns the stream
// type ("stdout"/"stderr") and the raw payload bytes (may contain multiple
// lines). On EOF returns io.EOF.
func readLogFrame(r *bufio.Reader) (stream string, payload []byte, err error) {
	var hdr [8]byte
	if _, err = io.ReadFull(r, hdr[:]); err != nil {
		return "", nil, err
	}
	streamType := hdr[0]
	length := binary.BigEndian.Uint32(hdr[4:8])
	if length == 0 {
		return streamName(streamType), nil, nil
	}
	if length > 4*1024*1024 {
		return "", nil, errors.New("log frame too large (>4MB)")
	}
	payload = make([]byte, length)
	if _, err = io.ReadFull(r, payload); err != nil {
		return "", nil, err
	}
	return streamName(streamType), payload, nil
}

func streamName(t byte) string {
	if t == 2 {
		return "stderr"
	}
	return "stdout"
}

// parseLogLine splits a Docker log line "2026-06-04T12:34:56.789Z message…\n"
// into its RFC3339 timestamp and message body. If parsing fails, ts is the
// current time and msg is the original line minus trailing newline.
func parseLogLine(b []byte) (time.Time, string) {
	s := strings.TrimRight(string(b), "\r\n")
	if i := strings.IndexByte(s, ' '); i > 0 && i < 40 {
		if t, err := time.Parse(time.RFC3339Nano, s[:i]); err == nil {
			return t, s[i+1:]
		}
	}
	return time.Now(), s
}
