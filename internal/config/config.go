package config

// Database download URLs
const (
	GeoLite2CityURL = "https://github.com/P3TERX/GeoLite.mmdb/releases/latest/download/GeoLite2-City.mmdb"
	GeoLite2ASNURL  = "https://github.com/P3TERX/GeoLite.mmdb/releases/latest/download/GeoLite2-ASN.mmdb"
	IPinfoLiteURL   = "https://github.com/NetworkCats/IPinfoLite-Download/releases/latest/download/ipinfo_lite.mmdb"
	DBIPCityIPv4URL = "https://unpkg.com/@ip-location-db/dbip-city-mmdb/dbip-city-ipv4.mmdb"
	DBIPCityIPv6URL = "https://unpkg.com/@ip-location-db/dbip-city-mmdb/dbip-city-ipv6.mmdb"
)

// Local file paths for downloaded databases
const (
	GeoLite2CityFile = "download/GeoLite2-City.mmdb"
	GeoLite2ASNFile  = "download/GeoLite2-ASN.mmdb"
	IPinfoLiteFile   = "download/ipinfo_lite.mmdb"
	DBIPCityIPv4File = "download/dbip-city-ipv4.mmdb"
	DBIPCityIPv6File = "download/dbip-city-ipv6.mmdb"
)

// Output file path
const (
	OutputFile = "Merged-IP.mmdb"
)

// Supported languages for multi-language names
var SupportedLanguages = []string{
	"de",    // German
	"en",    // English
	"es",    // Spanish
	"fr",    // French
	"ja",    // Japanese
	"pt-BR", // Portuguese (Brazil)
	"ru",    // Russian
	"zh-CN", // Chinese (Simplified)
}

// Database metadata
const (
	DatabaseType        = "Merged-IP-City-ASN"
	DatabaseDescription = "Merged IP geolocation database combining GeoLite2, IPinfo Lite, and DB-IP data"
)

// Download settings
const (
	DownloadTimeout     = 300 // seconds
	DownloadMaxRetries  = 3
	DownloadRetryDelay  = 5 // seconds
	DownloadConcurrency = 5
)

// DatabaseSource represents a database source with its URL and local path
type DatabaseSource struct {
	Name string
	URL  string
	Path string
}

// GetAllSources returns all database sources for downloading
func GetAllSources() []DatabaseSource {
	return []DatabaseSource{
		{Name: "GeoLite2-City", URL: GeoLite2CityURL, Path: GeoLite2CityFile},
		{Name: "GeoLite2-ASN", URL: GeoLite2ASNURL, Path: GeoLite2ASNFile},
		{Name: "IPinfo-Lite", URL: IPinfoLiteURL, Path: IPinfoLiteFile},
		{Name: "DB-IP-IPv4", URL: DBIPCityIPv4URL, Path: DBIPCityIPv4File},
		{Name: "DB-IP-IPv6", URL: DBIPCityIPv6URL, Path: DBIPCityIPv6File},
	}
}
