# Merged IP Database

A Go program that merges multiple IP geolocation databases into a single, comprehensive MMDB file. The merged database combines the best data from each source using priority-based field-level merging.

## Data Sources

| Source | Primary Use | Coverage |
|--------|-------------|----------|
| [GeoLite2-City](https://github.com/P3TERX/GeoLite.mmdb) | Country, city, coordinates, timezone, subdivisions, multi-language names | IPv4 + IPv6 |
| [GeoLite2-ASN](https://github.com/P3TERX/GeoLite.mmdb) | ASN fallback (secondary) | IPv4 + IPv6 |
| [IPinfo Lite](https://github.com/NetworkCats/IPinfoLite-Download) | ASN, AS organization, AS domain (primary) | IPv4 + IPv6 |
| [DB-IP City](https://github.com/sapics/ip-location-db#city) | Supplementary geo data | IPv4 + IPv6 |
| [Origin ASN](https://github.com/sapics/ip-location-db#asn) | ASN fallback (tertiary) | IPv4 + IPv6 |
| [GeoLite2 Country](https://github.com/sapics/ip-location-db#country) | Country fallback | IPv4 + IPv6 |
| [QQWry (Chunzhen)](https://github.com/nmgliangwei/qqwry.ipdb) | Province and city for every mainland China IP (overrides other sources), with English names from a built-in dictionary | IPv4 + IPv6 |
| [OpenProxyDB](https://github.com/NetworkCats/OpenProxyDB) | Proxy, VPN, Tor, hosting, and CDN detection | IPv4 + IPv6 |
| [iCloud Private Relay](https://mask-api.icloud.com/egress-ip-ranges.csv) | Proxy and VPN overlay for Apple iCloud Private Relay egress ranges | IPv4 + IPv6 |
| [X4B VPN lists](https://github.com/X4BNet/lists_vpn) | VPN ASN, hosting ASN, and VPN/hosting CIDR overlays for provider and datacenter ranges | IPv4 |
| [NordVPN server IP list](https://gist.github.com/JamoCA/eedaf4f7cce1cb0aeb5c1039af35f0b7) | VPN overlay for NordVPN server IPs | IPv4 |
| [bgp.tools Anycast](https://github.com/bgptools/anycast-prefixes) | CDN overlay for anycast prefixes (OR'd into `is_cdn`) | IPv4 + IPv6 |

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
wget https://github.com/NetworkCats/Merged-IP-Data/releases/latest/download/Merged-IP.mmdb
```

## Building from Source

### Prerequisites

- Go 1.25 or later

### Build

```bash
git clone https://github.com/NetworkCats/Merged-IP-Data.git
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
- Origin ASN: [CDLA Permissive 2.0](https://cdla.dev/permissive-2-0/)
- OpenProxyDB: [CC0 1.0](https://creativecommons.org/publicdomain/zero/1.0/)
- X4B VPN lists: [MIT License](https://github.com/X4BNet/lists_vpn#license)
- NordVPN server IP list: see the source gist for terms
- GeoNames (China place-name dictionary IDs, English names and coordinates): [CC BY 4.0](https://creativecommons.org/licenses/by/4.0/)

The merge tool source code is provided as-is for educational and personal use.
