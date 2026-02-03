package interner

import (
	"fmt"
	"sync"
	"sync/atomic"
)

// Interner provides string interning to reduce memory usage by deduplicating
// identical strings. This is particularly effective for repeated values like
// country codes, language codes, and timezone strings.
type Interner struct {
	pool    sync.Map
	hits    atomic.Int64
	misses  atomic.Int64
	savings atomic.Int64
}

// global is the default interner instance
var global = &Interner{}

// commonStrings contains frequently used strings that should be pre-interned
var commonStrings = []string{
	// Language codes (from config.SupportedLanguages)
	"de", "en", "es", "fr", "ja", "pt-BR", "ru", "zh-CN",

	// Continent codes
	"AF", "AN", "AS", "EU", "NA", "OC", "SA",

	// Common country codes (top 50 by IP allocation)
	"US", "CN", "JP", "DE", "GB", "FR", "KR", "BR", "CA", "IT",
	"RU", "AU", "IN", "NL", "ES", "MX", "ID", "PL", "SE", "CH",
	"TW", "BE", "AR", "NO", "AT", "ZA", "DK", "FI", "IE", "NZ",
	"SG", "HK", "CZ", "PT", "IL", "TH", "MY", "RO", "UA", "CL",
	"PH", "VN", "CO", "GR", "HU", "AE", "PK", "EG", "SA", "NG",

	// MMDB map keys
	"city", "continent", "country", "location", "postal",
	"registered_country", "subdivisions", "asn",
	"geoname_id", "names", "code", "iso_code",
	"accuracy_radius", "latitude", "longitude", "metro_code", "time_zone",
	"autonomous_system_number", "autonomous_system_organization", "as_domain",
}

// Init pre-populates the interner with common strings.
// This should be called once at program startup.
func Init() {
	for _, s := range commonStrings {
		global.pool.Store(s, s)
	}
}

// Intern returns the canonical version of the string.
// If the string was seen before, the previously stored version is returned.
// This allows the GC to collect the duplicate string.
func Intern(s string) string {
	if s == "" {
		return ""
	}

	if existing, ok := global.pool.Load(s); ok {
		global.hits.Add(1)
		return existing.(string)
	}

	global.misses.Add(1)
	global.savings.Add(int64(len(s)))
	actual, _ := global.pool.LoadOrStore(s, s)
	return actual.(string)
}

// InternBytes converts a byte slice to an interned string.
// This is useful when building strings from byte data.
func InternBytes(b []byte) string {
	return Intern(string(b))
}

// Stats returns interning statistics as a formatted string.
func Stats() string {
	hits := global.hits.Load()
	misses := global.misses.Load()
	total := hits + misses
	hitRate := float64(0)
	if total > 0 {
		hitRate = float64(hits) / float64(total) * 100
	}

	var poolSize int
	global.pool.Range(func(_, _ any) bool {
		poolSize++
		return true
	})

	return fmt.Sprintf("Interner: pool_size=%d, hits=%d, misses=%d, hit_rate=%.1f%%, potential_savings=%d bytes",
		poolSize, hits, misses, hitRate, global.savings.Load())
}

// Reset clears the interner state. Primarily used for testing.
func Reset() {
	global = &Interner{}
	Init()
}
