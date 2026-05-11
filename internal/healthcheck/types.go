// Package healthcheck runs periodic probes against configured services
// and publishes status changes through a single in-process hub.
package healthcheck

// HostStatus enumerates the possible probe outcomes for a single host.
type HostStatus string

const (
	StatusUp   HostStatus = "up"
	StatusDown HostStatus = "down"
	StatusNA   HostStatus = "n/a"
)

// ServiceStatus is the status pair for a service's primary and alternate hosts.
type ServiceStatus struct {
	Primary HostStatus `json:"primary"`
	Alt     HostStatus `json:"alt"`
}

// StatusMap maps service IDs to their current ServiceStatus snapshot.
type StatusMap map[int64]ServiceStatus

// Equal returns true when both maps contain the same service IDs with
// identical status pairs.
func (m StatusMap) Equal(other StatusMap) bool {
	if len(m) != len(other) {
		return false
	}
	for k, v := range m {
		if o, ok := other[k]; !ok || o != v {
			return false
		}
	}
	return true
}

// Service is the minimal projection of a service row the worker needs.
type Service struct {
	ID               int64
	HostPrimary      string
	PortPrimary      *int
	HostAlt          *string
	PortAlt          *int
	PingPrimary      bool
	PingAlt          bool
	HCPrimaryEnabled bool
	HCPrimaryURL     *string
	HCAltEnabled     bool
	HCAltURL         *string
}
