package merger

import (
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"runtime"
	"runtime/debug"
	"sort"
	"time"

	"merged-ip-data/internal/config"
	"merged-ip-data/internal/interner"
	"merged-ip-data/internal/reader"

	"github.com/maxmind/mmdbwriter"
	"github.com/maxmind/mmdbwriter/mmdbtype"
	"go4.org/netipx"
)

const maxMergeWorkers = 8
const mergeGCPercent = 75

// tuneMergeGC lowers the default heap-growth target during the memory-heavy
// merge while respecting any tighter GOGC setting chosen by the caller.
func tuneMergeGC() func() {
	previous := debug.SetGCPercent(mergeGCPercent)
	if previous <= mergeGCPercent {
		debug.SetGCPercent(previous)
		return func() {}
	}
	return func() { debug.SetGCPercent(previous) }
}

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

func mergeWorkerCount() int {
	numWorkers := runtime.GOMAXPROCS(0)
	if numWorkers < 1 {
		return 1
	}
	if numWorkers > maxMergeWorkers {
		return maxMergeWorkers
	}
	return numWorkers
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
	openproxyDB     *reader.OpenproxyDBReader
	badASN          *reader.BadASNReader
	asnOverlay      *reader.ASNOverlayReader

	tree *mmdbwriter.Tree

	stats Stats

	// Effective GeoLite networks with primary city/country data, coalesced into
	// sorted ranges for efficient DB-IP gap processing.
	geoPrimaryRanges []netipx.IPRange

	// Reusable records for point-lookup sources that do not expose iterable
	// boundaries suitable for the merge.
	reusableGeoWhoisRecord reader.GeoWhoisCountryRecord
}

// Stats holds merge statistics
type Stats struct {
	TotalNetworks                    int64
	GeoLiteCityHits                  int64
	GeoLiteASNHits                   int64
	IPinfoLiteHits                   int64
	DBIPHits                         int64
	RouteViewsASNHits                int64
	GeoWhoisCountryHits              int64
	QQWryHits                        int64
	OpenproxyDBHits                  int64
	OpenproxyDBCIDRRangesInserted    int64
	VPNProviderRangesInserted        int64
	ASNOverlayHits                   int64
	ASNOverlayNetworksInserted       int64
	BadASNHits                       int64
	EmptyRecords                     int64
	ProcessedNetworks                int64
	SingleProxyIPsInserted           int64
	ICloudPrivateRelayRangesInserted int64
	AnycastPrefixesInserted          int64
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
		return nil, fmt.Errorf("failed to open Origin ASN: %w", err)
	}
	closers = append(closers, routeViewsASN)

	geoWhoisCountry, err := reader.OpenGeoWhoisCountry()
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("failed to open GeoLite2 Country: %w", err)
	}
	closers = append(closers, geoWhoisCountry)

	qqwry, err := reader.OpenQQWry()
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("failed to open QQWry: %w", err)
	}
	closers = append(closers, qqwry)

	openproxyDB, err := reader.OpenOpenproxyDB()
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("failed to open OpenProxyDB: %w", err)
	}
	closers = append(closers, openproxyDB)

	badASN, err := reader.OpenBadASNList(config.BadASNListFile)
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("failed to open bad ASN list: %w", err)
	}
	closers = append(closers, badASN)
	fmt.Printf("Bad ASN list loaded: %d ASNs (includes %d manual entries)\n",
		badASN.Count(), len(reader.ManuallyAddedBadASNs))

	asnOverlay, err := reader.OpenASNOverlayLists(
		reader.ASNOverlaySource{
			Path: config.X4BVPNASNFile,
			Record: reader.OpenproxyDBRecord{
				IsVPN: true,
			},
		},
		reader.ASNOverlaySource{
			Path: config.X4BDatacenterASNFile,
			Record: reader.OpenproxyDBRecord{
				IsHosting: true,
			},
		},
	)
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("failed to open ASN proxy overlays: %w", err)
	}
	closers = append(closers, asnOverlay)
	fmt.Printf("ASN proxy overlays loaded: %d ASNs merged into VPN/hosting data\n", asnOverlay.Count())

	singleIPs, cidrRanges := openproxyDB.Stats()
	fmt.Printf("OpenProxyDB loaded: %d single IPs, %d CIDR ranges\n", singleIPs, cidrRanges)

	vpnProviderCount, err := openproxyDB.LoadVPNProviderCIDRRanges(
		config.X4BMullvadVPNFile,
		config.X4BPIAVPNFile,
		config.X4BProtonVPNFile,
		config.X4BDatacenterProtonFile,
	)
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("failed to load VPN provider ranges: %w", err)
	}
	fmt.Printf("VPN provider ranges loaded: %d CIDRs merged into VPN/hosting data\n", vpnProviderCount)

	nordVPNCount, err := openproxyDB.LoadVPNProviderCIDRRangesWithRecord(
		reader.OpenproxyDBRecord{
			IsVPN: true,
		},
		config.NordVPNIPListFile,
	)
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("failed to load NordVPN IP ranges: %w", err)
	}
	fmt.Printf("NordVPN IPs loaded: %d entries merged into VPN data\n", nordVPNCount)

	badIPCount, err := openproxyDB.LoadBadIPList(config.BadIPListFile)
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("failed to load BadIPList: %w", err)
	}
	fmt.Printf("BadIPList loaded: %d IPs merged into proxy data\n", badIPCount)

	torCount, err := openproxyDB.LoadTorRelays(config.TorRelaysFile)
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("failed to load Tor relays: %w", err)
	}
	fmt.Printf("Tor relays loaded: %d unique IPs merged into proxy data\n", torCount)

	icloudCount, err := openproxyDB.LoadICloudPrivateRelayRanges(config.ICloudPrivateRelayFile)
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("failed to load iCloud Private Relay ranges: %w", err)
	}
	fmt.Printf("iCloud Private Relay ranges loaded: %d CIDRs merged into proxy/VPN data\n", icloudCount)

	anycastCount, err := openproxyDB.LoadAnycastPrefixes(config.AnycastV4File, config.AnycastV6File)
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("failed to load anycast prefixes: %w", err)
	}
	fmt.Printf("Anycast prefixes loaded: %d entries (%d in lookup set) — CDN overlay active\n",
		anycastCount, openproxyDB.AnycastPrefixCount())

	singleIPs, cidrRanges = openproxyDB.Stats()
	fmt.Printf("OpenProxyDB total after merge: %d single IPs, %d CIDR ranges\n", singleIPs, cidrRanges)

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
		openproxyDB:     openproxyDB,
		badASN:          badASN,
		asnOverlay:      asnOverlay,
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
	if m.openproxyDB != nil {
		if err := m.openproxyDB.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if m.badASN != nil {
		if err := m.badASN.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if m.asnOverlay != nil {
		if err := m.asnOverlay.Close(); err != nil {
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
	restoreGC := tuneMergeGC()
	defer restoreGC()

	fmt.Println("Starting database merge...")
	startTime := time.Now()
	logMemStats("Start")

	fmt.Println("Processing ASN networks with exact source boundaries...")
	if err := m.processASNNetworks(); err != nil {
		return fmt.Errorf("failed to process ASN networks: %w", err)
	}
	logMemStats("After ASN networks")

	numWorkers := mergeWorkerCount()
	fmt.Printf("Processing GeoLite2-City networks (primary source) with %d workers...\n", numWorkers)
	if err := m.processGeoLiteCityNetworksParallel(numWorkers); err != nil {
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
	m.geoPrimaryRanges = nil
	logMemStats("After DB-IP")

	// DB-IP merges create many short-lived maps while the long-lived tree is
	// already large. Reclaim them before overlay phases so the heap-growth
	// target does not allow transient overlay allocations to drive peak RSS.
	runtime.GC()
	logMemStats("After GC (Phase 2)")

	fmt.Println("Processing GeoLite2 Country fallback networks...")
	if err := m.processCountryFallbackNetworks(); err != nil {
		return fmt.Errorf("failed to process GeoLite2 Country fallback: %w", err)
	}
	logMemStats("After GeoLite2 Country fallback")

	fmt.Println("Processing QQWry China locations (overrides Chinese province/city)...")
	if err := m.processQQWryChinaLocations(); err != nil {
		return fmt.Errorf("failed to process QQWry China locations: %w", err)
	}
	logMemStats("After QQWry China locations")

	fmt.Println("Processing OpenProxyDB CIDR ranges (direct CIDR insertion)...")
	if err := m.processOpenProxyDBCIDRRanges(); err != nil {
		return fmt.Errorf("failed to process OpenProxyDB CIDR ranges: %w", err)
	}
	logMemStats("After OpenProxyDB CIDR ranges")

	fmt.Println("Processing VPN provider ranges (direct CIDR insertion)...")
	if err := m.processVPNProviderRanges(); err != nil {
		return fmt.Errorf("failed to process VPN provider ranges: %w", err)
	}
	logMemStats("After VPN provider ranges")

	fmt.Println("Processing iCloud Private Relay ranges (direct CIDR insertion)...")
	if err := m.processICloudPrivateRelayRanges(); err != nil {
		return fmt.Errorf("failed to process iCloud Private Relay ranges: %w", err)
	}
	logMemStats("After iCloud Private Relay")

	fmt.Println("Processing anycast prefixes (direct CDN insertion)...")
	if err := m.processAnycastPrefixes(); err != nil {
		return fmt.Errorf("failed to process anycast prefixes: %w", err)
	}
	logMemStats("After Anycast Prefixes")

	fmt.Println("Processing single proxy IPs (direct /32 and /128 insertion)...")
	if err := m.processSingleProxyIPs(); err != nil {
		return fmt.Errorf("failed to process single proxy IPs: %w", err)
	}
	logMemStats("After Single Proxy IPs")

	// Final GC before write phase
	runtime.GC()
	logMemStats("After GC (Phase 3)")

	elapsed := time.Since(startTime)
	fmt.Printf("Merge completed in %v\n", elapsed)
	m.printStats()

	// Print interner statistics
	fmt.Printf("[Interner] %s\n", interner.Stats())

	return nil
}

// processGeoLiteCityNetworksParallel processes GeoLite2-City networks using parallel workers.
// This significantly speeds up processing on multi-core systems by:
// 1. Reading networks from GeoLite2-City sequentially (iterator is not thread-safe)
// 2. Processing country enrichment and record encoding in parallel
// 3. Inserting results into the tree sequentially (tree is not thread-safe)
func (m *Merger) processGeoLiteCityNetworksParallel(numWorkers int) error {
	// Create worker pool
	pool := newWorkerPool(
		numWorkers,
		m.geoWhoisCountry,
	)

	// Start workers
	pool.start()

	// Start result consumer in a separate goroutine
	var insertErr error
	var insertedCount int64
	insertDone := make(chan struct{})

	go func() {
		defer close(insertDone)
		for result := range pool.results() {
			if err := m.insertMMDBMapWithMerge(result.network, result.mmdbRecord); err != nil {
				if isSkippableInsertError(err) {
					continue
				}
				if insertErr == nil {
					insertErr = fmt.Errorf("failed to insert network %s: %w", result.network, err)
				}
				fmt.Printf("Warning: failed to insert network %s: %v\n", result.network, err)
				continue
			}
			insertedCount++
			if insertedCount%100000 == 0 {
				fmt.Printf("  Inserted %d networks...\n", insertedCount)
			}
		}
	}()

	// Read networks and submit to worker pool
	networks := m.geoLiteCity.Networks()
	var decodeErr error
	primaryRanges := make([]netipx.IPRange, 0, 4096)
	for networks.Next() {
		var geoRecord reader.GeoLite2CityRecord
		network, err := networks.Network(&geoRecord)
		if err != nil {
			decodeErr = fmt.Errorf("failed to read GeoLite2-City network: %w", err)
			break
		}
		if geoRecord.HasPrimaryGeoData() {
			prefix, ok := netipx.FromStdIPNet(network)
			if !ok {
				decodeErr = fmt.Errorf("invalid GeoLite2-City network %s", network)
				break
			}
			primaryRanges = appendCoalescedIPRange(primaryRanges, netipx.RangeOfPrefix(prefix))
		}

		pool.submit(workItem{
			network:   network,
			geoRecord: geoRecord,
		})
	}

	// Signal no more work
	pool.closeWork()

	// Wait for all workers to finish
	pool.wait()

	// Wait for all insertions to complete
	<-insertDone

	// Check for iterator errors
	if err := networks.Err(); err != nil {
		return err
	}
	if decodeErr != nil {
		return decodeErr
	}
	m.geoPrimaryRanges = primaryRanges

	if insertErr != nil {
		return insertErr
	}

	// Aggregate statistics from workers
	workerStats := pool.aggregateStats()
	m.stats.TotalNetworks = workerStats.TotalNetworks
	m.stats.GeoLiteCityHits = workerStats.GeoLiteCityHits
	m.stats.GeoWhoisCountryHits = workerStats.GeoWhoisCountryHits
	m.stats.EmptyRecords = workerStats.EmptyRecords
	m.stats.ProcessedNetworks = insertedCount

	return nil
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
			return fmt.Errorf("failed to read DB-IP network: %w", err)
		}

		if !dbipRecord.HasGeoData() {
			continue
		}

		for _, prefix := range m.geoUncoveredPrefixes(network) {
			uncovered := netipPrefixToIPNet(prefix)
			if uncovered == nil {
				continue
			}
			m.stats.TotalNetworks++
			record.Reset()
			m.buildMergedRecordFromDBIP(uncovered, &dbipRecord, &record)
			if record.IsEmpty() {
				m.stats.EmptyRecords++
				continue
			}
			if err := m.insertWithMerge(uncovered, &record); err != nil {
				if isSkippableInsertError(err) {
					continue
				}
				return fmt.Errorf("failed to insert DB-IP network %s: %w", uncovered, err)
			}
			m.stats.DBIPHits++
			m.stats.ProcessedNetworks++
		}
	}

	return networks.Err()
}

func appendCoalescedIPRange(ranges []netipx.IPRange, next netipx.IPRange) []netipx.IPRange {
	if !next.IsValid() {
		return ranges
	}
	if len(ranges) == 0 {
		return append(ranges, next)
	}
	last := ranges[len(ranges)-1]
	if last.To().BitLen() == next.From().BitLen() &&
		(next.From().Compare(last.To()) <= 0 || last.To().Next() == next.From()) {
		if next.To().Compare(last.To()) > 0 {
			ranges[len(ranges)-1] = netipx.IPRangeFrom(last.From(), next.To())
		}
		return ranges
	}
	return append(ranges, next)
}

func (m *Merger) geoUncoveredPrefixes(network *net.IPNet) []netip.Prefix {
	prefix, ok := netipx.FromStdIPNet(network)
	if !ok {
		return nil
	}
	target := netipx.RangeOfPrefix(prefix)
	start, end := target.From(), target.To()
	i := sort.Search(len(m.geoPrimaryRanges), func(i int) bool {
		return m.geoPrimaryRanges[i].To().Compare(start) >= 0
	})
	var result []netip.Prefix
	cursor := start
	for ; i < len(m.geoPrimaryRanges); i++ {
		covered := m.geoPrimaryRanges[i]
		if covered.From().Compare(end) > 0 {
			break
		}
		if covered.From().Compare(cursor) > 0 {
			result = netipx.IPRangeFrom(cursor, covered.From().Prev()).AppendPrefixes(result)
		}
		if covered.To().Compare(end) >= 0 {
			return result
		}
		if covered.To().Compare(cursor) >= 0 {
			cursor = covered.To().Next()
		}
	}
	if cursor.IsValid() && cursor.Compare(end) <= 0 {
		result = netipx.IPRangeFrom(cursor, end).AppendPrefixes(result)
	}
	return result
}

func networkContains(outer, inner *net.IPNet) bool {
	if outer == nil || inner == nil {
		return false
	}
	outerOnes, outerBits := outer.Mask.Size()
	innerOnes, innerBits := inner.Mask.Size()
	return outerOnes >= 0 && innerOnes >= 0 && outerBits == innerBits &&
		outerOnes <= innerOnes && outer.Contains(inner.IP)
}

// buildMergedRecordFromDBIP creates a merged record using DB-IP as primary geo source.
// The record parameter should be pre-reset before calling this function.
func (m *Merger) buildMergedRecordFromDBIP(network *net.IPNet, dbipRecord *reader.DBIPCityRecord, record *MergedRecord) {
	if dbipRecord.HasGeoData() {
		if dbipRecord.City != "" {
			record.City = CityRecord{
				Names: map[string]string{"en": dbipRecord.City},
			}
		}

		if dbipRecord.CountryCode != "" {
			record.Country = CountryRecord{
				ISOCode: dbipRecord.CountryCode,
			}
		}

		if dbipRecord.HasLocationData() {
			latitude, longitude, hasCoordinates := dbipRecord.Coordinates()
			record.Location = LocationRecord{
				Latitude:       latitude,
				Longitude:      longitude,
				TimeZone:       dbipRecord.Timezone,
				HasCoordinates: hasCoordinates,
			}
		}

		if dbipRecord.Postcode != "" {
			record.Postal = PostalRecord{
				Code: dbipRecord.Postcode,
			}
		}

		if dbipRecord.State1 != "" || dbipRecord.State2 != "" {
			record.Subdivisions = subdivisionsFromDBIP(dbipRecord)
		}
	}

	m.enrichWithCountryFallback(network.IP, record)
}

func subdivisionsFromDBIP(record *reader.DBIPCityRecord) []SubdivisionRecord {
	subdivisions := make([]SubdivisionRecord, 0, 2)
	if record.State1 != "" {
		subdivisions = append(subdivisions, SubdivisionRecord{Names: map[string]string{"en": record.State1}})
	}
	if record.State2 != "" {
		subdivisions = append(subdivisions, SubdivisionRecord{Names: map[string]string{"en": record.State2}})
	}
	return subdivisions
}

func (m *Merger) processCountryFallbackNetworks() error {
	networks := m.geoWhoisCountry.Networks()
	processed, changed, skipped := 0, 0, 0
	for networks.Next() {
		var source reader.GeoWhoisCountryRecord
		network, err := networks.Network(&source)
		if err != nil {
			return fmt.Errorf("failed to read GeoLite2 Country network: %w", err)
		}
		if !source.HasCountry() {
			continue
		}
		fallback := (&MergedRecord{Country: CountryRecord{ISOCode: source.CountryCode}}).ToMMDBType()
		didChange, err := m.insertMMDBMapWithMergeTracked(network, fallback)
		if err != nil {
			if isSkippableInsertError(err) {
				skipped++
				continue
			}
			return fmt.Errorf("failed to insert GeoLite2 Country network %s: %w", network, err)
		}
		processed++
		if didChange {
			changed++
		}
	}
	if err := networks.Err(); err != nil {
		return err
	}
	m.stats.GeoWhoisCountryHits += int64(changed)
	fmt.Printf("GeoLite2 Country fallback networks: %d processed, %d changed, %d skipped\n", processed, changed, skipped)
	return nil
}

// enrichWithCountryFallback adds country information from GeoLite2 Country when country is missing.
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

func applyASNProxyOverlay(record *MergedRecord, asnOverlay *reader.ASNOverlayReader) bool {
	if record.ASN.Number == 0 || asnOverlay == nil {
		return false
	}
	overlay, ok := asnOverlay.Lookup(record.ASN.Number)
	if !ok {
		return false
	}
	mergeProxyOverlay(&record.Proxy, overlay)
	return true
}

func mergeProxyOverlay(proxy *ProxyRecord, overlay reader.OpenproxyDBRecord) {
	proxy.IsProxy = proxy.IsProxy || overlay.IsProxy
	proxy.IsVPN = proxy.IsVPN || overlay.IsVPN
	proxy.IsTor = proxy.IsTor || overlay.IsTor
	proxy.IsHosting = proxy.IsHosting || overlay.IsHosting
	proxy.IsCDN = proxy.IsCDN || overlay.IsCDN
	proxy.IsSchool = proxy.IsSchool || overlay.IsSchool
	proxy.IsAnonymous = proxy.IsAnonymous || overlay.IsAnonymous
	if proxy.IsProxy || proxy.IsVPN || proxy.IsTor {
		proxy.IsAnonymous = true
	}
}

func proxyRecordFromOpenproxy(record reader.OpenproxyDBRecord) ProxyRecord {
	proxy := ProxyRecord{
		IsProxy:     record.IsProxy,
		IsVPN:       record.IsVPN,
		IsTor:       record.IsTor,
		IsHosting:   record.IsHosting,
		IsCDN:       record.IsCDN,
		IsSchool:    record.IsSchool,
		IsAnonymous: record.IsAnonymous,
	}
	if proxy.IsProxy || proxy.IsVPN || proxy.IsTor {
		proxy.IsAnonymous = true
	}
	return proxy
}

// processASNNetworks inserts each ASN source on its native network boundaries.
// Sources are processed from lowest to highest priority so later records
// replace the ASN (and ASN-derived proxy flags) without point-sampling errors.
func (m *Merger) processASNNetworks() error {
	if err := m.processRouteViewsASNNetworks(); err != nil {
		return err
	}
	if err := m.processGeoLiteASNNetworks(); err != nil {
		return err
	}
	return m.processIPinfoASNNetworks()
}

func (m *Merger) processRouteViewsASNNetworks() error {
	networks := m.routeViewsASN.Networks()
	inserted, skipped, shadowed := 0, 0, 0
	var ipinfoRecord reader.IPinfoLiteRecord
	var geoRecord reader.GeoLite2ASNRecord
	for networks.Next() {
		var source reader.RouteViewsASNRecord
		network, err := networks.Network(&source)
		if err != nil {
			return fmt.Errorf("failed to read Origin ASN network: %w", err)
		}
		if !source.HasASN() {
			continue
		}
		ipinfoRecord.Reset()
		if higher, ok, err := m.ipinfoLite.LookupNetworkTo(network.IP, &ipinfoRecord); err != nil {
			return fmt.Errorf("failed to check IPinfo coverage for Origin ASN network %s: %w", network, err)
		} else if ok && ipinfoRecord.HasASN() && networkContains(higher, network) {
			shadowed++
			continue
		}
		geoRecord.Reset()
		if higher, ok, err := m.geoLiteASN.LookupNetworkTo(network.IP, &geoRecord); err != nil {
			return fmt.Errorf("failed to check GeoLite2-ASN coverage for Origin ASN network %s: %w", network, err)
		} else if ok && geoRecord.HasASN() && networkContains(higher, network) {
			shadowed++
			continue
		}
		asn := ASNRecord{Number: source.AutonomousSystemNumber, Organization: source.AutonomousSystemOrganization}
		overlay, err := m.insertExactASN(network, asn)
		if err != nil {
			if isSkippableInsertError(err) {
				skipped++
				continue
			}
			return fmt.Errorf("failed to insert Origin ASN network %s: %w", network, err)
		}
		inserted++
		m.stats.RouteViewsASNHits++
		m.countASNDerivedFlags(asn, overlay)
	}
	if err := networks.Err(); err != nil {
		return err
	}
	fmt.Printf("  Origin ASN networks: %d inserted, %d shadowed, %d skipped\n", inserted, shadowed, skipped)
	return nil
}

func (m *Merger) processGeoLiteASNNetworks() error {
	networks := m.geoLiteASN.Networks()
	inserted, skipped, shadowed := 0, 0, 0
	var ipinfoRecord reader.IPinfoLiteRecord
	for networks.Next() {
		var source reader.GeoLite2ASNRecord
		network, err := networks.Network(&source)
		if err != nil {
			return fmt.Errorf("failed to read GeoLite2-ASN network: %w", err)
		}
		if !source.HasASN() {
			continue
		}
		ipinfoRecord.Reset()
		if higher, ok, err := m.ipinfoLite.LookupNetworkTo(network.IP, &ipinfoRecord); err != nil {
			return fmt.Errorf("failed to check IPinfo coverage for GeoLite2-ASN network %s: %w", network, err)
		} else if ok && ipinfoRecord.HasASN() && networkContains(higher, network) {
			shadowed++
			continue
		}
		asn := ASNRecord{Number: source.AutonomousSystemNumber, Organization: source.AutonomousSystemOrganization}
		overlay, err := m.insertExactASN(network, asn)
		if err != nil {
			if isSkippableInsertError(err) {
				skipped++
				continue
			}
			return fmt.Errorf("failed to insert GeoLite2-ASN network %s: %w", network, err)
		}
		inserted++
		m.stats.GeoLiteASNHits++
		m.countASNDerivedFlags(asn, overlay)
	}
	if err := networks.Err(); err != nil {
		return err
	}
	fmt.Printf("  GeoLite2-ASN networks: %d inserted, %d shadowed, %d skipped\n", inserted, shadowed, skipped)
	return nil
}

func (m *Merger) processIPinfoASNNetworks() error {
	networks := m.ipinfoLite.Networks()
	inserted, skipped := 0, 0
	for networks.Next() {
		var source reader.IPinfoLiteRecord
		network, err := networks.Network(&source)
		if err != nil {
			return fmt.Errorf("failed to read IPinfo ASN network: %w", err)
		}
		if !source.HasASN() {
			continue
		}
		asn := ASNRecord{Number: source.GetASNumber(), Organization: source.ASName, Domain: source.ASDomain}
		overlay, err := m.insertExactASN(network, asn)
		if err != nil {
			if isSkippableInsertError(err) {
				skipped++
				continue
			}
			return fmt.Errorf("failed to insert IPinfo ASN network %s: %w", network, err)
		}
		inserted++
		m.stats.IPinfoLiteHits++
		m.countASNDerivedFlags(asn, overlay)
	}
	if err := networks.Err(); err != nil {
		return err
	}
	fmt.Printf("  IPinfo ASN networks: %d inserted, %d skipped\n", inserted, skipped)
	return nil
}

// insertExactASN replaces the prior, lower-priority ASN and its derived proxy
// flags. Exact proxy feeds are inserted later and therefore cannot be removed
// by this source-priority phase.
func (m *Merger) insertExactASN(network *net.IPNet, asn ASNRecord) (ProxyRecord, error) {
	proxy := m.asnDerivedProxy(asn)
	asnMMDB := asn.toMMDBType()
	proxyMMDB := proxy.toMMDBType()

	err := m.tree.InsertFunc(network, func(existing mmdbtype.DataType) (mmdbtype.DataType, error) {
		if existing == nil {
			result := makeMMDBMap(2)
			result[keyASN] = asnMMDB
			if proxyMMDB != nil {
				result[keyProxy] = proxyMMDB
			}
			return result, nil
		}
		existingMap, ok := existing.(mmdbtype.Map)
		if !ok {
			result := mmdbtype.Map{keyASN: asnMMDB}
			if proxyMMDB != nil {
				result[keyProxy] = proxyMMDB
			}
			return result, nil
		}
		result := shallowCopyMMDBMap(existingMap, 1)
		result[keyASN] = asnMMDB
		if proxyMMDB == nil {
			delete(result, keyProxy)
		} else {
			result[keyProxy] = proxyMMDB
		}
		return result, nil
	})
	return proxy, err
}

func (m *Merger) asnDerivedProxy(asn ASNRecord) ProxyRecord {
	record := MergedRecord{ASN: asn}
	applySchoolASNMatch(&record)
	applyASNProxyOverlay(&record, m.asnOverlay)
	if !record.Proxy.IsProxy && m.badASN.Contains(asn.Number) {
		record.Proxy.IsProxy = true
		record.Proxy.IsHosting = true
		record.Proxy.IsAnonymous = true
	}
	return record.Proxy
}

func (m *Merger) countASNDerivedFlags(asn ASNRecord, proxy ProxyRecord) {
	if _, ok := m.asnOverlay.Lookup(asn.Number); ok {
		m.stats.ASNOverlayHits++
		m.stats.ASNOverlayNetworksInserted++
	}
	if m.badASN.Contains(asn.Number) && proxy.IsProxy && proxy.IsHosting {
		m.stats.BadASNHits++
	}
}

// processICloudPrivateRelayRanges directly overlays every Apple-published
// iCloud Private Relay CIDR with proxy and VPN flags.
func (m *Merger) processICloudPrivateRelayRanges() error {
	ranges := m.openproxyDB.ICloudPrivateRelayRanges()
	if len(ranges) == 0 {
		fmt.Println("iCloud Private Relay ranges: 0 inserted, 0 skipped")
		return nil
	}

	proxy := ProxyRecord{
		IsProxy:     true,
		IsVPN:       true,
		IsAnonymous: true,
	}
	proxyMMDB := proxy.toMMDBType()

	inserted := 0
	skipped := 0
	for _, prefix := range ranges {
		network := netipPrefixToIPNet(prefix)
		if network == nil {
			skipped++
			continue
		}

		if err := m.insertProxyMap(network, proxyMMDB); err != nil {
			if isSkippableInsertError(err) {
				skipped++
				continue
			}
			return fmt.Errorf("failed to insert iCloud Private Relay range %s: %w", prefix, err)
		}
		inserted++
	}

	fmt.Printf("iCloud Private Relay ranges: %d inserted, %d skipped (of %d total)\n", inserted, skipped, len(ranges))
	m.stats.ICloudPrivateRelayRangesInserted = int64(inserted)
	return nil
}

// processOpenProxyDBCIDRRanges directly overlays every CIDR range from
// OpenProxyDB. Lookup-time enrichment only samples a source network's base IP,
// so direct insertion is required for exact proxy coverage when proxy ranges
// are narrower than the geo/ASN networks already in the tree.
func (m *Merger) processOpenProxyDBCIDRRanges() error {
	ranges := m.openproxyDB.CIDRRanges()
	if len(ranges) == 0 {
		fmt.Println("OpenProxyDB CIDR ranges: 0 inserted, 0 skipped")
		return nil
	}

	inserted := 0
	skipped := 0
	for _, cidrRange := range ranges {
		proxy := ProxyRecord{
			IsProxy:     cidrRange.Record.IsProxy,
			IsVPN:       cidrRange.Record.IsVPN,
			IsTor:       cidrRange.Record.IsTor,
			IsHosting:   cidrRange.Record.IsHosting,
			IsCDN:       cidrRange.Record.IsCDN,
			IsSchool:    cidrRange.Record.IsSchool,
			IsAnonymous: cidrRange.Record.IsAnonymous,
		}
		proxyMMDB := proxy.toMMDBType()
		if proxyMMDB == nil {
			skipped++
			continue
		}

		network := netipPrefixToIPNet(cidrRange.Prefix)
		if network == nil {
			skipped++
			continue
		}

		if err := m.insertProxyMap(network, proxyMMDB); err != nil {
			if isSkippableInsertError(err) {
				skipped++
				continue
			}
			return fmt.Errorf("failed to insert OpenProxyDB CIDR range %s: %w", cidrRange.Prefix, err)
		}
		inserted++
	}

	fmt.Printf("OpenProxyDB CIDR ranges: %d inserted, %d skipped (of %d total)\n", inserted, skipped, len(ranges))
	m.stats.OpenproxyDBCIDRRangesInserted = int64(inserted)
	return nil
}

// processVPNProviderRanges directly overlays third-party VPN provider CIDRs
// with their configured proxy flags.
func (m *Merger) processVPNProviderRanges() error {
	ranges := m.openproxyDB.VPNProviderRanges()
	if len(ranges) == 0 {
		fmt.Println("VPN provider ranges: 0 inserted, 0 skipped")
		return nil
	}

	inserted := 0
	skipped := 0
	for _, cidrRange := range ranges {
		proxy := proxyRecordFromOpenproxy(cidrRange.Record)
		proxyMMDB := proxy.toMMDBType()
		if proxyMMDB == nil {
			skipped++
			continue
		}

		network := netipPrefixToIPNet(cidrRange.Prefix)
		if network == nil {
			skipped++
			continue
		}

		if err := m.insertProxyMap(network, proxyMMDB); err != nil {
			if isSkippableInsertError(err) {
				skipped++
				continue
			}
			return fmt.Errorf("failed to insert VPN provider range %s: %w", cidrRange.Prefix, err)
		}
		inserted++
	}

	fmt.Printf("VPN provider ranges: %d inserted, %d skipped (of %d total)\n", inserted, skipped, len(ranges))
	m.stats.VPNProviderRangesInserted = int64(inserted)
	return nil
}

// processAnycastPrefixes directly overlays bgp.tools anycast prefixes with
// the CDN flag. This avoids relying on geo/ASN source network boundaries to
// happen to align with anycast CIDRs.
func (m *Merger) processAnycastPrefixes() error {
	prefixes := m.openproxyDB.AnycastPrefixes()
	if len(prefixes) == 0 {
		fmt.Println("Anycast prefixes: 0 inserted, 0 skipped")
		return nil
	}

	proxyMMDB := (&ProxyRecord{IsCDN: true}).toMMDBType()
	inserted := 0
	skipped := 0
	for _, prefix := range prefixes {
		network := netipPrefixToIPNet(prefix)
		if network == nil {
			skipped++
			continue
		}

		if err := m.insertProxyMap(network, proxyMMDB); err != nil {
			if isSkippableInsertError(err) {
				skipped++
				continue
			}
			return fmt.Errorf("failed to insert anycast prefix %s: %w", prefix, err)
		}
		inserted++
	}

	fmt.Printf("Anycast prefixes: %d inserted, %d skipped (of %d total)\n", inserted, skipped, len(prefixes))
	m.stats.AnycastPrefixesInserted = int64(inserted)
	return nil
}

// processSingleProxyIPs directly inserts every single IP from OpenProxyDB and BadIPList
// as /32 (IPv4) or /128 (IPv6) networks into the MMDB tree.
// This ensures complete proxy coverage for individual IPs that would otherwise be missed
// when only the network base address is checked during enrichment.
// Uses InsertFunc to merge proxy flags with any existing geo/ASN data in the tree.
func (m *Merger) processSingleProxyIPs() error {
	singleIPs := m.openproxyDB.SingleIPs()
	inserted := 0
	skipped := 0

	for addr, proxyRecord := range singleIPs {
		// Build the proxy mmdbtype
		proxy := ProxyRecord{
			IsProxy:     proxyRecord.IsProxy,
			IsVPN:       proxyRecord.IsVPN,
			IsTor:       proxyRecord.IsTor,
			IsHosting:   proxyRecord.IsHosting,
			IsCDN:       proxyRecord.IsCDN,
			IsSchool:    proxyRecord.IsSchool,
			IsAnonymous: proxyRecord.IsAnonymous,
		}
		proxyMMDB := proxy.toMMDBType()
		if proxyMMDB == nil {
			skipped++
			continue
		}

		// Convert netip.Addr to net.IP and build /32 or /128 network
		ip := addr.AsSlice()
		ones := 32
		if addr.Is6() {
			ones = 128
		}
		network := &net.IPNet{
			IP:   ip,
			Mask: net.CIDRMask(ones, ones),
		}

		if err := m.insertProxyMap(network, proxyMMDB); err != nil {
			// Silently skip reserved and aliased networks — consistent with DB-IP phase
			if isSkippableInsertError(err) {
				skipped++
				continue
			}
			return fmt.Errorf("failed to insert single proxy IP %s: %w", addr, err)
		}
		inserted++
	}

	fmt.Printf("Single proxy IPs: %d inserted, %d skipped (of %d total)\n", inserted, skipped, len(singleIPs))
	m.stats.SingleProxyIPsInserted = int64(inserted)
	return nil
}

func netipPrefixToIPNet(prefix netip.Prefix) *net.IPNet {
	if !prefix.IsValid() {
		return nil
	}

	prefix = prefix.Masked()
	addr := prefix.Addr()
	bits := prefix.Bits()
	if addr.Is4In6() {
		if bits >= 96 {
			bits -= 96
		} else {
			bits = 0
		}
	}
	addr = addr.Unmap()
	bitLen := addr.BitLen()
	if bits > bitLen {
		bits = bitLen
	}

	return &net.IPNet{
		IP:   addr.AsSlice(),
		Mask: net.CIDRMask(bits, bitLen),
	}
}

func (m *Merger) insertProxyMap(network *net.IPNet, proxyMMDB mmdbtype.Map) error {
	return m.tree.InsertFunc(network, func(existing mmdbtype.DataType) (mmdbtype.DataType, error) {
		if existing == nil {
			return mmdbtype.Map{keyProxy: proxyMMDB}, nil
		}

		existingMap, ok := existing.(mmdbtype.Map)
		if !ok {
			return mmdbtype.Map{keyProxy: proxyMMDB}, nil
		}

		// Only the top-level proxy value changes. Nested geo/ASN values are
		// immutable after insertion and can safely be shared. A deep Copy here
		// multiplies allocations for every leaf intersected by a broad overlay.
		if prev, hasPrev := existingMap[keyProxy].(mmdbtype.Map); hasPrev {
			if proxyMapContainsAll(prev, proxyMMDB) {
				return existing, nil
			}
		}

		copied := makeMMDBMap(len(existingMap))
		for key, value := range existingMap {
			copied[key] = value
		}
		if prev, hasPrev := existingMap[keyProxy].(mmdbtype.Map); hasPrev {
			copied[keyProxy] = unionProxyMaps(prev, proxyMMDB)
		} else {
			copied[keyProxy] = proxyMMDB
		}
		return copied, nil
	})
}

func isSkippableInsertError(err error) bool {
	var aliasedErr *mmdbwriter.AliasedNetworkError
	var reservedErr *mmdbwriter.ReservedNetworkError
	return errors.As(err, &aliasedErr) || errors.As(err, &reservedErr)
}

// insertWithMerge inserts a record, merging with existing data if present
func (m *Merger) insertWithMerge(network *net.IPNet, record *MergedRecord) error {
	return m.insertMMDBMapWithMerge(network, record.ToMMDBType())
}

func (m *Merger) insertMMDBMapWithMerge(network *net.IPNet, newMap mmdbtype.Map) error {
	_, err := m.insertMMDBMapWithMergeTracked(network, newMap)
	return err
}

func (m *Merger) insertMMDBMapWithMergeTracked(network *net.IPNet, newMap mmdbtype.Map) (bool, error) {
	changed := false
	err := m.tree.InsertFunc(network, func(existing mmdbtype.DataType) (mmdbtype.DataType, error) {
		if existing == nil {
			changed = true
			return newMap, nil
		}

		existingMap, ok := existing.(mmdbtype.Map)
		if !ok {
			changed = true
			return newMap, nil
		}

		merged, didChange := mergeMMDBMapsChanged(existingMap, newMap)
		changed = changed || didChange
		return merged, nil
	})
	return changed, err
}

// mergeMMDBMaps merges two mmdbtype.Map values, with new values filling in missing fields
func mergeMMDBMaps(existing, new mmdbtype.Map) mmdbtype.Map {
	result, _ := mergeMMDBMapsChanged(existing, new)
	return result
}

func mergeMMDBMapsChanged(existing, new mmdbtype.Map) (mmdbtype.Map, bool) {
	result := existing
	changed := false
	for k, v := range new {
		existingValue, exists := result[k]
		if !exists {
			if !changed {
				result = shallowCopyMMDBMap(existing, len(new))
				changed = true
			}
			result[k] = v
			continue
		}
		if k == keyProxy {
			if existingProxy, ok := existingValue.(mmdbtype.Map); ok {
				if newProxy, ok := v.(mmdbtype.Map); ok {
					if !proxyMapContainsAll(existingProxy, newProxy) {
						if !changed {
							result = shallowCopyMMDBMap(existing, len(new))
							changed = true
						}
						result[k] = unionProxyMaps(existingProxy, newProxy)
					}
				}
			}
			continue
		}
		if existingMap, ok := existingValue.(mmdbtype.Map); ok {
			if newMap, ok := v.(mmdbtype.Map); ok {
				merged, nestedChanged := mergeMMDBMapsChanged(existingMap, newMap)
				if nestedChanged {
					if !changed {
						result = shallowCopyMMDBMap(existing, len(new))
						changed = true
					}
					result[k] = merged
				}
			}
		}
	}

	return result, changed
}

// shallowCopyMMDBMap copies only the map header and entries. Nested values are
// immutable and deliberately shared.
func shallowCopyMMDBMap(source mmdbtype.Map, extraCapacity int) mmdbtype.Map {
	result := makeMMDBMap(len(source) + extraCapacity)
	for key, value := range source {
		result[key] = value
	}
	return result
}

func proxyMapContainsAll(existing, overlay mmdbtype.Map) bool {
	for key, value := range overlay {
		existingValue, ok := existing[key]
		if !ok || !existingValue.Equal(value) {
			return false
		}
	}
	return true
}

// unionProxyMaps returns a fresh map containing the union of boolean flag keys
// from a and b. Every value in the inputs is a mmdbtype.Bool(true) (the proxy
// encoder omits false flags), so a key present in either map means "true".
// Neither input is mutated.
func unionProxyMaps(a, b mmdbtype.Map) mmdbtype.Map {
	result := mmdbtype.Map{}
	for k, v := range a {
		result[k] = v
	}
	for k, v := range b {
		result[k] = v
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
	fmt.Printf("  Origin ASN hits: %d\n", m.stats.RouteViewsASNHits)
	fmt.Printf("  DB-IP supplementary records: %d\n", m.stats.DBIPHits)
	fmt.Printf("  GeoLite2 Country fallback hits: %d\n", m.stats.GeoWhoisCountryHits)
	fmt.Printf("  QQWry (Chunzhen) China records relocated: %d\n", m.stats.QQWryHits)
	fmt.Printf("  OpenProxyDB proxy enrichment hits: %d\n", m.stats.OpenproxyDBHits)
	fmt.Printf("  OpenProxyDB CIDR ranges inserted: %d\n", m.stats.OpenproxyDBCIDRRangesInserted)
	fmt.Printf("  VPN provider ranges inserted: %d\n", m.stats.VPNProviderRangesInserted)
	fmt.Printf("  ASN proxy overlay hits: %d\n", m.stats.ASNOverlayHits)
	fmt.Printf("  ASN proxy overlay networks inserted: %d\n", m.stats.ASNOverlayNetworksInserted)
	fmt.Printf("  Bad ASN fallback hits: %d\n", m.stats.BadASNHits)
	fmt.Printf("  iCloud Private Relay ranges inserted: %d\n", m.stats.ICloudPrivateRelayRangesInserted)
	fmt.Printf("  Anycast prefixes inserted: %d\n", m.stats.AnycastPrefixesInserted)
	fmt.Printf("  Single proxy IPs inserted (/32, /128): %d\n", m.stats.SingleProxyIPsInserted)
	fmt.Printf("  Empty records skipped: %d\n", m.stats.EmptyRecords)
	fmt.Printf("  Final network count: %d\n", m.stats.ProcessedNetworks)
}
