package reader

import (
	"net"

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
func (r *GeoLite2ASNReader) Lookup(ip net.IP) (*GeoLite2ASNRecord, error) {
	var record GeoLite2ASNRecord
	err := r.Reader.Lookup(ip, &record)
	if err != nil {
		return nil, err
	}
	return &record, nil
}

// LookupTo looks up an IP address into a pre-allocated record to reduce allocations
func (r *GeoLite2ASNReader) LookupTo(ip net.IP, record *GeoLite2ASNRecord) error {
	return r.Reader.Lookup(ip, record)
}

// LookupNetwork looks up an IP and returns the network and record
func (r *GeoLite2ASNReader) LookupNetwork(ip net.IP) (*net.IPNet, *GeoLite2ASNRecord, bool, error) {
	var record GeoLite2ASNRecord
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
func (r *GeoLite2ASNRecord) HasASN() bool {
	return r.AutonomousSystemNumber != 0
}
