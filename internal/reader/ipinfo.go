package reader

import (
	"net"
	"strconv"
	"strings"

	"merged-ip-data/internal/config"
)

// IPinfoLiteRecord represents a record from IPinfo Lite database
type IPinfoLiteRecord struct {
	ASDomain      string `maxminddb:"as_domain"`
	ASName        string `maxminddb:"as_name"`
	ASN           string `maxminddb:"asn"` // Format: "AS12345"
	Continent     string `maxminddb:"continent"`
	ContinentCode string `maxminddb:"continent_code"`
	Country       string `maxminddb:"country"`
	CountryCode   string `maxminddb:"country_code"`
}

// IPinfoLiteReader reads the IPinfo Lite database
type IPinfoLiteReader struct {
	*Reader
}

// OpenIPinfoLite opens the IPinfo Lite database
func OpenIPinfoLite() (*IPinfoLiteReader, error) {
	r, err := Open(config.IPinfoLiteFile)
	if err != nil {
		return nil, err
	}
	return &IPinfoLiteReader{Reader: r}, nil
}

// Lookup looks up an IP address in the IPinfo Lite database
func (r *IPinfoLiteReader) Lookup(ip net.IP) (*IPinfoLiteRecord, error) {
	var record IPinfoLiteRecord
	err := r.Reader.Lookup(ip, &record)
	if err != nil {
		return nil, err
	}
	return &record, nil
}

// LookupNetwork looks up an IP and returns the network and record
func (r *IPinfoLiteReader) LookupNetwork(ip net.IP) (*net.IPNet, *IPinfoLiteRecord, bool, error) {
	var record IPinfoLiteRecord
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
func (r *IPinfoLiteRecord) HasASN() bool {
	return r.ASN != ""
}

// HasGeoData checks if the record has geographic data
func (r *IPinfoLiteRecord) HasGeoData() bool {
	return r.CountryCode != ""
}

// GetASNumber extracts the numeric ASN from the "AS12345" format
func (r *IPinfoLiteRecord) GetASNumber() uint32 {
	if r.ASN == "" {
		return 0
	}
	asnStr := strings.TrimPrefix(r.ASN, "AS")
	asn, err := strconv.ParseUint(asnStr, 10, 32)
	if err != nil {
		return 0
	}
	return uint32(asn)
}
