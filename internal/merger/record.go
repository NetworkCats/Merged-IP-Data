package merger

import (
	"merged-ip-data/internal/interner"

	"github.com/maxmind/mmdbwriter/mmdbtype"
)

// Pre-defined mmdbtype.String keys to avoid repeated allocations.
// These are used as map keys in ToMMDBType() methods.
var (
	keyCity              = mmdbtype.String("city")
	keyContinent         = mmdbtype.String("continent")
	keyCountry           = mmdbtype.String("country")
	keyLocation          = mmdbtype.String("location")
	keyPostal            = mmdbtype.String("postal")
	keyRegisteredCountry = mmdbtype.String("registered_country")
	keySubdivisions      = mmdbtype.String("subdivisions")
	keyASN               = mmdbtype.String("asn")
	keyProxy             = mmdbtype.String("proxy")
	keyGeonameID         = mmdbtype.String("geoname_id")
	keyNames             = mmdbtype.String("names")
	keyCode              = mmdbtype.String("code")
	keyISOCode           = mmdbtype.String("iso_code")
	keyAccuracyRadius    = mmdbtype.String("accuracy_radius")
	keyLatitude          = mmdbtype.String("latitude")
	keyLongitude         = mmdbtype.String("longitude")
	keyMetroCode         = mmdbtype.String("metro_code")
	keyTimeZone          = mmdbtype.String("time_zone")
	keyASNumber          = mmdbtype.String("autonomous_system_number")
	keyASOrg             = mmdbtype.String("autonomous_system_organization")
	keyASDomain          = mmdbtype.String("as_domain")
	keyIsProxy           = mmdbtype.String("is_proxy")
	keyIsVPN             = mmdbtype.String("is_vpn")
	keyIsTor             = mmdbtype.String("is_tor")
	keyIsHosting         = mmdbtype.String("is_hosting")
	keyIsCDN             = mmdbtype.String("is_cdn")
	keyIsSchool          = mmdbtype.String("is_school")
	keyIsAnonymous       = mmdbtype.String("is_anonymous")
)

// MergedRecord represents the unified record structure for the output database.
// This structure combines data from all sources with priority-based field selection.
type MergedRecord struct {
	City              CityRecord          `maxminddb:"city"`
	Continent         ContinentRecord     `maxminddb:"continent"`
	Country           CountryRecord       `maxminddb:"country"`
	Location          LocationRecord      `maxminddb:"location"`
	Postal            PostalRecord        `maxminddb:"postal"`
	RegisteredCountry CountryRecord       `maxminddb:"registered_country"`
	Subdivisions      []SubdivisionRecord `maxminddb:"subdivisions"`
	ASN               ASNRecord           `maxminddb:"asn"`
	Proxy             ProxyRecord         `maxminddb:"proxy"`
}

// CityRecord contains city information with multi-language support
type CityRecord struct {
	GeonameID uint32            `maxminddb:"geoname_id"`
	Names     map[string]string `maxminddb:"names"`
}

// ContinentRecord contains continent information with multi-language support
type ContinentRecord struct {
	Code      string            `maxminddb:"code"`
	GeonameID uint32            `maxminddb:"geoname_id"`
	Names     map[string]string `maxminddb:"names"`
}

// CountryRecord contains country information with multi-language support
type CountryRecord struct {
	GeonameID uint32            `maxminddb:"geoname_id"`
	ISOCode   string            `maxminddb:"iso_code"`
	Names     map[string]string `maxminddb:"names"`
}

// LocationRecord contains geographic coordinates and related data
type LocationRecord struct {
	AccuracyRadius uint16  `maxminddb:"accuracy_radius"`
	Latitude       float64 `maxminddb:"latitude"`
	Longitude      float64 `maxminddb:"longitude"`
	MetroCode      uint16  `maxminddb:"metro_code"`
	TimeZone       string  `maxminddb:"time_zone"`
	HasCoordinates bool    // Tracks if coordinates were explicitly set (fixes 0,0 being valid)
}

// PostalRecord contains postal code information
type PostalRecord struct {
	Code string `maxminddb:"code"`
}

// SubdivisionRecord contains subdivision (state/province) information
type SubdivisionRecord struct {
	GeonameID uint32            `maxminddb:"geoname_id"`
	ISOCode   string            `maxminddb:"iso_code"`
	Names     map[string]string `maxminddb:"names"`
}

// ASNRecord contains autonomous system number information
type ASNRecord struct {
	Number       uint32 `maxminddb:"autonomous_system_number"`
	Organization string `maxminddb:"autonomous_system_organization"`
	Domain       string `maxminddb:"as_domain"`
}

// ProxyRecord contains proxy/anonymity detection data from OpenProxyDB
type ProxyRecord struct {
	IsProxy     bool `maxminddb:"is_proxy"`
	IsVPN       bool `maxminddb:"is_vpn"`
	IsTor       bool `maxminddb:"is_tor"`
	IsHosting   bool `maxminddb:"is_hosting"`
	IsCDN       bool `maxminddb:"is_cdn"`
	IsSchool    bool `maxminddb:"is_school"`
	IsAnonymous bool `maxminddb:"is_anonymous"`
}

// ToMMDBType converts the MergedRecord to mmdbtype.Map for insertion into the database.
// Only non-empty fields are included to minimize database size.
func (r *MergedRecord) ToMMDBType() mmdbtype.Map {
	// Convert all sub-records first
	city := r.City.toMMDBType()
	continent := r.Continent.toMMDBType()
	country := r.Country.toMMDBType()
	location := r.Location.toMMDBType()
	postal := r.Postal.toMMDBType()
	regCountry := r.RegisteredCountry.toMMDBType()
	subdivisions := r.subdivisionsToMMDBType()
	asn := r.ASN.toMMDBType()
	proxy := r.Proxy.toMMDBType()

	// Count non-nil fields to allocate exact capacity
	count := 0
	if city != nil {
		count++
	}
	if continent != nil {
		count++
	}
	if country != nil {
		count++
	}
	if location != nil {
		count++
	}
	if postal != nil {
		count++
	}
	if regCountry != nil {
		count++
	}
	if subdivisions != nil {
		count++
	}
	if asn != nil {
		count++
	}
	if proxy != nil {
		count++
	}

	if count == 0 {
		return nil
	}

	result := make(mmdbtype.Map, count)

	if city != nil {
		result[keyCity] = city
	}
	if continent != nil {
		result[keyContinent] = continent
	}
	if country != nil {
		result[keyCountry] = country
	}
	if location != nil {
		result[keyLocation] = location
	}
	if postal != nil {
		result[keyPostal] = postal
	}
	if regCountry != nil {
		result[keyRegisteredCountry] = regCountry
	}
	if subdivisions != nil {
		result[keySubdivisions] = subdivisions
	}
	if asn != nil {
		result[keyASN] = asn
	}
	if proxy != nil {
		result[keyProxy] = proxy
	}

	return result
}

func (c *CityRecord) toMMDBType() mmdbtype.Map {
	// Count non-empty fields first to avoid over-allocation
	count := 0
	if c.GeonameID != 0 {
		count++
	}
	if len(c.Names) > 0 {
		count++
	}
	if count == 0 {
		return nil
	}

	result := make(mmdbtype.Map, count)

	if c.GeonameID != 0 {
		result[keyGeonameID] = mmdbtype.Uint32(c.GeonameID)
	}

	if len(c.Names) > 0 {
		names := make(mmdbtype.Map, len(c.Names))
		for lang, name := range c.Names {
			names[mmdbtype.String(interner.Intern(lang))] = mmdbtype.String(interner.Intern(name))
		}
		result[keyNames] = names
	}

	return result
}

func (c *ContinentRecord) toMMDBType() mmdbtype.Map {
	// Count non-empty fields first to avoid over-allocation
	count := 0
	if c.Code != "" {
		count++
	}
	if c.GeonameID != 0 {
		count++
	}
	if len(c.Names) > 0 {
		count++
	}
	if count == 0 {
		return nil
	}

	result := make(mmdbtype.Map, count)

	if c.Code != "" {
		result[keyCode] = mmdbtype.String(interner.Intern(c.Code))
	}

	if c.GeonameID != 0 {
		result[keyGeonameID] = mmdbtype.Uint32(c.GeonameID)
	}

	if len(c.Names) > 0 {
		names := make(mmdbtype.Map, len(c.Names))
		for lang, name := range c.Names {
			names[mmdbtype.String(interner.Intern(lang))] = mmdbtype.String(interner.Intern(name))
		}
		result[keyNames] = names
	}

	return result
}

func (c *CountryRecord) toMMDBType() mmdbtype.Map {
	// Count non-empty fields first to avoid over-allocation
	count := 0
	if c.GeonameID != 0 {
		count++
	}
	if c.ISOCode != "" {
		count++
	}
	if len(c.Names) > 0 {
		count++
	}
	if count == 0 {
		return nil
	}

	result := make(mmdbtype.Map, count)

	if c.GeonameID != 0 {
		result[keyGeonameID] = mmdbtype.Uint32(c.GeonameID)
	}

	if c.ISOCode != "" {
		result[keyISOCode] = mmdbtype.String(interner.Intern(c.ISOCode))
	}

	if len(c.Names) > 0 {
		names := make(mmdbtype.Map, len(c.Names))
		for lang, name := range c.Names {
			names[mmdbtype.String(interner.Intern(lang))] = mmdbtype.String(interner.Intern(name))
		}
		result[keyNames] = names
	}

	return result
}

func (l *LocationRecord) toMMDBType() mmdbtype.Map {
	// Count non-empty fields first to avoid over-allocation
	count := 0
	if l.AccuracyRadius != 0 {
		count++
	}
	if l.HasCoordinates {
		count += 2 // latitude and longitude
	}
	if l.MetroCode != 0 {
		count++
	}
	if l.TimeZone != "" {
		count++
	}
	if count == 0 {
		return nil
	}

	result := make(mmdbtype.Map, count)

	if l.AccuracyRadius != 0 {
		result[keyAccuracyRadius] = mmdbtype.Uint16(l.AccuracyRadius)
	}

	// Use HasCoordinates flag to correctly handle (0,0) as a valid location
	if l.HasCoordinates {
		result[keyLatitude] = mmdbtype.Float64(l.Latitude)
		result[keyLongitude] = mmdbtype.Float64(l.Longitude)
	}

	if l.MetroCode != 0 {
		result[keyMetroCode] = mmdbtype.Uint16(l.MetroCode)
	}

	if l.TimeZone != "" {
		result[keyTimeZone] = mmdbtype.String(interner.Intern(l.TimeZone))
	}

	return result
}

func (p *PostalRecord) toMMDBType() mmdbtype.Map {
	if p.Code == "" {
		return nil
	}

	result := make(mmdbtype.Map, 1)
	result[keyCode] = mmdbtype.String(p.Code)
	return result
}

func (s *SubdivisionRecord) toMMDBType() mmdbtype.Map {
	// Count non-empty fields first to avoid over-allocation
	count := 0
	if s.GeonameID != 0 {
		count++
	}
	if s.ISOCode != "" {
		count++
	}
	if len(s.Names) > 0 {
		count++
	}
	if count == 0 {
		return nil
	}

	result := make(mmdbtype.Map, count)

	if s.GeonameID != 0 {
		result[keyGeonameID] = mmdbtype.Uint32(s.GeonameID)
	}

	if s.ISOCode != "" {
		result[keyISOCode] = mmdbtype.String(interner.Intern(s.ISOCode))
	}

	if len(s.Names) > 0 {
		names := make(mmdbtype.Map, len(s.Names))
		for lang, name := range s.Names {
			names[mmdbtype.String(interner.Intern(lang))] = mmdbtype.String(interner.Intern(name))
		}
		result[keyNames] = names
	}

	return result
}

func (r *MergedRecord) subdivisionsToMMDBType() mmdbtype.Slice {
	if len(r.Subdivisions) == 0 {
		return nil
	}

	result := make(mmdbtype.Slice, 0, len(r.Subdivisions))
	for _, sub := range r.Subdivisions {
		if subMap := sub.toMMDBType(); subMap != nil {
			result = append(result, subMap)
		}
	}

	if len(result) == 0 {
		return nil
	}
	return result
}

func (a *ASNRecord) toMMDBType() mmdbtype.Map {
	// Count non-empty fields first to avoid over-allocation
	count := 0
	if a.Number != 0 {
		count++
	}
	if a.Organization != "" {
		count++
	}
	if a.Domain != "" {
		count++
	}
	if count == 0 {
		return nil
	}

	result := make(mmdbtype.Map, count)

	if a.Number != 0 {
		result[keyASNumber] = mmdbtype.Uint32(a.Number)
	}

	if a.Organization != "" {
		result[keyASOrg] = mmdbtype.String(interner.Intern(a.Organization))
	}

	if a.Domain != "" {
		result[keyASDomain] = mmdbtype.String(interner.Intern(a.Domain))
	}

	return result
}

func (p *ProxyRecord) toMMDBType() mmdbtype.Map {
	// Count non-empty fields first to avoid over-allocation
	count := 0
	if p.IsProxy {
		count++
	}
	if p.IsVPN {
		count++
	}
	if p.IsTor {
		count++
	}
	if p.IsHosting {
		count++
	}
	if p.IsCDN {
		count++
	}
	if p.IsSchool {
		count++
	}
	if p.IsAnonymous {
		count++
	}
	if count == 0 {
		return nil
	}

	result := make(mmdbtype.Map, count)

	if p.IsProxy {
		result[keyIsProxy] = mmdbtype.Bool(true)
	}
	if p.IsVPN {
		result[keyIsVPN] = mmdbtype.Bool(true)
	}
	if p.IsTor {
		result[keyIsTor] = mmdbtype.Bool(true)
	}
	if p.IsHosting {
		result[keyIsHosting] = mmdbtype.Bool(true)
	}
	if p.IsCDN {
		result[keyIsCDN] = mmdbtype.Bool(true)
	}
	if p.IsSchool {
		result[keyIsSchool] = mmdbtype.Bool(true)
	}
	if p.IsAnonymous {
		result[keyIsAnonymous] = mmdbtype.Bool(true)
	}

	return result
}

// IsEmpty checks if the record has no meaningful data
func (r *MergedRecord) IsEmpty() bool {
	return r.Country.ISOCode == "" &&
		r.City.GeonameID == 0 &&
		len(r.City.Names) == 0 &&
		r.ASN.Number == 0 &&
		!r.Location.HasCoordinates
}

// Reset clears all fields for reuse, reducing allocations
func (r *MergedRecord) Reset() {
	r.City = CityRecord{}
	r.Continent = ContinentRecord{}
	r.Country = CountryRecord{}
	r.Location = LocationRecord{}
	r.Postal = PostalRecord{}
	r.RegisteredCountry = CountryRecord{}
	r.Subdivisions = nil
	r.ASN = ASNRecord{}
	r.Proxy = ProxyRecord{}
}

// HasGeoData checks if the record has geographic data
func (r *MergedRecord) HasGeoData() bool {
	return r.Country.ISOCode != "" || r.City.GeonameID != 0 || len(r.City.Names) > 0
}

// HasASNData checks if the record has ASN data
func (r *MergedRecord) HasASNData() bool {
	return r.ASN.Number != 0
}

// HasLocationData checks if the record has coordinate data
func (r *MergedRecord) HasLocationData() bool {
	return r.Location.HasCoordinates
}
