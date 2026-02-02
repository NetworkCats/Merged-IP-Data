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
	result := mmdbtype.Map{}

	if city := r.City.toMMDBType(); len(city) > 0 {
		result["city"] = city
	}

	if continent := r.Continent.toMMDBType(); len(continent) > 0 {
		result["continent"] = continent
	}

	if country := r.Country.toMMDBType(); len(country) > 0 {
		result["country"] = country
	}

	if location := r.Location.toMMDBType(); len(location) > 0 {
		result["location"] = location
	}

	if postal := r.Postal.toMMDBType(); len(postal) > 0 {
		result["postal"] = postal
	}

	if regCountry := r.RegisteredCountry.toMMDBType(); len(regCountry) > 0 {
		result["registered_country"] = regCountry
	}

	if subdivisions := r.subdivisionsToMMDBType(); len(subdivisions) > 0 {
		result["subdivisions"] = subdivisions
	}

	if asn := r.ASN.toMMDBType(); len(asn) > 0 {
		result["asn"] = asn
	}

	return result
}

func (c *CityRecord) toMMDBType() mmdbtype.Map {
	result := mmdbtype.Map{}

	if c.GeonameID != 0 {
		result["geoname_id"] = mmdbtype.Uint32(c.GeonameID)
	}

	if len(c.Names) > 0 {
		names := mmdbtype.Map{}
		for lang, name := range c.Names {
			names[mmdbtype.String(lang)] = mmdbtype.String(name)
		}
		result["names"] = names
	}

	return result
}

func (c *ContinentRecord) toMMDBType() mmdbtype.Map {
	result := mmdbtype.Map{}

	if c.Code != "" {
		result["code"] = mmdbtype.String(c.Code)
	}

	if c.GeonameID != 0 {
		result["geoname_id"] = mmdbtype.Uint32(c.GeonameID)
	}

	if len(c.Names) > 0 {
		names := mmdbtype.Map{}
		for lang, name := range c.Names {
			names[mmdbtype.String(lang)] = mmdbtype.String(name)
		}
		result["names"] = names
	}

	return result
}

func (c *CountryRecord) toMMDBType() mmdbtype.Map {
	result := mmdbtype.Map{}

	if c.GeonameID != 0 {
		result["geoname_id"] = mmdbtype.Uint32(c.GeonameID)
	}

	if c.ISOCode != "" {
		result["iso_code"] = mmdbtype.String(c.ISOCode)
	}

	if len(c.Names) > 0 {
		names := mmdbtype.Map{}
		for lang, name := range c.Names {
			names[mmdbtype.String(lang)] = mmdbtype.String(name)
		}
		result["names"] = names
	}

	return result
}

func (l *LocationRecord) toMMDBType() mmdbtype.Map {
	result := mmdbtype.Map{}

	if l.AccuracyRadius != 0 {
		result["accuracy_radius"] = mmdbtype.Uint16(l.AccuracyRadius)
	}

	if l.Latitude != 0 {
		result["latitude"] = mmdbtype.Float64(l.Latitude)
	}

	if l.Longitude != 0 {
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
	result := mmdbtype.Map{}

	if p.Code != "" {
		result["code"] = mmdbtype.String(p.Code)
	}

	return result
}

func (s *SubdivisionRecord) toMMDBType() mmdbtype.Map {
	result := mmdbtype.Map{}

	if s.GeonameID != 0 {
		result["geoname_id"] = mmdbtype.Uint32(s.GeonameID)
	}

	if s.ISOCode != "" {
		result["iso_code"] = mmdbtype.String(s.ISOCode)
	}

	if len(s.Names) > 0 {
		names := mmdbtype.Map{}
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
	result := mmdbtype.Map{}

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
		r.Location.Latitude == 0 &&
		r.Location.Longitude == 0
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
	return r.Location.Latitude != 0 || r.Location.Longitude != 0
}
