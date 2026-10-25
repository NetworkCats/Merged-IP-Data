package reader

import (
	"net/netip"

	"merged-ip-data/internal/config"
)

// GeoLite2ASNRecord represents a record from GeoLite2-ASN database
type GeoLite2ASNRecord struct {
	AutonomousSystemNumber       uint32 `maxminddb:"autonomous_system_number"`
	AutonomousSystemOrganization string `maxminddb:"autonomous_system_organization"`
}

// GeoLite2ASNReader reads the GeoLite2-ASN database
type GeoLite2ASNReader struct {
	*Reader
}

// OpenGeoLite2ASN opens the GeoLite2-ASN database
func OpenGeoLite2ASN() (*GeoLite2ASNReader, error) {
	r, err := Open(config.GeoLite2ASNFile)
	if err != nil {
		return nil, err
	}
	return &GeoLite2ASNReader{Reader: r}, nil
}

// Lookup looks up an IP address in the GeoLite2-ASN database
func (r *GeoLite2ASNReader) Lookup(ip netip.Addr) (*GeoLite2ASNRecord, error) {
	var record GeoLite2ASNRecord
	err := r.Reader.Lookup(ip, &record)
	if err != nil {
		return nil, err
	}
	return &record, nil
}

// LookupTo looks up an IP address into a pre-allocated record to reduce allocations
func (r *GeoLite2ASNReader) LookupTo(ip netip.Addr, record *GeoLite2ASNRecord) error {
	return r.Reader.Lookup(ip, record)
}

// LookupNetwork looks up an IP and returns the network and record
func (r *GeoLite2ASNReader) LookupNetwork(ip netip.Addr) (netip.Prefix, *GeoLite2ASNRecord, bool, error) {
	var record GeoLite2ASNRecord
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
func (r *GeoLite2ASNReader) LookupNetworkTo(ip netip.Addr, record *GeoLite2ASNRecord) (netip.Prefix, bool, error) {
	network, ok, err := r.Reader.LookupNetwork(ip, record)
	if err != nil {
		return netip.Prefix{}, false, err
	}
	return network, ok, nil
}

// HasASN checks if the record has ASN data
func (r *GeoLite2ASNRecord) HasASN() bool {
	return r.AutonomousSystemNumber != 0
}

// Reset clears all fields for reuse, reducing allocations
func (r *GeoLite2ASNRecord) Reset() {
	r.AutonomousSystemNumber = 0
	r.AutonomousSystemOrganization = ""
}
