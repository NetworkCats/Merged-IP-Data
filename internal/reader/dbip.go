package reader

import (
	"net/netip"

	"merged-ip-data/internal/config"
)

// DBIPCityRecord represents a record from DB-IP City database
type DBIPCityRecord struct {
	City        string   `maxminddb:"city"`
	CountryCode string   `maxminddb:"country_code"`
	Latitude    *float32 `maxminddb:"latitude"`
	Longitude   *float32 `maxminddb:"longitude"`
	Postcode    string   `maxminddb:"postcode"`
	State1      string   `maxminddb:"state1"` // Primary subdivision (e.g., state/province)
	State2      string   `maxminddb:"state2"` // Secondary subdivision
	Timezone    string   `maxminddb:"timezone"`
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
func (r *DBIPCityReader) Lookup(ip netip.Addr) (*DBIPCityRecord, error) {
	var record DBIPCityRecord
	var err error

	if ip = ip.Unmap(); ip.Is4() {
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
func (r *DBIPCityReader) LookupNetwork(ip netip.Addr) (netip.Prefix, *DBIPCityRecord, bool, error) {
	var record DBIPCityRecord
	var network netip.Prefix
	var ok bool
	var err error

	if ip = ip.Unmap(); ip.Is4() {
		network, ok, err = r.ipv4Reader.LookupNetwork(ip, &record)
	} else {
		network, ok, err = r.ipv6Reader.LookupNetwork(ip, &record)
	}

	if err != nil {
		return netip.Prefix{}, nil, false, err
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
	return r.CountryCode != "" ||
		r.City != "" ||
		r.Postcode != "" ||
		r.State1 != "" ||
		r.State2 != "" ||
		r.HasLocationData()
}

// HasLocationData checks if the record has any location data.
func (r *DBIPCityRecord) HasLocationData() bool {
	return r.HasCoordinates() || r.Timezone != ""
}

// HasCoordinates checks whether latitude and longitude were present in the DB.
func (r *DBIPCityRecord) HasCoordinates() bool {
	return r.Latitude != nil && r.Longitude != nil
}

// Coordinates returns latitude, longitude, and whether both were present.
func (r *DBIPCityRecord) Coordinates() (float64, float64, bool) {
	if !r.HasCoordinates() {
		return 0, 0, false
	}
	return float64(*r.Latitude), float64(*r.Longitude), true
}

// Reset clears all fields for reuse, reducing allocations
func (r *DBIPCityRecord) Reset() {
	r.City = ""
	r.CountryCode = ""
	r.Latitude = nil
	r.Longitude = nil
	r.Postcode = ""
	r.State1 = ""
	r.State2 = ""
	r.Timezone = ""
}
