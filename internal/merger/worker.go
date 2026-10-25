package merger

import (
	"net"
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

// workerContext holds the per-worker state for country lookups.
// Each worker has its own context to avoid contention.
type workerContext struct {
	// Database readers (shared, read-only)
	geoWhoisCountry *reader.GeoWhoisCountryReader

	// Per-worker reusable records (not shared between workers)
	reusableGeoWhoisRecord reader.GeoWhoisCountryRecord
	reusableMergedRecord   MergedRecord

	// Per-worker statistics; each context is owned by one worker.
	stats workerStats
}

// workerStats holds per-worker statistics
type workerStats struct {
	geoLiteCityHits     int64
	geoWhoisCountryHits int64
	emptyRecords        int64
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
}

// newWorkerPool creates a new worker pool with the specified number of workers
func newWorkerPool(
	numWorkers int,
	geoWhoisCountry *reader.GeoWhoisCountryReader,
) *workerPool {
	if numWorkers <= 0 {
		numWorkers = mergeWorkerCount()
	}

	// Keep buffers bounded: work items hold decoded maps and results hold MMDB
	// maps, so oversized buffers inflate heap use without helping GitHub Actions.
	workChanSize := numWorkers * 256
	resultChanSize := numWorkers * 64

	pool := &workerPool{
		numWorkers: numWorkers,
		workChan:   make(chan workItem, workChanSize),
		resultChan: make(chan resultItem, resultChanSize),
		contexts:   make([]*workerContext, numWorkers),
	}

	// Create worker contexts with shared readers but per-worker reusable records
	for i := 0; i < numWorkers; i++ {
		pool.contexts[i] = &workerContext{
			geoWhoisCountry: geoWhoisCountry,
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
	var stats Stats
	stats.TotalNetworks = p.totalNetworks.Load()

	for _, ctx := range p.contexts {
		stats.GeoLiteCityHits += ctx.stats.geoLiteCityHits
		stats.GeoWhoisCountryHits += ctx.stats.geoWhoisCountryHits
		stats.EmptyRecords += ctx.stats.emptyRecords
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

	ctx.enrichWithCountryFallback(network.IP, record)
}

// enrichWithCountryFallback adds country information from GeoLite2 Country when country is missing.
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
