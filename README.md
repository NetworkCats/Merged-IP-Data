# Merged IP Database

A Go program that merges multiple IP geolocation databases into a single, comprehensive MMDB file. The merged database combines the best data from each source using priority-based field-level merging.

## Data Sources

| Source | Primary Use | Coverage |
|--------|-------------|----------|
| [GeoLite2-City](https://github.com/P3TERX/GeoLite.mmdb) | Country, city, coordinates, timezone, subdivisions, multi-language names | IPv4 + IPv6 |
| [GeoLite2-ASN](https://github.com/P3TERX/GeoLite.mmdb) | ASN fallback (secondary) | IPv4 + IPv6 |
| [IPinfo Lite](https://github.com/NetworkCats/IPinfoLite-Download) | ASN, AS organization, AS domain (primary) | IPv4 + IPv6 |
| [DB-IP City](https://db-ip.com/) | Supplementary geo data | IPv4 + IPv6 |
| [RouteViews ASN](https://www.npmjs.com/package/@ip-location-db/asn-mmdb) | ASN fallback (tertiary) | IPv4 + IPv6 |
| [GeoLite2-Geo-Whois-ASN-Country](https://www.npmjs.com/package/@ip-location-db/geolite2-geo-whois-asn-country-mmdb) | Country fallback | IPv4 + IPv6 |
| [QQWry (Chunzhen)](https://github.com/metowolf/qqwry.ipdb) | Enhanced Chinese IP geolocation with native zh-CN names | IPv4 |
| [OpenProxyDB](https://github.com/NetworkCats/OpenProxyDB) | Proxy, VPN, Tor, hosting, and CDN detection | IPv4 + IPv6 |

## Output Format

The merged database contains the following fields:

```
{
  "city": {
    "geoname_id": <uint32>,
    "names": { "en": "...", "de": "...", ... }
  },
  "continent": {
    "code": "...",
    "geoname_id": <uint32>,
    "names": { "en": "...", "de": "...", ... }
  },
  "country": {
    "geoname_id": <uint32>,
    "iso_code": "...",
    "names": { "en": "...", "de": "...", ... }
  },
  "location": {
    "accuracy_radius": <uint16>,
    "latitude": <double>,
    "longitude": <double>,
    "metro_code": <uint16>,
    "time_zone": "..."
  },
  "postal": {
    "code": "..."
  },
  "registered_country": {
    "geoname_id": <uint32>,
    "iso_code": "...",
    "names": { "en": "...", "de": "...", ... }
  },
  "subdivisions": [
    {
      "geoname_id": <uint32>,
      "iso_code": "...",
      "names": { "en": "...", "de": "...", ... }
    }
  ],
  "asn": {
    "autonomous_system_number": <uint32>,
    "autonomous_system_organization": "...",
    "as_domain": "..."
  },
  "proxy": {
    "is_proxy": <bool>,
    "is_vpn": <bool>,
    "is_tor": <bool>,
    "is_hosting": <bool>,
    "is_cdn": <bool>,
    "is_school": <bool>,
    "is_anonymous": <bool>
  }
}
```

## Download

Download the latest merged database from [Releases](../../releases/latest):

```bash
wget https://github.com/YOUR_USERNAME/Merged-IP-Data/releases/latest/download/Merged-IP.mmdb
```

## Building from Source

### Prerequisites

- Go 1.25 or later

### Build

```bash
git clone https://github.com/YOUR_USERNAME/Merged-IP-Data.git
cd Merged-IP-Data
go build -o merge-tool ./cmd/merge
```

### Run

```bash
# Download databases and merge
./merge-tool

# Use existing downloaded databases
./merge-tool -skip-download

# Custom output path
./merge-tool -output custom.mmdb
```

## Automatic Updates

The database is automatically updated daily at 1:00 UTC via GitHub Actions. Each release includes:

- The merged MMDB file
- Release notes with data source information

## License

This project merges data from multiple sources. Please refer to each source's license:

- GeoLite2: [Creative Commons Attribution-ShareAlike 4.0 International License](https://creativecommons.org/licenses/by-sa/4.0/)
- IPinfo Lite: [Creative Commons Attribution-ShareAlike 4.0 International License](https://creativecommons.org/licenses/by-sa/4.0/)
- DB-IP: [CC BY 4.0](https://creativecommons.org/licenses/by/4.0/)
- RouteViews ASN: [CC0 1.0](https://creativecommons.org/publicdomain/zero/1.0/)
- OpenProxyDB: [CC0 1.0](https://creativecommons.org/publicdomain/zero/1.0/)

The merge tool source code is provided as-is for educational and personal use.
