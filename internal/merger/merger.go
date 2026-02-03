package merger

import (
	"fmt"
	"io"
	"net"
	"runtime"
	"time"

	"merged-ip-data/internal/config"
	"merged-ip-data/internal/interner"
	"merged-ip-data/internal/reader"

	"github.com/maxmind/mmdbwriter"
	"github.com/maxmind/mmdbwriter/mmdbtype"
)

// logMemStats logs current memory statistics for profiling
func logMemStats(phase string) {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	fmt.Printf("[Memory] %s: Alloc=%d MB, TotalAlloc=%d MB, Sys=%d MB, NumGC=%d\n",
		phase,
		m.Alloc/1024/1024,
		m.TotalAlloc/1024/1024,
		m.Sys/1024/1024,
		m.NumGC)
}

// closerList holds a list of io.Closers for cleanup
type closerList []io.Closer

// closeAll closes all resources and returns the first error encountered
func (cl closerList) closeAll() error {
	var firstErr error
	for _, c := range cl {
		if c != nil {
			if err := c.Close(); err != nil && firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

// Merger handles the merging of multiple IP databases
type Merger struct {
	geoLiteCity     *reader.GeoLite2CityReader
	geoLiteASN      *reader.GeoLite2ASNReader
	ipinfoLite      *reader.IPinfoLiteReader
	dbipCity        *reader.DBIPCityReader
	routeViewsASN   *reader.RouteViewsASNReader
	geoWhoisCountry *reader.GeoWhoisCountryReader
	qqwry           *reader.QQWryReader

	tree *mmdbwriter.Tree

	stats Stats

	// Reusable records for lookups to reduce allocations during merge
	reusableIPinfoRecord      reader.IPinfoLiteRecord
	reusableGeoLiteASNRecord  reader.GeoLite2ASNRecord
	reusableRouteViewsRecord  reader.RouteViewsASNRecord
	reusableGeoWhoisRecord    reader.GeoWhoisCountryRecord
	reusableQQWryRecord       reader.QQWryRecord
	reusableGeoLiteCityRecord reader.GeoLite2CityRecord
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
	QQWryHits           int64
	EmptyRecords        int64
	ProcessedNetworks   int64
}

// New creates a new Merger instance
func New() (*Merger, error) {
	// Initialize string interner with common values
	interner.Init()

	var closers closerList
	cleanup := func() { closers.closeAll() }

	geoLiteCity, err := reader.OpenGeoLite2City()
	if err != nil {
		return nil, fmt.Errorf("failed to open GeoLite2-City: %w", err)
	}
	closers = append(closers, geoLiteCity)

	geoLiteASN, err := reader.OpenGeoLite2ASN()
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("failed to open GeoLite2-ASN: %w", err)
	}
	closers = append(closers, geoLiteASN)

	ipinfoLite, err := reader.OpenIPinfoLite()
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("failed to open IPinfo Lite: %w", err)
	}
	closers = append(closers, ipinfoLite)

	dbipCity, err := reader.OpenDBIPCity()
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("failed to open DB-IP City: %w", err)
	}
	closers = append(closers, dbipCity)

	routeViewsASN, err := reader.OpenRouteViewsASN()
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("failed to open RouteViews ASN: %w", err)
	}
	closers = append(closers, routeViewsASN)

	geoWhoisCountry, err := reader.OpenGeoWhoisCountry()
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("failed to open GeoWhois Country: %w", err)
	}
	closers = append(closers, geoWhoisCountry)

	qqwry, err := reader.OpenQQWry()
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("failed to open QQWry: %w", err)
	}
	closers = append(closers, qqwry)

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
		cleanup()
		return nil, fmt.Errorf("failed to create mmdb tree: %w", err)
	}

	return &Merger{
		geoLiteCity:     geoLiteCity,
		geoLiteASN:      geoLiteASN,
		ipinfoLite:      ipinfoLite,
		dbipCity:        dbipCity,
		routeViewsASN:   routeViewsASN,
		geoWhoisCountry: geoWhoisCountry,
		qqwry:           qqwry,
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
	if m.qqwry != nil {
		if err := m.qqwry.Close(); err != nil {
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
	logMemStats("Start")

	fmt.Println("Processing GeoLite2-City networks (primary source)...")
	if err := m.processGeoLiteCityNetworks(); err != nil {
		return fmt.Errorf("failed to process GeoLite2-City: %w", err)
	}
	logMemStats("After GeoLite2-City")

	// Release memory from completed phase before starting next
	runtime.GC()
	logMemStats("After GC (Phase 1)")

	fmt.Println("Processing DB-IP networks (supplementary data)...")
	if err := m.processDBIPNetworks(); err != nil {
		return fmt.Errorf("failed to process DB-IP: %w", err)
	}
	logMemStats("After DB-IP")

	// Final GC before write phase
	runtime.GC()
	logMemStats("After GC (Phase 2)")

	elapsed := time.Since(startTime)
	fmt.Printf("Merge completed in %v\n", elapsed)
	m.printStats()

	// Print interner statistics
	fmt.Printf("[Interner] %s\n", interner.Stats())

	return nil
}

// processGeoLiteCityNetworks iterates through GeoLite2-City and merges with other sources
func (m *Merger) processGeoLiteCityNetworks() error {
	networks := m.geoLiteCity.Networks()

	// Reuse a single record to reduce allocations
	var record MergedRecord

	for networks.Next() {
		var geoRecord reader.GeoLite2CityRecord
		network, err := networks.Network(&geoRecord)
		if err != nil {
			fmt.Printf("Warning: failed to read network: %v\n", err)
			continue
		}

		m.stats.TotalNetworks++

		record.Reset()
		m.buildMergedRecord(network, &geoRecord, &record)

		if record.IsEmpty() {
			m.stats.EmptyRecords++
			continue
		}

		if err := m.tree.Insert(network, record.ToMMDBType()); err != nil {
			fmt.Printf("Warning: failed to insert network %s: %v\n", network, err)
			continue
		}

		m.stats.ProcessedNetworks++

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

	// Reuse a single record to reduce allocations
	var record MergedRecord

	for networks.Next() {
		var dbipRecord reader.DBIPCityRecord
		network, err := networks.Network(&dbipRecord)
		if err != nil {
			fmt.Printf("Warning: failed to read DB-IP network: %v\n", err)
			continue
		}

		if !dbipRecord.HasGeoData() {
			continue
		}

		ip := network.IP

		// Use reusable record to check if GeoLite2 has data for this IP
		m.reusableGeoLiteCityRecord.Reset()
		if err := m.geoLiteCity.LookupTo(ip, &m.reusableGeoLiteCityRecord); err == nil && m.reusableGeoLiteCityRecord.HasGeoData() {
			continue
		}

		m.stats.TotalNetworks++

		record.Reset()
		m.buildMergedRecordFromDBIP(network, &dbipRecord, &record)

		if record.IsEmpty() {
			m.stats.EmptyRecords++
			continue
		}

		if err := m.insertWithMerge(network, &record); err != nil {
			fmt.Printf("Warning: failed to insert DB-IP network %s: %v\n", network, err)
			continue
		}

		m.stats.DBIPHits++
		m.stats.ProcessedNetworks++
	}

	return networks.Err()
}

// buildMergedRecord creates a merged record for a network using GeoLite2-City as primary.
// The record parameter should be pre-reset before calling this function.
func (m *Merger) buildMergedRecord(network *net.IPNet, geoRecord *reader.GeoLite2CityRecord, record *MergedRecord) {
	if geoRecord.HasGeoData() {
		m.stats.GeoLiteCityHits++

		// Source maps from maxminddb are read-only, safe to reference directly
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
			Latitude:       geoRecord.Location.Latitude,
			Longitude:      geoRecord.Location.Longitude,
			MetroCode:      geoRecord.Location.MetroCode,
			TimeZone:       geoRecord.Location.TimeZone,
			HasCoordinates: geoRecord.HasLocationData(),
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

	m.enrichWithASNData(network.IP, record)
	m.enrichWithCountryFallback(network.IP, record)
	m.enrichWithQQWryData(network.IP, record)
}

// buildMergedRecordFromDBIP creates a merged record using DB-IP as primary geo source.
// The record parameter should be pre-reset before calling this function.
func (m *Merger) buildMergedRecordFromDBIP(network *net.IPNet, dbipRecord *reader.DBIPCityRecord, record *MergedRecord) {
	if dbipRecord.HasGeoData() {
		record.City = CityRecord{
			Names: map[string]string{"en": dbipRecord.City},
		}

		record.Country = CountryRecord{
			ISOCode: dbipRecord.CountryCode,
		}

		if dbipRecord.HasLocationData() {
			record.Location = LocationRecord{
				Latitude:       float64(dbipRecord.Latitude),
				Longitude:      float64(dbipRecord.Longitude),
				TimeZone:       dbipRecord.Timezone,
				HasCoordinates: true,
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
	m.enrichWithQQWryData(network.IP, record)
}

// enrichWithCountryFallback adds country information from GeoWhois when country is missing
func (m *Merger) enrichWithCountryFallback(ip net.IP, record *MergedRecord) {
	if record.Country.ISOCode != "" {
		return
	}

	m.reusableGeoWhoisRecord.Reset()
	if err := m.geoWhoisCountry.LookupTo(ip, &m.reusableGeoWhoisRecord); err == nil && m.reusableGeoWhoisRecord.HasCountry() {
		m.stats.GeoWhoisCountryHits++
		record.Country.ISOCode = m.reusableGeoWhoisRecord.CountryCode
	}
}

// enrichWithQQWryData adds Chinese location data from QQWry (Chunzhen) database for Chinese IPs.
// This provides more accurate and detailed Chinese location names (zh-CN) for IPs in China.
func (m *Merger) enrichWithQQWryData(ip net.IP, record *MergedRecord) {
	// Only enrich for Chinese IPs
	if record.Country.ISOCode != "CN" {
		return
	}

	m.reusableQQWryRecord.Reset()
	if err := m.qqwry.LookupTo(ip, &m.reusableQQWryRecord); err != nil || !m.reusableQQWryRecord.HasGeoData() {
		return
	}

	// Verify the record is indeed for China
	if !m.reusableQQWryRecord.IsChina() {
		return
	}

	m.stats.QQWryHits++

	// Enrich city names with Chinese (zh-CN)
	if m.reusableQQWryRecord.HasCityData() {
		if record.City.Names == nil {
			record.City.Names = make(map[string]string)
		}
		record.City.Names["zh-CN"] = m.reusableQQWryRecord.CityName
	}

	// Enrich subdivision (province) names with Chinese (zh-CN)
	if m.reusableQQWryRecord.HasRegionData() {
		if len(record.Subdivisions) == 0 {
			record.Subdivisions = []SubdivisionRecord{{
				Names: map[string]string{"zh-CN": m.reusableQQWryRecord.RegionName},
			}}
		} else {
			if record.Subdivisions[0].Names == nil {
				record.Subdivisions[0].Names = make(map[string]string)
			}
			record.Subdivisions[0].Names["zh-CN"] = m.reusableQQWryRecord.RegionName
		}
	}

	// Add Chinese country name if not present
	if record.Country.Names == nil {
		record.Country.Names = make(map[string]string)
	}
	if _, ok := record.Country.Names["zh-CN"]; !ok {
		record.Country.Names["zh-CN"] = m.reusableQQWryRecord.CountryName
	}
}

// enrichWithASNData adds ASN information from IPinfo Lite (primary), GeoLite2-ASN (secondary), or RouteViews (tertiary)
func (m *Merger) enrichWithASNData(ip net.IP, record *MergedRecord) {
	// Priority 1: IPinfo Lite (includes as_domain)
	m.reusableIPinfoRecord.Reset()
	if err := m.ipinfoLite.LookupTo(ip, &m.reusableIPinfoRecord); err == nil && m.reusableIPinfoRecord.HasASN() {
		m.stats.IPinfoLiteHits++
		record.ASN = ASNRecord{
			Number:       m.reusableIPinfoRecord.GetASNumber(),
			Organization: m.reusableIPinfoRecord.ASName,
			Domain:       m.reusableIPinfoRecord.ASDomain,
		}
		return
	}

	// Priority 2: GeoLite2-ASN
	m.reusableGeoLiteASNRecord.Reset()
	if err := m.geoLiteASN.LookupTo(ip, &m.reusableGeoLiteASNRecord); err == nil && m.reusableGeoLiteASNRecord.HasASN() {
		m.stats.GeoLiteASNHits++
		record.ASN = ASNRecord{
			Number:       m.reusableGeoLiteASNRecord.AutonomousSystemNumber,
			Organization: m.reusableGeoLiteASNRecord.AutonomousSystemOrganization,
		}
		return
	}

	// Priority 3: RouteViews ASN
	m.reusableRouteViewsRecord.Reset()
	if err := m.routeViewsASN.LookupTo(ip, &m.reusableRouteViewsRecord); err == nil && m.reusableRouteViewsRecord.HasASN() {
		m.stats.RouteViewsASNHits++
		record.ASN = ASNRecord{
			Number:       m.reusableRouteViewsRecord.AutonomousSystemNumber,
			Organization: m.reusableRouteViewsRecord.AutonomousSystemOrganization,
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
	fmt.Printf("  QQWry (Chunzhen) China enrichment hits: %d\n", m.stats.QQWryHits)
	fmt.Printf("  Empty records skipped: %d\n", m.stats.EmptyRecords)
	fmt.Printf("  Final network count: %d\n", m.stats.ProcessedNetworks)
}
