package reader

import (
	"net"

	"merged-ip-data/internal/config"
)

// GeoWhoisCountryRecord represents a record from the GeoLite2-Geo-Whois-ASN-Country database
type GeoWhoisCountryRecord struct {
	CountryCode string `maxminddb:"country_code"`
}

// GeoWhoisCountryReader reads the GeoLite2-Geo-Whois-ASN-Country database
type GeoWhoisCountryReader struct {
	*Reader
}

// OpenGeoWhoisCountry opens the GeoLite2-Geo-Whois-ASN-Country database
func OpenGeoWhoisCountry() (*GeoWhoisCountryReader, error) {
	r, err := Open(config.GeoWhoisCountryFile)
	if err != nil {
		return nil, err
	}
	return &GeoWhoisCountryReader{Reader: r}, nil
}

// Lookup looks up an IP address in the GeoWhois Country database
func (r *GeoWhoisCountryReader) Lookup(ip net.IP) (*GeoWhoisCountryRecord, error) {
	var record GeoWhoisCountryRecord
	err := r.Reader.Lookup(ip, &record)
	if err != nil {
		return nil, err
	}
	return &record, nil
}

// LookupTo looks up an IP address into a pre-allocated record to reduce allocations
func (r *GeoWhoisCountryReader) LookupTo(ip net.IP, record *GeoWhoisCountryRecord) error {
	return r.Reader.Lookup(ip, record)
}

// LookupNetwork looks up an IP and returns the network and record
func (r *GeoWhoisCountryReader) LookupNetwork(ip net.IP) (*net.IPNet, *GeoWhoisCountryRecord, bool, error) {
	var record GeoWhoisCountryRecord
	network, ok, err := r.Reader.LookupNetwork(ip, &record)
	if err != nil {
		return nil, nil, false, err
	}
	if !ok {
		return network, nil, false, nil
	}
	return network, &record, true, nil
}

// HasCountry checks if the record has country data
func (r *GeoWhoisCountryRecord) HasCountry() bool {
	return r.CountryCode != ""
}

// Reset clears all fields for reuse, reducing allocations
func (r *GeoWhoisCountryRecord) Reset() {
	r.CountryCode = ""
}
