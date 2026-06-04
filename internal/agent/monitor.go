package agent

import (
	"context"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/Bartis-Dev/LabExtend/internal/config"
	pb "github.com/Bartis-Dev/LabExtend/internal/grpc/pb"
)

// eventEmitter is the callback the agent uses to push unsolicited events
// (container reports, log batches, backup progress) onto the active gRPC
// stream. Nil when no stream is currently connected.
type eventEmitter func(*pb.AgentMessage) error

// monitor orchestrates container + log shipping for the agent. Wired by
// grpc_client.go: Start(ctx, send) is called once the agent's bidi stream is
// established, Stop() is called when the stream closes.
type monitor struct {
	cfg    *config.Config
	docker *dockerClient

	containers *containerCollector
	logs       *logCollector

	mu     sync.Mutex
	cancel context.CancelFunc
	wg     sync.WaitGroup
	emit   eventEmitter // set in Start, cleared in Stop
}

func newMonitor(cfg *config.Config) *monitor {
	socket := dockerSocketPath()
	d := newDockerClient(socket)
	return &monitor{
		cfg:        cfg,
		docker:     d,
		containers: newContainerCollector(d, 5*time.Second),
		logs:       newLogCollector(d),
	}
}

// Start spawns the goroutines. Idempotent — calling twice without Stop is a
// no-op the second time. The send func is stored so other agent components
// (e.g. the backup runner) can publish events too.
func (m *monitor) Start(parent context.Context, send func(*pb.AgentMessage) error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cancel != nil {
		return
	}

	m.emit = send

	ctx, cancel := context.WithCancel(parent)
	m.cancel = cancel

	if !m.docker.Available(ctx) {
		slog.Warn("monitor: docker socket not reachable, container monitoring disabled",
			"socket", m.docker.SocketPath())
		return
	}
	slog.Info("monitor: started", "socket", m.docker.SocketPath())

	m.wg.Add(2)
	go func() {
		defer m.wg.Done()
		m.containers.Run(ctx, send, func(current map[string]containerSummary) {
			m.logs.Sync(ctx, current)
		})
	}()
	go func() {
		defer m.wg.Done()
		m.logs.Run(ctx, send)
	}()
}

// Stop cancels and waits for goroutines.
func (m *monitor) Stop() {
	m.mu.Lock()
	cancel := m.cancel
	m.cancel = nil
	m.emit = nil
	m.mu.Unlock()

	if cancel == nil {
		return
	}
	cancel()
	m.wg.Wait()
}

// eventEmitter returns the current emit function (or nil if not connected).
// Safe for concurrent calls.
func (m *monitor) eventEmitter() eventEmitter {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.emit
}

// SetLogsEnabled forwards to the log collector.
func (m *monitor) SetLogsEnabled(containerID string, enabled bool) {
	m.logs.SetDisabled(containerID, !enabled)
}

// dockerSocketPath returns the most likely Docker socket. Inside our agent
// container the host socket is bind-mounted at /host/var/run/docker.sock
// (per docker-stack.yml); on a bare host it's /var/run/docker.sock.
// BPM_DOCKER_SOCKET overrides this.
func dockerSocketPath() string {
	if v := os.Getenv("BPM_DOCKER_SOCKET"); v != "" {
		return v
	}
	for _, candidate := range []string{
		"/host/var/run/docker.sock",
		"/var/run/docker.sock",
	} {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return "/var/run/docker.sock"
}
