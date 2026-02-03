package config

// Database download URLs
const (
	GeoLite2CityURL    = "https://github.com/P3TERX/GeoLite.mmdb/releases/latest/download/GeoLite2-City.mmdb"
	GeoLite2ASNURL     = "https://github.com/P3TERX/GeoLite.mmdb/releases/latest/download/GeoLite2-ASN.mmdb"
	IPinfoLiteURL      = "https://github.com/NetworkCats/IPinfoLite-Download/releases/latest/download/ipinfo_lite.mmdb"
	DBIPCityIPv4URL    = "https://unpkg.com/@ip-location-db/dbip-city-mmdb/dbip-city-ipv4.mmdb"
	DBIPCityIPv6URL    = "https://unpkg.com/@ip-location-db/dbip-city-mmdb/dbip-city-ipv6.mmdb"
	RouteViewsASNURL   = "https://cdn.jsdelivr.net/npm/@ip-location-db/asn-mmdb/asn.mmdb"
	GeoWhoisCountryURL = "https://cdn.jsdelivr.net/npm/@ip-location-db/geolite2-geo-whois-asn-country-mmdb/geolite2-geo-whois-asn-country.mmdb"
	QQWryURL           = "https://cdn.jsdelivr.net/npm/qqwry.ipdb/qqwry.ipdb"
	OpenproxyDBURL     = "https://github.com/NetworkCats/OpenProxyDB/releases/latest/download/proxy_blocks.csv"
)

// Local file paths for downloaded databases
const (
	GeoLite2CityFile    = "download/GeoLite2-City.mmdb"
	GeoLite2ASNFile     = "download/GeoLite2-ASN.mmdb"
	IPinfoLiteFile      = "download/ipinfo_lite.mmdb"
	DBIPCityIPv4File    = "download/dbip-city-ipv4.mmdb"
	DBIPCityIPv6File    = "download/dbip-city-ipv6.mmdb"
	RouteViewsASNFile   = "download/routeviews-asn.mmdb"
	GeoWhoisCountryFile = "download/geolite2-geo-whois-asn-country.mmdb"
	QQWryFile           = "download/qqwry.ipdb"
	OpenproxyDBFile     = "download/proxy_blocks.csv"
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
	DownloadConcurrency = 7
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
		{Name: "RouteViews-ASN", URL: RouteViewsASNURL, Path: RouteViewsASNFile},
		{Name: "GeoWhois-Country", URL: GeoWhoisCountryURL, Path: GeoWhoisCountryFile},
		{Name: "QQWry-Chunzhen", URL: QQWryURL, Path: QQWryFile},
		{Name: "OpenProxyDB", URL: OpenproxyDBURL, Path: OpenproxyDBFile},
	}
}
