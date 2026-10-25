package merger

import (
	"fmt"
	"net/netip"

	"merged-ip-data/internal/reader"

	"github.com/maxmind/mmdbwriter/mmdbtype"
	"go4.org/netipx"
)

// sourceRecordKey identifies the MMDB value built from one source record. Far
// fewer distinct records than networks exist (GeoLite2-City has roughly one
// record per 17 networks), so values are built once per record and shared by
// every network that uses it. country is the GeoLite2 Country fallback given
// to a record that has no country of its own.
type sourceRecordKey struct {
	offset  uintptr
	country string
}

// sourceRecord is the cached MMDB value of one source record. The value is
// shared between tree entries and must never be mutated.
type sourceRecord struct {
	value      mmdbtype.Map // nil when the record is empty
	hasGeo     bool
	hasPrimary bool
	hasCountry bool
}

// fallbackCountry returns the GeoLite2 Country code for ip, or "" if none.
func (m *Merger) fallbackCountry(ip netip.Addr) string {
	m.reusableGeoWhoisRecord.Reset()
	if err := m.geoWhoisCountry.LookupTo(ip, &m.reusableGeoWhoisRecord); err != nil || !m.reusableGeoWhoisRecord.HasCountry() {
		return ""
	}
	m.stats.GeoWhoisCountryHits++
	return m.reusableGeoWhoisRecord.CountryCode
}

// processGeoLiteCityNetworks inserts every GeoLite2-City network and collects
// the ranges with primary geo data for the DB-IP gap phase.
func (m *Merger) processGeoLiteCityNetworks() error {
	records := make(map[sourceRecordKey]sourceRecord)
	var geoRecord reader.GeoLite2CityRecord
	var merged MergedRecord
	record := func(key sourceRecordKey) (sourceRecord, error) {
		if cached, ok := records[key]; ok {
			return cached, nil
		}
		geoRecord.Reset()
		if err := m.geoLiteCity.Decode(key.offset, &geoRecord); err != nil {
			return sourceRecord{}, err
		}
		merged.Reset()
		mergedRecordFromGeoLiteCity(&geoRecord, &merged)
		cached := sourceRecord{
			hasGeo:     geoRecord.HasGeoData(),
			hasPrimary: geoRecord.HasPrimaryGeoData(),
			hasCountry: merged.Country.ISOCode != "",
		}
		if !cached.hasCountry {
			merged.Country.ISOCode = key.country
		}
		if !merged.IsEmpty() {
			cached.value = merged.ToMMDBType()
			if err := m.shared.shareFields(cached.value); err != nil {
				return sourceRecord{}, err
			}
		}
		records[key] = cached
		return cached, nil
	}

	primaryRanges := make([]netipx.IPRange, 0, 4096)
	var inserted int64
	for result := range m.geoLiteCity.Networks() {
		prefix, network, err := sourceNetwork(result)
		if err != nil {
			return fmt.Errorf("failed to read GeoLite2-City network: %w", err)
		}
		offset := result.Offset()
		source, err := record(sourceRecordKey{offset: offset})
		if err != nil {
			return fmt.Errorf("failed to decode GeoLite2-City network %s: %w", prefix, err)
		}
		if source.hasPrimary {
			primaryRanges = appendCoalescedIPRange(primaryRanges, netipx.RangeOfPrefix(prefix))
		}
		if !source.hasCountry {
			if country := m.fallbackCountry(prefix.Addr()); country != "" {
				if source, err = record(sourceRecordKey{offset: offset, country: country}); err != nil {
					return fmt.Errorf("failed to decode GeoLite2-City network %s: %w", prefix, err)
				}
			}
		}

		m.stats.TotalNetworks++
		if source.hasGeo {
			m.stats.GeoLiteCityHits++
		}
		if source.value == nil {
			m.stats.EmptyRecords++
			continue
		}
		if err := m.insertMMDBMapWithMerge(network, source.value); err != nil {
			if isSkippableInsertError(err) {
				continue
			}
			return fmt.Errorf("failed to insert network %s: %w", prefix, err)
		}
		inserted++
		if inserted%100000 == 0 {
			fmt.Printf("  Inserted %d networks...\n", inserted)
		}
	}

	m.geoPrimaryRanges = primaryRanges
	m.stats.ProcessedNetworks = inserted
	fmt.Printf("  GeoLite2-City: %d networks, %d distinct records\n", m.stats.TotalNetworks, len(records))
	return nil
}

// mergedRecordFromGeoLiteCity fills record with GeoLite2-City as the primary
// geo source. The record should be reset before calling this function.
func mergedRecordFromGeoLiteCity(geoRecord *reader.GeoLite2CityRecord, record *MergedRecord) {
	if !geoRecord.HasGeoData() {
		return
	}
	latitude, longitude, hasCoordinates := geoRecord.Coordinates()

	record.City = CityRecord{
		GeonameID: geoRecord.City.GeonameID,
		Names:     geoRecord.City.Names,
	}

	record.Continent = ContinentRecord{
		Code:      geoRecord.Continent.Code,
		GeonameID: geoRecord.Continent.GeonameID,
		Names:     geoRecord.Continent.Names,
	}

	record.Country = CountryRecord{
		GeonameID: geoRecord.Country.GeonameID,
		ISOCode:   geoRecord.Country.ISOCode,
		Names:     geoRecord.Country.Names,
	}

	record.Location = LocationRecord{
		AccuracyRadius: geoRecord.Location.AccuracyRadius,
		Latitude:       latitude,
		Longitude:      longitude,
		MetroCode:      geoRecord.Location.MetroCode,
		TimeZone:       geoRecord.Location.TimeZone,
		HasCoordinates: hasCoordinates,
	}

	record.Postal = PostalRecord{
		Code: geoRecord.Postal.Code,
	}

	record.RegisteredCountry = CountryRecord{
		GeonameID: geoRecord.RegisteredCountry.GeonameID,
		ISOCode:   geoRecord.RegisteredCountry.ISOCode,
		Names:     geoRecord.RegisteredCountry.Names,
	}

	if len(geoRecord.Subdivisions) > 0 {
		record.Subdivisions = make([]SubdivisionRecord, len(geoRecord.Subdivisions))
		for i, sub := range geoRecord.Subdivisions {
			record.Subdivisions[i] = SubdivisionRecord{
				GeonameID: sub.GeonameID,
				ISOCode:   sub.ISOCode,
				Names:     sub.Names,
			}
		}
	}
}
