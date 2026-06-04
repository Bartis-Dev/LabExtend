//go:build !linux

package agent

import (
	"runtime"

	pb "github.com/Bartis-Dev/LabExtend/internal/grpc/pb"
)

// hostCollector stub for non-Linux. Production agents always run on Linux;
// this exists so the IDE on Windows/macOS can resolve symbols and so
// cross-compile is possible.
type hostCollector struct{}

func newHostCollector() *hostCollector { return &hostCollector{} }

func (c *hostCollector) Sample() *pb.Heartbeat {
	return &pb.Heartbeat{CpuCores: uint32(runtime.NumCPU())}
}
