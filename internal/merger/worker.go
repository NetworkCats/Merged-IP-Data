package merger

import (
	"net"
	"runtime"
	"sync"
	"sync/atomic"

	"merged-ip-data/internal/reader"

	"github.com/maxmind/mmdbwriter/mmdbtype"
)

// workItem represents a unit of work for parallel processing
type workItem struct {
	network   *net.IPNet
	geoRecord reader.GeoLite2CityRecord
}

// resultItem represents the processed result ready for insertion
type resultItem struct {
	network    *net.IPNet
	mmdbRecord mmdbtype.Map
}

// workerContext holds the per-worker state for enrichment lookups.
// Each worker has its own context to avoid contention.
type workerContext struct {
	// Database readers (shared, read-only)
	ipinfoLite      *reader.IPinfoLiteReader
	geoLiteASN      *reader.GeoLite2ASNReader
	routeViewsASN   *reader.RouteViewsASNReader
	geoWhoisCountry *reader.GeoWhoisCountryReader
	qqwry           *reader.QQWryReader
	openproxyDB     *reader.OpenproxyDBReader

	// Per-worker reusable records (not shared between workers)
	reusableIPinfoRecord     reader.IPinfoLiteRecord
	reusableGeoLiteASNRecord reader.GeoLite2ASNRecord
	reusableRouteViewsRecord reader.RouteViewsASNRecord
	reusableGeoWhoisRecord   reader.GeoWhoisCountryRecord
	reusableQQWryRecord      reader.QQWryRecord
	reusableOpenproxyRecord  reader.OpenproxyDBRecord
	reusableMergedRecord     MergedRecord

	// Per-worker ASN cache
	cachedASN        ASNRecord
	cachedASNNetwork *net.IPNet
	cachedASNValid   bool

	// Per-worker statistics (atomically updated)
	stats workerStats
}

// workerStats holds per-worker statistics
type workerStats struct {
	geoLiteCityHits     int64
	geoLiteASNHits      int64
	ipinfoLiteHits      int64
	routeViewsASNHits   int64
	geoWhoisCountryHits int64
	qqwryHits           int64
	openproxyDBHits     int64
	emptyRecords        int64
	processedNetworks   int64
}

// workerPool manages a pool of workers for parallel processing
type workerPool struct {
	numWorkers int
	workChan   chan workItem
	resultChan chan resultItem
	wg         sync.WaitGroup
	contexts   []*workerContext

	// Aggregated statistics
	totalNetworks atomic.Int64
	stats         Stats
	statsMu       sync.Mutex
}

// newWorkerPool creates a new worker pool with the specified number of workers
func newWorkerPool(
	numWorkers int,
	ipinfoLite *reader.IPinfoLiteReader,
	geoLiteASN *reader.GeoLite2ASNReader,
	routeViewsASN *reader.RouteViewsASNReader,
	geoWhoisCountry *reader.GeoWhoisCountryReader,
	qqwry *reader.QQWryReader,
	openproxyDB *reader.OpenproxyDBReader,
) *workerPool {
	if numWorkers <= 0 {
		numWorkers = runtime.NumCPU()
	}

	// Use buffered channels to reduce blocking
	// Work channel: large buffer to keep workers busy
	// Result channel: batch size * 2 to allow some buffering
	workChanSize := numWorkers * 1000
	resultChanSize := numWorkers * 100

	pool := &workerPool{
		numWorkers: numWorkers,
		workChan:   make(chan workItem, workChanSize),
		resultChan: make(chan resultItem, resultChanSize),
		contexts:   make([]*workerContext, numWorkers),
	}

	// Create worker contexts with shared readers but per-worker reusable records
	for i := 0; i < numWorkers; i++ {
		pool.contexts[i] = &workerContext{
			ipinfoLite:      ipinfoLite,
			geoLiteASN:      geoLiteASN,
			routeViewsASN:   routeViewsASN,
			geoWhoisCountry: geoWhoisCountry,
			qqwry:           qqwry,
			openproxyDB:     openproxyDB,
		}
	}

	return pool
}

// start launches all workers
func (p *workerPool) start() {
	for i := 0; i < p.numWorkers; i++ {
		p.wg.Add(1)
		go p.worker(i)
	}
}

// submit sends a work item to the pool
func (p *workerPool) submit(item workItem) {
	p.totalNetworks.Add(1)
	p.workChan <- item
}

// closeWork signals that no more work will be submitted
func (p *workerPool) closeWork() {
	close(p.workChan)
}

// wait waits for all workers to complete and closes the result channel
func (p *workerPool) wait() {
	p.wg.Wait()
	close(p.resultChan)
}

// results returns the channel for reading processed results
func (p *workerPool) results() <-chan resultItem {
	return p.resultChan
}

// aggregateStats aggregates all worker statistics into the pool stats
func (p *workerPool) aggregateStats() Stats {
	p.statsMu.Lock()
	defer p.statsMu.Unlock()

	var stats Stats
	stats.TotalNetworks = p.totalNetworks.Load()

	for _, ctx := range p.contexts {
		stats.GeoLiteCityHits += ctx.stats.geoLiteCityHits
		stats.GeoLiteASNHits += ctx.stats.geoLiteASNHits
		stats.IPinfoLiteHits += ctx.stats.ipinfoLiteHits
		stats.RouteViewsASNHits += ctx.stats.routeViewsASNHits
		stats.GeoWhoisCountryHits += ctx.stats.geoWhoisCountryHits
		stats.QQWryHits += ctx.stats.qqwryHits
		stats.OpenproxyDBHits += ctx.stats.openproxyDBHits
		stats.EmptyRecords += ctx.stats.emptyRecords
		stats.ProcessedNetworks += ctx.stats.processedNetworks
	}

	return stats
}

// worker is the main worker goroutine
func (p *workerPool) worker(id int) {
	defer p.wg.Done()

	ctx := p.contexts[id]

	for item := range p.workChan {
		result := ctx.processWorkItem(item)
		if result.mmdbRecord != nil {
			p.resultChan <- result
		}
	}
}

// processWorkItem processes a single work item and returns the result
func (ctx *workerContext) processWorkItem(item workItem) resultItem {
	ctx.reusableMergedRecord.Reset()

	// Build merged record from GeoLite2-City as primary source
	ctx.buildMergedRecord(item.network, &item.geoRecord)

	if ctx.reusableMergedRecord.IsEmpty() {
		ctx.stats.emptyRecords++
		return resultItem{network: item.network, mmdbRecord: nil}
	}

	ctx.stats.processedNetworks++

	return resultItem{
		network:    item.network,
		mmdbRecord: ctx.reusableMergedRecord.ToMMDBType(),
	}
}

// buildMergedRecord creates a merged record for a network using GeoLite2-City as primary
func (ctx *workerContext) buildMergedRecord(network *net.IPNet, geoRecord *reader.GeoLite2CityRecord) {
	record := &ctx.reusableMergedRecord

	if geoRecord.HasGeoData() {
		ctx.stats.geoLiteCityHits++

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

	ctx.enrichWithASNData(network.IP, record)
	ctx.enrichWithCountryFallback(network.IP, record)
	ctx.enrichWithQQWryData(network.IP, record)
	ctx.enrichWithProxyData(network.IP, record)
}

// enrichWithASNData adds ASN information with caching
func (ctx *workerContext) enrichWithASNData(ip net.IP, record *MergedRecord) {
	// Check cache first
	if ctx.cachedASNValid && ctx.cachedASNNetwork != nil && ctx.cachedASNNetwork.Contains(ip) {
		if ctx.cachedASN.Number != 0 {
			record.ASN = ctx.cachedASN
		}
		return
	}

	ctx.cachedASNValid = false
	ctx.cachedASNNetwork = nil

	// Priority 1: IPinfo Lite
	ctx.reusableIPinfoRecord.Reset()
	if err := ctx.ipinfoLite.LookupTo(ip, &ctx.reusableIPinfoRecord); err == nil && ctx.reusableIPinfoRecord.HasASN() {
		ctx.stats.ipinfoLiteHits++
		record.ASN = ASNRecord{
			Number:       ctx.reusableIPinfoRecord.GetASNumber(),
			Organization: ctx.reusableIPinfoRecord.ASName,
			Domain:       ctx.reusableIPinfoRecord.ASDomain,
		}
		// Cache (simplified - use lookup result)
		ctx.cachedASN = record.ASN
		ctx.cachedASNValid = true
		return
	}

	// Priority 2: GeoLite2-ASN
	ctx.reusableGeoLiteASNRecord.Reset()
	if err := ctx.geoLiteASN.LookupTo(ip, &ctx.reusableGeoLiteASNRecord); err == nil && ctx.reusableGeoLiteASNRecord.HasASN() {
		ctx.stats.geoLiteASNHits++
		record.ASN = ASNRecord{
			Number:       ctx.reusableGeoLiteASNRecord.AutonomousSystemNumber,
			Organization: ctx.reusableGeoLiteASNRecord.AutonomousSystemOrganization,
		}
		ctx.cachedASN = record.ASN
		ctx.cachedASNValid = true
		return
	}

	// Priority 3: RouteViews ASN
	ctx.reusableRouteViewsRecord.Reset()
	if err := ctx.routeViewsASN.LookupTo(ip, &ctx.reusableRouteViewsRecord); err == nil && ctx.reusableRouteViewsRecord.HasASN() {
		ctx.stats.routeViewsASNHits++
		record.ASN = ASNRecord{
			Number:       ctx.reusableRouteViewsRecord.AutonomousSystemNumber,
			Organization: ctx.reusableRouteViewsRecord.AutonomousSystemOrganization,
		}
		ctx.cachedASN = record.ASN
		ctx.cachedASNValid = true
		return
	}

	// No ASN found
	ctx.cachedASN = ASNRecord{}
	ctx.cachedASNValid = true
}

// enrichWithCountryFallback adds country information from GeoWhois when country is missing
func (ctx *workerContext) enrichWithCountryFallback(ip net.IP, record *MergedRecord) {
	if record.Country.ISOCode != "" {
		return
	}

	ctx.reusableGeoWhoisRecord.Reset()
	if err := ctx.geoWhoisCountry.LookupTo(ip, &ctx.reusableGeoWhoisRecord); err == nil && ctx.reusableGeoWhoisRecord.HasCountry() {
		ctx.stats.geoWhoisCountryHits++
		record.Country.ISOCode = ctx.reusableGeoWhoisRecord.CountryCode
	}
}

// enrichWithQQWryData adds Chinese location data for Chinese IPs
func (ctx *workerContext) enrichWithQQWryData(ip net.IP, record *MergedRecord) {
	if record.Country.ISOCode != "CN" {
		return
	}

	ctx.reusableQQWryRecord.Reset()
	if err := ctx.qqwry.LookupTo(ip, &ctx.reusableQQWryRecord); err != nil || !ctx.reusableQQWryRecord.HasGeoData() {
		return
	}

	if !ctx.reusableQQWryRecord.IsChina() {
		return
	}

	ctx.stats.qqwryHits++

	if ctx.reusableQQWryRecord.HasCityData() {
		if record.City.Names == nil {
			record.City.Names = make(map[string]string)
		}
		record.City.Names["zh-CN"] = ctx.reusableQQWryRecord.CityName
	}

	if ctx.reusableQQWryRecord.HasRegionData() {
		if len(record.Subdivisions) == 0 {
			record.Subdivisions = []SubdivisionRecord{{
				Names: map[string]string{"zh-CN": ctx.reusableQQWryRecord.RegionName},
			}}
		} else {
			if record.Subdivisions[0].Names == nil {
				record.Subdivisions[0].Names = make(map[string]string)
			}
			record.Subdivisions[0].Names["zh-CN"] = ctx.reusableQQWryRecord.RegionName
		}
	}

	if record.Country.Names == nil {
		record.Country.Names = make(map[string]string)
	}
	if _, ok := record.Country.Names["zh-CN"]; !ok {
		record.Country.Names["zh-CN"] = ctx.reusableQQWryRecord.CountryName
	}
}

// enrichWithProxyData adds proxy/anonymity information from OpenProxyDB
func (ctx *workerContext) enrichWithProxyData(ip net.IP, record *MergedRecord) {
	ctx.reusableOpenproxyRecord.Reset()
	if !ctx.openproxyDB.LookupTo(ip, &ctx.reusableOpenproxyRecord) {
		return
	}

	ctx.stats.openproxyDBHits++
	record.Proxy = ProxyRecord{
		IsProxy:     ctx.reusableOpenproxyRecord.IsProxy,
		IsVPN:       ctx.reusableOpenproxyRecord.IsVPN,
		IsTor:       ctx.reusableOpenproxyRecord.IsTor,
		IsHosting:   ctx.reusableOpenproxyRecord.IsHosting,
		IsCDN:       ctx.reusableOpenproxyRecord.IsCDN,
		IsSchool:    ctx.reusableOpenproxyRecord.IsSchool,
		IsAnonymous: ctx.reusableOpenproxyRecord.IsAnonymous,
	}
}
