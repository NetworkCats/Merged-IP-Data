package reader

import (
	"net"

	"merged-ip-data/internal/config"
)

// RouteViewsASNRecord represents a record from RouteViews ASN database
type RouteViewsASNRecord struct {
	AutonomousSystemNumber       uint32 `maxminddb:"autonomous_system_number"`
	AutonomousSystemOrganization string `maxminddb:"autonomous_system_organization"`
}

// RouteViewsASNReader reads the RouteViews ASN database
type RouteViewsASNReader struct {
	*Reader
}

// OpenRouteViewsASN opens the RouteViews ASN database
func OpenRouteViewsASN() (*RouteViewsASNReader, error) {
	r, err := Open(config.RouteViewsASNFile)
	if err != nil {
		return nil, err
	}
	return &RouteViewsASNReader{Reader: r}, nil
}

// Lookup looks up an IP address in the RouteViews ASN database
func (r *RouteViewsASNReader) Lookup(ip net.IP) (*RouteViewsASNRecord, error) {
	var record RouteViewsASNRecord
	err := r.Reader.Lookup(ip, &record)
	if err != nil {
		return nil, err
	}
	return &record, nil
}

// LookupTo looks up an IP address into a pre-allocated record to reduce allocations
func (r *RouteViewsASNReader) LookupTo(ip net.IP, record *RouteViewsASNRecord) error {
	return r.Reader.Lookup(ip, record)
}

// LookupNetwork looks up an IP and returns the network and record
func (r *RouteViewsASNReader) LookupNetwork(ip net.IP) (*net.IPNet, *RouteViewsASNRecord, bool, error) {
	var record RouteViewsASNRecord
	network, ok, err := r.Reader.LookupNetwork(ip, &record)
	if err != nil {
		return nil, nil, false, err
	}
	if !ok {
		return network, nil, false, nil
	}
	return network, &record, true, nil
}

// HasASN checks if the record has ASN data
func (r *RouteViewsASNRecord) HasASN() bool {
	return r.AutonomousSystemNumber != 0
}
