package merger

import (
	"github.com/maxmind/mmdbwriter/mmdbtype"
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

	if count == 0 {
		return nil
	}

	result := make(mmdbtype.Map, count)

	if city != nil {
		result["city"] = city
	}
	if continent != nil {
		result["continent"] = continent
	}
	if country != nil {
		result["country"] = country
	}
	if location != nil {
		result["location"] = location
	}
	if postal != nil {
		result["postal"] = postal
	}
	if regCountry != nil {
		result["registered_country"] = regCountry
	}
	if subdivisions != nil {
		result["subdivisions"] = subdivisions
	}
	if asn != nil {
		result["asn"] = asn
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
		result["geoname_id"] = mmdbtype.Uint32(c.GeonameID)
	}

	if len(c.Names) > 0 {
		names := make(mmdbtype.Map, len(c.Names))
		for lang, name := range c.Names {
			names[mmdbtype.String(lang)] = mmdbtype.String(name)
		}
		result["names"] = names
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
		result["code"] = mmdbtype.String(c.Code)
	}

	if c.GeonameID != 0 {
		result["geoname_id"] = mmdbtype.Uint32(c.GeonameID)
	}

	if len(c.Names) > 0 {
		names := make(mmdbtype.Map, len(c.Names))
		for lang, name := range c.Names {
			names[mmdbtype.String(lang)] = mmdbtype.String(name)
		}
		result["names"] = names
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
		result["geoname_id"] = mmdbtype.Uint32(c.GeonameID)
	}

	if c.ISOCode != "" {
		result["iso_code"] = mmdbtype.String(c.ISOCode)
	}

	if len(c.Names) > 0 {
		names := make(mmdbtype.Map, len(c.Names))
		for lang, name := range c.Names {
			names[mmdbtype.String(lang)] = mmdbtype.String(name)
		}
		result["names"] = names
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
		result["accuracy_radius"] = mmdbtype.Uint16(l.AccuracyRadius)
	}

	// Use HasCoordinates flag to correctly handle (0,0) as a valid location
	if l.HasCoordinates {
		result["latitude"] = mmdbtype.Float64(l.Latitude)
		result["longitude"] = mmdbtype.Float64(l.Longitude)
	}

	if l.MetroCode != 0 {
		result["metro_code"] = mmdbtype.Uint16(l.MetroCode)
	}

	if l.TimeZone != "" {
		result["time_zone"] = mmdbtype.String(l.TimeZone)
	}

	return result
}

func (p *PostalRecord) toMMDBType() mmdbtype.Map {
	if p.Code == "" {
		return nil
	}

	result := make(mmdbtype.Map, 1)
	result["code"] = mmdbtype.String(p.Code)
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
		result["geoname_id"] = mmdbtype.Uint32(s.GeonameID)
	}

	if s.ISOCode != "" {
		result["iso_code"] = mmdbtype.String(s.ISOCode)
	}

	if len(s.Names) > 0 {
		names := make(mmdbtype.Map, len(s.Names))
		for lang, name := range s.Names {
			names[mmdbtype.String(lang)] = mmdbtype.String(name)
		}
		result["names"] = names
	}

	return result
}

func (r *MergedRecord) subdivisionsToMMDBType() mmdbtype.Slice {
	if len(r.Subdivisions) == 0 {
		return nil
	}

	result := make(mmdbtype.Slice, 0, len(r.Subdivisions))
	for _, sub := range r.Subdivisions {
		if subMap := sub.toMMDBType(); len(subMap) > 0 {
			result = append(result, subMap)
		}
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
		result["autonomous_system_number"] = mmdbtype.Uint32(a.Number)
	}

	if a.Organization != "" {
		result["autonomous_system_organization"] = mmdbtype.String(a.Organization)
	}

	if a.Domain != "" {
		result["as_domain"] = mmdbtype.String(a.Domain)
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
