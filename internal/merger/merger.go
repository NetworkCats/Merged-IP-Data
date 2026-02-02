package merger

import (
	"fmt"
	"net"
	"sync/atomic"
	"time"

	"merged-ip-data/internal/config"
	"merged-ip-data/internal/reader"

	"github.com/maxmind/mmdbwriter"
	"github.com/maxmind/mmdbwriter/mmdbtype"
)

// Merger handles the merging of multiple IP databases
type Merger struct {
	geoLiteCity     *reader.GeoLite2CityReader
	geoLiteASN      *reader.GeoLite2ASNReader
	ipinfoLite      *reader.IPinfoLiteReader
	dbipCity        *reader.DBIPCityReader
	routeViewsASN   *reader.RouteViewsASNReader
	geoWhoisCountry *reader.GeoWhoisCountryReader

	tree *mmdbwriter.Tree

	stats Stats
}

// Stats holds merge statistics
type Stats struct {
	TotalNetworks       int64
	GeoLiteCityHits     int64
	GeoLiteASNHits      int64
	IPinfoLiteHits      int64
	DBIPHits            int64
	RouteViewsASNHits   int64
	GeoWhoisCountryHits int64
	EmptyRecords        int64
	ProcessedNetworks   int64
}

// New creates a new Merger instance
func New() (*Merger, error) {
	geoLiteCity, err := reader.OpenGeoLite2City()
	if err != nil {
		return nil, fmt.Errorf("failed to open GeoLite2-City: %w", err)
	}

	geoLiteASN, err := reader.OpenGeoLite2ASN()
	if err != nil {
		geoLiteCity.Close()
		return nil, fmt.Errorf("failed to open GeoLite2-ASN: %w", err)
	}

	ipinfoLite, err := reader.OpenIPinfoLite()
	if err != nil {
		geoLiteCity.Close()
		geoLiteASN.Close()
		return nil, fmt.Errorf("failed to open IPinfo Lite: %w", err)
	}

	dbipCity, err := reader.OpenDBIPCity()
	if err != nil {
		geoLiteCity.Close()
		geoLiteASN.Close()
		ipinfoLite.Close()
		return nil, fmt.Errorf("failed to open DB-IP City: %w", err)
	}

	routeViewsASN, err := reader.OpenRouteViewsASN()
	if err != nil {
		geoLiteCity.Close()
		geoLiteASN.Close()
		ipinfoLite.Close()
		dbipCity.Close()
		return nil, fmt.Errorf("failed to open RouteViews ASN: %w", err)
	}

	geoWhoisCountry, err := reader.OpenGeoWhoisCountry()
	if err != nil {
		geoLiteCity.Close()
		geoLiteASN.Close()
		ipinfoLite.Close()
		dbipCity.Close()
		routeViewsASN.Close()
		return nil, fmt.Errorf("failed to open GeoWhois Country: %w", err)
	}

	tree, err := mmdbwriter.New(mmdbwriter.Options{
		DatabaseType:            config.DatabaseType,
		Description:             map[string]string{"en": config.DatabaseDescription},
		Languages:               config.SupportedLanguages,
		IPVersion:               6,
		RecordSize:              28,
		IncludeReservedNetworks: false,
		DisableIPv4Aliasing:     false,
	})
	if err != nil {
		geoLiteCity.Close()
		geoLiteASN.Close()
		ipinfoLite.Close()
		dbipCity.Close()
		routeViewsASN.Close()
		geoWhoisCountry.Close()
		return nil, fmt.Errorf("failed to create mmdb tree: %w", err)
	}

	return &Merger{
		geoLiteCity:     geoLiteCity,
		geoLiteASN:      geoLiteASN,
		ipinfoLite:      ipinfoLite,
		dbipCity:        dbipCity,
		routeViewsASN:   routeViewsASN,
		geoWhoisCountry: geoWhoisCountry,
		tree:            tree,
	}, nil
}

// Close closes all database readers
func (m *Merger) Close() error {
	var errs []error

	if m.geoLiteCity != nil {
		if err := m.geoLiteCity.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if m.geoLiteASN != nil {
		if err := m.geoLiteASN.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if m.ipinfoLite != nil {
		if err := m.ipinfoLite.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if m.dbipCity != nil {
		if err := m.dbipCity.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if m.routeViewsASN != nil {
		if err := m.routeViewsASN.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if m.geoWhoisCountry != nil {
		if err := m.geoWhoisCountry.Close(); err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("errors closing readers: %v", errs)
	}
	return nil
}

// Merge performs the database merge operation
func (m *Merger) Merge() error {
	fmt.Println("Starting database merge...")
	startTime := time.Now()

	fmt.Println("Processing GeoLite2-City networks (primary source)...")
	if err := m.processGeoLiteCityNetworks(); err != nil {
		return fmt.Errorf("failed to process GeoLite2-City: %w", err)
	}

	fmt.Println("Processing DB-IP networks (supplementary data)...")
	if err := m.processDBIPNetworks(); err != nil {
		return fmt.Errorf("failed to process DB-IP: %w", err)
	}

	elapsed := time.Since(startTime)
	fmt.Printf("Merge completed in %v\n", elapsed)
	m.printStats()

	return nil
}

// processGeoLiteCityNetworks iterates through GeoLite2-City and merges with other sources
func (m *Merger) processGeoLiteCityNetworks() error {
	networks := m.geoLiteCity.Networks()

	for networks.Next() {
		var geoRecord reader.GeoLite2CityRecord
		network, err := networks.Network(&geoRecord)
		if err != nil {
			continue
		}

		atomic.AddInt64(&m.stats.TotalNetworks, 1)

		record := m.buildMergedRecord(network, &geoRecord)

		if record.IsEmpty() {
			atomic.AddInt64(&m.stats.EmptyRecords, 1)
			continue
		}

		if err := m.tree.Insert(network, record.ToMMDBType()); err != nil {
			continue
		}

		atomic.AddInt64(&m.stats.ProcessedNetworks, 1)

		if m.stats.ProcessedNetworks%100000 == 0 {
			fmt.Printf("  Processed %d networks...\n", m.stats.ProcessedNetworks)
		}
	}

	return networks.Err()
}

// processDBIPNetworks processes DB-IP networks for IPs not covered by GeoLite2
func (m *Merger) processDBIPNetworks() error {
	if err := m.processDBIPReader(m.dbipCity.IPv4Reader()); err != nil {
		return err
	}
	return m.processDBIPReader(m.dbipCity.IPv6Reader())
}

func (m *Merger) processDBIPReader(r *reader.Reader) error {
	networks := r.Networks()

	for networks.Next() {
		var dbipRecord reader.DBIPCityRecord
		network, err := networks.Network(&dbipRecord)
		if err != nil {
			continue
		}

		if !dbipRecord.HasGeoData() {
			continue
		}

		ip := network.IP
		geoRecord, _ := m.geoLiteCity.Lookup(ip)

		if geoRecord != nil && geoRecord.HasGeoData() {
			continue
		}

		atomic.AddInt64(&m.stats.TotalNetworks, 1)

		record := m.buildMergedRecordFromDBIP(network, &dbipRecord)

		if record.IsEmpty() {
			atomic.AddInt64(&m.stats.EmptyRecords, 1)
			continue
		}

		if err := m.insertWithMerge(network, record); err != nil {
			continue
		}

		atomic.AddInt64(&m.stats.DBIPHits, 1)
		atomic.AddInt64(&m.stats.ProcessedNetworks, 1)
	}

	return networks.Err()
}

// buildMergedRecord creates a merged record for a network using GeoLite2-City as primary
func (m *Merger) buildMergedRecord(network *net.IPNet, geoRecord *reader.GeoLite2CityRecord) *MergedRecord {
	record := &MergedRecord{}

	if geoRecord.HasGeoData() {
		atomic.AddInt64(&m.stats.GeoLiteCityHits, 1)

		record.City = CityRecord{
			GeonameID: geoRecord.City.GeonameID,
			Names:     copyMap(geoRecord.City.Names),
		}

		record.Continent = ContinentRecord{
			Code:      geoRecord.Continent.Code,
			GeonameID: geoRecord.Continent.GeonameID,
			Names:     copyMap(geoRecord.Continent.Names),
		}

		record.Country = CountryRecord{
			GeonameID: geoRecord.Country.GeonameID,
			ISOCode:   geoRecord.Country.ISOCode,
			Names:     copyMap(geoRecord.Country.Names),
		}

		record.Location = LocationRecord{
			AccuracyRadius: geoRecord.Location.AccuracyRadius,
			Latitude:       geoRecord.Location.Latitude,
			Longitude:      geoRecord.Location.Longitude,
			MetroCode:      geoRecord.Location.MetroCode,
			TimeZone:       geoRecord.Location.TimeZone,
		}

		record.Postal = PostalRecord{
			Code: geoRecord.Postal.Code,
		}

		record.RegisteredCountry = CountryRecord{
			GeonameID: geoRecord.RegisteredCountry.GeonameID,
			ISOCode:   geoRecord.RegisteredCountry.ISOCode,
			Names:     copyMap(geoRecord.RegisteredCountry.Names),
		}

		if len(geoRecord.Subdivisions) > 0 {
			record.Subdivisions = make([]SubdivisionRecord, len(geoRecord.Subdivisions))
			for i, sub := range geoRecord.Subdivisions {
				record.Subdivisions[i] = SubdivisionRecord{
					GeonameID: sub.GeonameID,
					ISOCode:   sub.ISOCode,
					Names:     copyMap(sub.Names),
				}
			}
		}
	}

	m.enrichWithASNData(network.IP, record)
	m.enrichWithCountryFallback(network.IP, record)

	return record
}

// buildMergedRecordFromDBIP creates a merged record using DB-IP as primary geo source
func (m *Merger) buildMergedRecordFromDBIP(network *net.IPNet, dbipRecord *reader.DBIPCityRecord) *MergedRecord {
	record := &MergedRecord{}

	if dbipRecord.HasGeoData() {
		record.City = CityRecord{
			Names: map[string]string{"en": dbipRecord.City},
		}

		record.Country = CountryRecord{
			ISOCode: dbipRecord.CountryCode,
		}

		if dbipRecord.HasLocationData() {
			record.Location = LocationRecord{
				Latitude:  float64(dbipRecord.Latitude),
				Longitude: float64(dbipRecord.Longitude),
				TimeZone:  dbipRecord.Timezone,
			}
		}

		if dbipRecord.Postcode != "" {
			record.Postal = PostalRecord{
				Code: dbipRecord.Postcode,
			}
		}

		if dbipRecord.State1 != "" {
			record.Subdivisions = []SubdivisionRecord{
				{
					Names: map[string]string{"en": dbipRecord.State1},
				},
			}
		}
	}

	m.enrichWithASNData(network.IP, record)
	m.enrichWithCountryFallback(network.IP, record)

	return record
}

// enrichWithCountryFallback adds country information from GeoWhois when country is missing
func (m *Merger) enrichWithCountryFallback(ip net.IP, record *MergedRecord) {
	if record.Country.ISOCode != "" {
		return
	}

	geoWhoisRecord, err := m.geoWhoisCountry.Lookup(ip)
	if err == nil && geoWhoisRecord.HasCountry() {
		atomic.AddInt64(&m.stats.GeoWhoisCountryHits, 1)
		record.Country.ISOCode = geoWhoisRecord.CountryCode
	}
}

// enrichWithASNData adds ASN information from IPinfo Lite (primary), GeoLite2-ASN (secondary), or RouteViews (tertiary)
func (m *Merger) enrichWithASNData(ip net.IP, record *MergedRecord) {
	// Priority 1: IPinfo Lite (includes as_domain)
	ipinfoRecord, err := m.ipinfoLite.Lookup(ip)
	if err == nil && ipinfoRecord.HasASN() {
		atomic.AddInt64(&m.stats.IPinfoLiteHits, 1)
		record.ASN = ASNRecord{
			Number:       ipinfoRecord.GetASNumber(),
			Organization: ipinfoRecord.ASName,
			Domain:       ipinfoRecord.ASDomain,
		}
		return
	}

	// Priority 2: GeoLite2-ASN
	asnRecord, err := m.geoLiteASN.Lookup(ip)
	if err == nil && asnRecord.HasASN() {
		atomic.AddInt64(&m.stats.GeoLiteASNHits, 1)
		record.ASN = ASNRecord{
			Number:       asnRecord.AutonomousSystemNumber,
			Organization: asnRecord.AutonomousSystemOrganization,
		}
		return
	}

	// Priority 3: RouteViews ASN
	routeViewsRecord, err := m.routeViewsASN.Lookup(ip)
	if err == nil && routeViewsRecord.HasASN() {
		atomic.AddInt64(&m.stats.RouteViewsASNHits, 1)
		record.ASN = ASNRecord{
			Number:       routeViewsRecord.AutonomousSystemNumber,
			Organization: routeViewsRecord.AutonomousSystemOrganization,
		}
	}
}

// insertWithMerge inserts a record, merging with existing data if present
func (m *Merger) insertWithMerge(network *net.IPNet, record *MergedRecord) error {
	return m.tree.InsertFunc(network, func(existing mmdbtype.DataType) (mmdbtype.DataType, error) {
		if existing == nil {
			return record.ToMMDBType(), nil
		}

		existingMap, ok := existing.(mmdbtype.Map)
		if !ok {
			return record.ToMMDBType(), nil
		}

		newMap := record.ToMMDBType()
		return mergeMMDBMaps(existingMap, newMap), nil
	})
}

// mergeMMDBMaps merges two mmdbtype.Map values, with new values filling in missing fields
func mergeMMDBMaps(existing, new mmdbtype.Map) mmdbtype.Map {
	result := mmdbtype.Map{}

	for k, v := range existing {
		result[k] = v
	}

	for k, v := range new {
		if _, exists := result[k]; !exists {
			result[k] = v
		}
	}

	return result
}

// Tree returns the mmdbwriter tree for writing
func (m *Merger) Tree() *mmdbwriter.Tree {
	return m.tree
}

// Stats returns the merge statistics
func (m *Merger) Stats() Stats {
	return m.stats
}

func (m *Merger) printStats() {
	fmt.Println("Merge Statistics:")
	fmt.Printf("  Total networks processed: %d\n", m.stats.TotalNetworks)
	fmt.Printf("  GeoLite2-City hits: %d\n", m.stats.GeoLiteCityHits)
	fmt.Printf("  GeoLite2-ASN hits: %d\n", m.stats.GeoLiteASNHits)
	fmt.Printf("  IPinfo Lite hits: %d\n", m.stats.IPinfoLiteHits)
	fmt.Printf("  RouteViews ASN hits: %d\n", m.stats.RouteViewsASNHits)
	fmt.Printf("  DB-IP supplementary records: %d\n", m.stats.DBIPHits)
	fmt.Printf("  GeoWhois Country fallback hits: %d\n", m.stats.GeoWhoisCountryHits)
	fmt.Printf("  Empty records skipped: %d\n", m.stats.EmptyRecords)
	fmt.Printf("  Final network count: %d\n", m.stats.ProcessedNetworks)
}

// copyMap creates a deep copy of a string map
func copyMap(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	result := make(map[string]string, len(m))
	for k, v := range m {
		result[k] = v
	}
	return result
}
