package reader

import (
	"net/netip"

	"merged-ip-data/internal/config"
)

// RouteViewsASNRecord represents a record from the Origin ASN database.
type RouteViewsASNRecord struct {
	AutonomousSystemNumber       uint32 `maxminddb:"autonomous_system_number"`
	AutonomousSystemOrganization string `maxminddb:"autonomous_system_organization"`
}

// RouteViewsASNReader reads the Origin ASN database.
type RouteViewsASNReader struct {
	*Reader
}

// OpenRouteViewsASN opens the Origin ASN database.
func OpenRouteViewsASN() (*RouteViewsASNReader, error) {
	r, err := Open(config.RouteViewsASNFile)
	if err != nil {
		return nil, err
	}
	return &RouteViewsASNReader{Reader: r}, nil
}

// Lookup looks up an IP address in the Origin ASN database.
func (r *RouteViewsASNReader) Lookup(ip netip.Addr) (*RouteViewsASNRecord, error) {
	var record RouteViewsASNRecord
	err := r.Reader.Lookup(ip, &record)
	if err != nil {
		return nil, err
	}
	return &record, nil
}

// LookupTo looks up an IP address into a pre-allocated record to reduce allocations
func (r *RouteViewsASNReader) LookupTo(ip netip.Addr, record *RouteViewsASNRecord) error {
	return r.Reader.Lookup(ip, record)
}

// LookupNetwork looks up an IP and returns the network and record
func (r *RouteViewsASNReader) LookupNetwork(ip netip.Addr) (netip.Prefix, *RouteViewsASNRecord, bool, error) {
	var record RouteViewsASNRecord
	network, ok, err := r.Reader.LookupNetwork(ip, &record)
	if err != nil {
		return netip.Prefix{}, nil, false, err
	}
	if !ok {
		return network, nil, false, nil
	}
	return network, &record, true, nil
}

// LookupNetworkTo looks up an IP into a pre-allocated record and returns the
// matched network. The caller should reset record before reuse.
func (r *RouteViewsASNReader) LookupNetworkTo(ip netip.Addr, record *RouteViewsASNRecord) (netip.Prefix, bool, error) {
	network, ok, err := r.Reader.LookupNetwork(ip, record)
	if err != nil {
		return netip.Prefix{}, false, err
	}
	return network, ok, nil
}

// HasASN checks if the record has ASN data
func (r *RouteViewsASNRecord) HasASN() bool {
	return r.AutonomousSystemNumber != 0
}

// Reset clears all fields for reuse, reducing allocations
func (r *RouteViewsASNRecord) Reset() {
	r.AutonomousSystemNumber = 0
	r.AutonomousSystemOrganization = ""
}
