package reader

import (
	"net"

	"merged-ip-data/internal/config"

	"github.com/ipipdotnet/ipdb-go"
)

// QQWryRecord represents a record from the QQWry (Chunzhen) database
type QQWryRecord struct {
	CountryName   string // Country name in Chinese
	RegionName    string // Province/Region name in Chinese
	CityName      string // City name in Chinese
	DistrictName  string // District name (if available)
	ISPDomain     string // ISP/Organization name
	CountryCode   string // ISO 3166-1 country code
	ContinentCode string // Continent code
}

// QQWryReader reads the QQWry IPDB database
type QQWryReader struct {
	db *ipdb.City
}

// OpenQQWry opens the QQWry IPDB database
func OpenQQWry() (*QQWryReader, error) {
	db, err := ipdb.NewCity(config.QQWryFile)
	if err != nil {
		return nil, err
	}
	return &QQWryReader{db: db}, nil
}

// Close closes the database (no-op for ipdb, but maintains interface consistency)
func (r *QQWryReader) Close() error {
	// ipdb.City does not have a Close method, data is loaded into memory
	return nil
}

// Lookup looks up an IP address in the QQWry database
func (r *QQWryReader) Lookup(ip net.IP) (*QQWryRecord, error) {
	info, err := r.db.FindInfo(ip.String(), "CN")
	if err != nil {
		return nil, err
	}

	return &QQWryRecord{
		CountryName:   info.CountryName,
		RegionName:    info.RegionName,
		CityName:      info.CityName,
		DistrictName:  info.DistrictName,
		ISPDomain:     info.IspDomain,
		CountryCode:   info.CountryCode,
		ContinentCode: info.ContinentCode,
	}, nil
}

// LookupString looks up an IP address string in the QQWry database
func (r *QQWryReader) LookupString(ipStr string) (*QQWryRecord, error) {
	info, err := r.db.FindInfo(ipStr, "CN")
	if err != nil {
		return nil, err
	}

	return &QQWryRecord{
		CountryName:   info.CountryName,
		RegionName:    info.RegionName,
		CityName:      info.CityName,
		DistrictName:  info.DistrictName,
		ISPDomain:     info.IspDomain,
		CountryCode:   info.CountryCode,
		ContinentCode: info.ContinentCode,
	}, nil
}

// IsIPv4Supported returns whether the database supports IPv4
func (r *QQWryReader) IsIPv4Supported() bool {
	return r.db.IsIPv4()
}

// IsIPv6Supported returns whether the database supports IPv6
func (r *QQWryReader) IsIPv6Supported() bool {
	return r.db.IsIPv6()
}

// HasGeoData checks if the record has geographic data
func (r *QQWryRecord) HasGeoData() bool {
	return r.CountryName != "" || r.RegionName != "" || r.CityName != ""
}

// HasCityData checks if the record has city-level data
func (r *QQWryRecord) HasCityData() bool {
	return r.CityName != ""
}

// HasRegionData checks if the record has region/province data
func (r *QQWryRecord) HasRegionData() bool {
	return r.RegionName != ""
}

// IsChina checks if the record is for a Chinese IP
func (r *QQWryRecord) IsChina() bool {
	return r.CountryCode == "CN" || r.CountryName == "中国"
}
