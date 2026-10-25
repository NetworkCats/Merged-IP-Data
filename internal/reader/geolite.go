package reader

import (
	"net/netip"

	"merged-ip-data/internal/config"
)

// GeoLite2CityRecord represents a record from GeoLite2-City database
type GeoLite2CityRecord struct {
	City struct {
		GeonameID uint32            `maxminddb:"geoname_id"`
		Names     map[string]string `maxminddb:"names"`
	} `maxminddb:"city"`
	Continent struct {
		Code      string            `maxminddb:"code"`
		GeonameID uint32            `maxminddb:"geoname_id"`
		Names     map[string]string `maxminddb:"names"`
	} `maxminddb:"continent"`
	Country struct {
		GeonameID uint32            `maxminddb:"geoname_id"`
		ISOCode   string            `maxminddb:"iso_code"`
		Names     map[string]string `maxminddb:"names"`
	} `maxminddb:"country"`
	Location struct {
		AccuracyRadius uint16   `maxminddb:"accuracy_radius"`
		Latitude       *float64 `maxminddb:"latitude"`
		Longitude      *float64 `maxminddb:"longitude"`
		MetroCode      uint16   `maxminddb:"metro_code"`
		TimeZone       string   `maxminddb:"time_zone"`
	} `maxminddb:"location"`
	Postal struct {
		Code string `maxminddb:"code"`
	} `maxminddb:"postal"`
	RegisteredCountry struct {
		GeonameID uint32            `maxminddb:"geoname_id"`
		ISOCode   string            `maxminddb:"iso_code"`
		Names     map[string]string `maxminddb:"names"`
	} `maxminddb:"registered_country"`
	Subdivisions []struct {
		GeonameID uint32            `maxminddb:"geoname_id"`
		ISOCode   string            `maxminddb:"iso_code"`
		Names     map[string]string `maxminddb:"names"`
	} `maxminddb:"subdivisions"`
}

// GeoLite2CityReader reads the GeoLite2-City database
type GeoLite2CityReader struct {
	*Reader
}

// OpenGeoLite2City opens the GeoLite2-City database
func OpenGeoLite2City() (*GeoLite2CityReader, error) {
	r, err := Open(config.GeoLite2CityFile)
	if err != nil {
		return nil, err
	}
	return &GeoLite2CityReader{Reader: r}, nil
}

// Lookup looks up an IP address in the GeoLite2-City database
func (r *GeoLite2CityReader) Lookup(ip netip.Addr) (*GeoLite2CityRecord, error) {
	var record GeoLite2CityRecord
	err := r.Reader.Lookup(ip, &record)
	if err != nil {
		return nil, err
	}
	return &record, nil
}

// LookupTo looks up an IP address into a pre-allocated record to reduce allocations
func (r *GeoLite2CityReader) LookupTo(ip netip.Addr, record *GeoLite2CityRecord) error {
	return r.Reader.Lookup(ip, record)
}

// LookupNetwork looks up an IP and returns the network and record
func (r *GeoLite2CityReader) LookupNetwork(ip netip.Addr) (netip.Prefix, *GeoLite2CityRecord, bool, error) {
	var record GeoLite2CityRecord
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
// effective network containing that record.
func (r *GeoLite2CityReader) LookupNetworkTo(ip netip.Addr, record *GeoLite2CityRecord) (netip.Prefix, bool, error) {
	network, ok, err := r.Reader.LookupNetwork(ip, record)
	if err != nil {
		return netip.Prefix{}, false, err
	}
	return network, ok, nil
}

// HasGeoData checks if the record has meaningful geographic data
func (r *GeoLite2CityRecord) HasGeoData() bool {
	return r.City.GeonameID != 0 ||
		hasStringMapData(r.City.Names) ||
		r.Continent.Code != "" ||
		r.Continent.GeonameID != 0 ||
		hasStringMapData(r.Continent.Names) ||
		r.Country.GeonameID != 0 ||
		r.Country.ISOCode != "" ||
		hasStringMapData(r.Country.Names) ||
		r.HasLocationData() ||
		r.Postal.Code != "" ||
		r.RegisteredCountry.GeonameID != 0 ||
		r.RegisteredCountry.ISOCode != "" ||
		hasStringMapData(r.RegisteredCountry.Names) ||
		hasSubdivisionData(r.Subdivisions)
}

// HasPrimaryGeoData checks if the record has the primary city/country fields
// used to decide whether a lower-priority geo source should be skipped.
func (r *GeoLite2CityRecord) HasPrimaryGeoData() bool {
	return r.City.GeonameID != 0 ||
		hasStringMapData(r.City.Names) ||
		r.Country.GeonameID != 0 ||
		r.Country.ISOCode != "" ||
		hasStringMapData(r.Country.Names)
}

// HasLocationData checks if the record has any location data.
func (r *GeoLite2CityRecord) HasLocationData() bool {
	return r.Location.AccuracyRadius != 0 || r.HasCoordinates() ||
		r.Location.MetroCode != 0 || r.Location.TimeZone != ""
}

// HasCoordinates checks whether latitude and longitude were present in the DB.
func (r *GeoLite2CityRecord) HasCoordinates() bool {
	return r.Location.Latitude != nil && r.Location.Longitude != nil
}

// Coordinates returns latitude, longitude, and whether both were present.
func (r *GeoLite2CityRecord) Coordinates() (float64, float64, bool) {
	if !r.HasCoordinates() {
		return 0, 0, false
	}
	return *r.Location.Latitude, *r.Location.Longitude, true
}

// Reset clears all fields for reuse, reducing allocations
func (r *GeoLite2CityRecord) Reset() {
	r.City.GeonameID = 0
	r.City.Names = nil
	r.Continent.Code = ""
	r.Continent.GeonameID = 0
	r.Continent.Names = nil
	r.Country.GeonameID = 0
	r.Country.ISOCode = ""
	r.Country.Names = nil
	r.Location.AccuracyRadius = 0
	r.Location.Latitude = nil
	r.Location.Longitude = nil
	r.Location.MetroCode = 0
	r.Location.TimeZone = ""
	r.Postal.Code = ""
	r.RegisteredCountry.GeonameID = 0
	r.RegisteredCountry.ISOCode = ""
	r.RegisteredCountry.Names = nil
	r.Subdivisions = nil
}

func hasStringMapData(names map[string]string) bool {
	for lang, name := range names {
		if lang != "" && name != "" {
			return true
		}
	}
	return false
}

func hasSubdivisionData(subdivisions []struct {
	GeonameID uint32            `maxminddb:"geoname_id"`
	ISOCode   string            `maxminddb:"iso_code"`
	Names     map[string]string `maxminddb:"names"`
}) bool {
	for _, sub := range subdivisions {
		if sub.GeonameID != 0 || sub.ISOCode != "" || hasStringMapData(sub.Names) {
			return true
		}
	}
	return false
}
