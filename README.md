# Merged IP Database

A Go program that merges multiple IP geolocation databases into a single, comprehensive MMDB file. The merged database combines the best data from each source using priority-based field-level merging.

## Data Sources

| Source | Primary Use | Coverage |
|--------|-------------|----------|
| [GeoLite2-City](https://github.com/P3TERX/GeoLite.mmdb) | Country, city, coordinates, timezone, subdivisions, multi-language names | IPv4 + IPv6 |
| [GeoLite2-ASN](https://github.com/P3TERX/GeoLite.mmdb) | ASN fallback | IPv4 + IPv6 |
| [IPinfo Lite](https://github.com/NetworkCats/IPinfoLite-Download) | ASN, AS organization, AS domain | IPv4 + IPv6 |
| [DB-IP City](https://db-ip.com/) | Supplementary geo data | IPv4 + IPv6 |

## Merge Priority

The merge logic uses a priority-based approach to select the most accurate data for each field:

| Data Field | Primary Source | Fallback Source |
|------------|----------------|-----------------|
| Country, City, Coordinates | GeoLite2-City | DB-IP |
| Timezone, Subdivisions | GeoLite2-City | DB-IP |
| Multi-language Names | GeoLite2-City | - |
| ASN, AS Organization | IPinfo Lite | GeoLite2-ASN |
| AS Domain | IPinfo Lite | - |

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
  }
}
```

### Supported Languages

- German (de)
- English (en)
- Spanish (es)
- French (fr)
- Japanese (ja)
- Portuguese - Brazil (pt-BR)
- Russian (ru)
- Chinese - Simplified (zh-CN)

## Download

Download the latest merged database from [Releases](../../releases/latest):

```bash
wget https://github.com/YOUR_USERNAME/Merged-IP-Data/releases/latest/download/Merged-IP.mmdb
```

## Usage Examples

### Using mmdblookup (CLI)

```bash
mmdblookup --file Merged-IP.mmdb --ip 8.8.8.8
```

### Using Go

```go
package main

import (
    "fmt"
    "net"

    "github.com/oschwald/maxminddb-golang"
)

type Record struct {
    Country struct {
        ISOCode string            `maxminddb:"iso_code"`
        Names   map[string]string `maxminddb:"names"`
    } `maxminddb:"country"`
    City struct {
        Names map[string]string `maxminddb:"names"`
    } `maxminddb:"city"`
    ASN struct {
        Number       uint32 `maxminddb:"autonomous_system_number"`
        Organization string `maxminddb:"autonomous_system_organization"`
        Domain       string `maxminddb:"as_domain"`
    } `maxminddb:"asn"`
}

func main() {
    db, err := maxminddb.Open("Merged-IP.mmdb")
    if err != nil {
        panic(err)
    }
    defer db.Close()

    ip := net.ParseIP("8.8.8.8")
    var record Record
    err = db.Lookup(ip, &record)
    if err != nil {
        panic(err)
    }

    fmt.Printf("Country: %s (%s)\n", record.Country.Names["en"], record.Country.ISOCode)
    fmt.Printf("City: %s\n", record.City.Names["en"])
    fmt.Printf("ASN: AS%d %s (%s)\n", record.ASN.Number, record.ASN.Organization, record.ASN.Domain)
}
```

### Using Python

```python
import maxminddb

with maxminddb.open_database('Merged-IP.mmdb') as reader:
    record = reader.get('8.8.8.8')
    
    print(f"Country: {record['country']['names']['en']} ({record['country']['iso_code']})")
    print(f"City: {record.get('city', {}).get('names', {}).get('en', 'N/A')}")
    print(f"ASN: AS{record['asn']['autonomous_system_number']} {record['asn']['autonomous_system_organization']}")
```

## Building from Source

### Prerequisites

- Go 1.22 or later

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

- GeoLite2: [MaxMind GeoLite2 End User License Agreement](https://www.maxmind.com/en/geolite2/eula)
- IPinfo Lite: [CC BY-SA 4.0](https://creativecommons.org/licenses/by-sa/4.0/)
- DB-IP: [CC BY 4.0](https://creativecommons.org/licenses/by/4.0/)

The merge tool source code is provided as-is for educational and personal use.
