package reader

import (
	"net"

	"merged-ip-data/internal/config"
)

// DBIPCityRecord represents a record from DB-IP City database
type DBIPCityRecord struct {
	City        string  `maxminddb:"city"`
	CountryCode string  `maxminddb:"country_code"`
	Latitude    float32 `maxminddb:"latitude"`
	Longitude   float32 `maxminddb:"longitude"`
	Postcode    string  `maxminddb:"postcode"`
	State1      string  `maxminddb:"state1"` // Primary subdivision (e.g., state/province)
	State2      string  `maxminddb:"state2"` // Secondary subdivision
	Timezone    string  `maxminddb:"timezone"`
}

// DBIPCityReader reads the DB-IP City databases (both IPv4 and IPv6)
type DBIPCityReader struct {
	ipv4Reader *Reader
	ipv6Reader *Reader
}

// OpenDBIPCity opens both DB-IP City databases (IPv4 and IPv6)
func OpenDBIPCity() (*DBIPCityReader, error) {
	ipv4, err := Open(config.DBIPCityIPv4File)
	if err != nil {
		return nil, err
	}

	ipv6, err := Open(config.DBIPCityIPv6File)
	if err != nil {
		ipv4.Close()
		return nil, err
	}

	return &DBIPCityReader{
		ipv4Reader: ipv4,
		ipv6Reader: ipv6,
	}, nil
}

// Close closes both database readers
func (r *DBIPCityReader) Close() error {
	var err error
	if r.ipv4Reader != nil {
		if e := r.ipv4Reader.Close(); e != nil {
			err = e
		}
	}
	if r.ipv6Reader != nil {
		if e := r.ipv6Reader.Close(); e != nil && err == nil {
			err = e
		}
	}
	return err
}

// Lookup looks up an IP address in the appropriate DB-IP database
func (r *DBIPCityReader) Lookup(ip net.IP) (*DBIPCityRecord, error) {
	var record DBIPCityRecord
	var err error

	if ip.To4() != nil {
		err = r.ipv4Reader.Lookup(ip, &record)
	} else {
		err = r.ipv6Reader.Lookup(ip, &record)
	}

	if err != nil {
		return nil, err
	}
	return &record, nil
}

// LookupNetwork looks up an IP and returns the network and record
func (r *DBIPCityReader) LookupNetwork(ip net.IP) (*net.IPNet, *DBIPCityRecord, bool, error) {
	var record DBIPCityRecord
	var network *net.IPNet
	var ok bool
	var err error

	if ip.To4() != nil {
		network, ok, err = r.ipv4Reader.LookupNetwork(ip, &record)
	} else {
		network, ok, err = r.ipv6Reader.LookupNetwork(ip, &record)
	}

	if err != nil {
		return nil, nil, false, err
	}
	if !ok {
		return network, nil, false, nil
	}
	return network, &record, true, nil
}

// IPv4Reader returns the IPv4 database reader for iteration
func (r *DBIPCityReader) IPv4Reader() *Reader {
	return r.ipv4Reader
}

// IPv6Reader returns the IPv6 database reader for iteration
func (r *DBIPCityReader) IPv6Reader() *Reader {
	return r.ipv6Reader
}

// HasGeoData checks if the record has meaningful geographic data
func (r *DBIPCityRecord) HasGeoData() bool {
	return r.CountryCode != "" || r.City != ""
}

// HasLocationData checks if the record has coordinate data.
// Note: (0,0) is a valid coordinate but extremely rare in real IP data.
func (r *DBIPCityRecord) HasLocationData() bool {
	return r.Latitude != 0 || r.Longitude != 0 || r.Timezone != ""
}

// Reset clears all fields for reuse, reducing allocations
func (r *DBIPCityRecord) Reset() {
	r.City = ""
	r.CountryCode = ""
	r.Latitude = 0
	r.Longitude = 0
	r.Postcode = ""
	r.State1 = ""
	r.State2 = ""
	r.Timezone = ""
}
