package merger

import (
	"fmt"
	"net"
	"net/netip"
	"strings"

	"merged-ip-data/internal/reader"

	"github.com/maxmind/mmdbwriter/mmdbtype"
	"go4.org/netipx"
)

// Accuracy radius (km) for coordinates taken from the QQWry dictionary, which
// only places an IP at a city or province, never a street.
const (
	chinaCityAccuracyRadius     = 100
	chinaProvinceAccuracyRadius = 1000
)

// Direct-administered municipalities, where QQWry often omits the city
// because the province is the city.
var chinaMunicipalities = map[string]bool{"北京": true, "上海": true, "天津": true, "重庆": true}

// chinaLocation is a QQWry place resolved through the dictionary, with its
// MMDB values prebuilt. The values are shared between records and never mutated.
type chinaLocation struct {
	provinceISO string
	provinceID  uint32
	subdivision mmdbtype.Map
	cityID      uint32
	city        mmdbtype.Map // nil when QQWry has no city the dictionary knows
	location    mmdbtype.Map
}

// resolveChinaLocation maps QQWry's Chinese province and city names onto the
// dictionary. It returns nil when the province is not recognized, in which case
// the record is left to the other sources.
func resolveChinaLocation(region, city string) *chinaLocation {
	provinceName, rest := matchChinaProvince(region)
	if provinceName == "" {
		return nil
	}
	province := chinaProvinces[provinceName]
	// QQWry sometimes packs the city into the region, e.g. "新疆乌鲁木齐".
	if city == "" {
		city = rest
	}
	if city == "" && chinaMunicipalities[provinceName] {
		city = provinceName
	}

	loc := &chinaLocation{
		provinceISO: province.ISOCode,
		provinceID:  province.GeonameID,
		subdivision: chinaPlaceMap(province.GeonameID, province.ISOCode, province.En, provinceName),
	}

	if cityName, ok := matchChinaCity(provinceName, city); ok {
		c := chinaCities[provinceName][cityName]
		loc.cityID = c.GeonameID
		loc.city = chinaPlaceMap(c.GeonameID, "", c.En, cityName)
		loc.location = chinaLocationMap(c.Latitude, c.Longitude, chinaCityAccuracyRadius, province.TimeZone)
	} else {
		loc.location = chinaLocationMap(province.Latitude, province.Longitude, chinaProvinceAccuracyRadius, province.TimeZone)
	}
	return loc
}

// matchChinaProvince returns the province QQWry's region starts with, and any
// trailing text. QQWry occasionally appends a city or school to the province.
func matchChinaProvince(region string) (string, string) {
	if _, ok := chinaProvinces[region]; ok {
		return region, ""
	}
	best := ""
	for name := range chinaProvinces {
		if len(name) > len(best) && strings.HasPrefix(region, name) {
			best = name
		}
	}
	if best == "" {
		return "", ""
	}
	return best, strings.TrimPrefix(region, best)
}

// matchChinaCity finds the dictionary city for QQWry's city name. Besides exact
// names it accepts a redundant "市" suffix and a trailing district or street
// ("青岛胶南市" -> "青岛").
func matchChinaCity(province, city string) (string, bool) {
	if city == "" {
		return "", false
	}
	cities := chinaCities[province]
	if _, ok := cities[city]; ok {
		return city, true
	}
	if trimmed := strings.TrimSuffix(city, "市"); trimmed != city {
		if _, ok := cities[trimmed]; ok {
			return trimmed, true
		}
	}
	best := ""
	for name := range cities {
		if len(name) > len(best) && strings.HasPrefix(city, name) {
			best = name
		}
	}
	return best, best != ""
}

func chinaPlaceMap(geonameID uint32, isoCode, en, zh string) mmdbtype.Map {
	place := mmdbtype.Map{
		keyNames: mmdbtype.Map{
			mmdbtype.String("en"):    mmdbtype.String(en),
			mmdbtype.String("zh-CN"): mmdbtype.String(zh),
		},
	}
	if geonameID != 0 {
		place[keyGeonameID] = mmdbtype.Uint32(geonameID)
	}
	if isoCode != "" {
		place[keyISOCode] = mmdbtype.String(isoCode)
	}
	return place
}

func chinaLocationMap(latitude, longitude float64, accuracyRadius uint16, timeZone string) mmdbtype.Map {
	return mmdbtype.Map{
		keyAccuracyRadius: mmdbtype.Uint16(accuracyRadius),
		keyLatitude:       mmdbtype.Float64(latitude),
		keyLongitude:      mmdbtype.Float64(longitude),
		keyTimeZone:       mmdbtype.String(timeZone),
	}
}

// applyChinaLocation makes QQWry the location source for a Chinese record.
// The province always comes from QQWry. The city does too when QQWry names one;
// when it only names the province, an existing city is kept if it lies in that
// province and dropped otherwise. Coordinates and postal code follow the city,
// so they are replaced whenever the city changes. It returns the existing map
// unchanged for records that are not in China.
func applyChinaLocation(existing mmdbtype.Map, loc *chinaLocation) (mmdbtype.Map, bool) {
	country, _ := existing[keyCountry].(mmdbtype.Map)
	if country[keyISOCode] != mmdbtype.String("CN") {
		return existing, false
	}

	result := shallowCopyMMDBMap(existing, 3)

	existingSubdivision := firstSubdivision(existing)
	sameProvince := geonameID(existingSubdivision) == loc.provinceID ||
		existingSubdivision[keyISOCode] == mmdbtype.String(loc.provinceISO)
	result[keySubdivisions] = mmdbtype.Slice{mergeChinaPlace(existingSubdivision, loc.subdivision, loc.provinceID)}

	existingCity, _ := existing[keyCity].(mmdbtype.Map)
	switch {
	case loc.city != nil && loc.cityID != 0 && geonameID(existingCity) == loc.cityID:
		// Same city as the existing source: keep its finer coordinates and postal code.
		result[keyCity] = mergeChinaPlace(existingCity, loc.city, loc.cityID)
	case loc.city != nil:
		result[keyCity] = loc.city
		result[keyLocation] = loc.location
		delete(result, keyPostal)
	case sameProvince && existingCity != nil:
		// QQWry has no city, and the existing one is in the right province.
	default:
		delete(result, keyCity)
		delete(result, keyPostal)
		result[keyLocation] = loc.location
	}
	return result, true
}

func firstSubdivision(record mmdbtype.Map) mmdbtype.Map {
	subdivisions, _ := record[keySubdivisions].(mmdbtype.Slice)
	if len(subdivisions) == 0 {
		return nil
	}
	first, _ := subdivisions[0].(mmdbtype.Map)
	return first
}

func geonameID(place mmdbtype.Map) uint32 {
	id, _ := place[keyGeonameID].(mmdbtype.Uint32)
	return uint32(id)
}

// mergeChinaPlace keeps the other languages of an existing place when it is
// the same GeoNames entity, with the dictionary's en and zh-CN names on top.
// A different place is replaced outright so names never mix between places.
func mergeChinaPlace(existing, place mmdbtype.Map, id uint32) mmdbtype.Map {
	if existing == nil || id == 0 || geonameID(existing) != id {
		return place
	}
	existingNames, _ := existing[keyNames].(mmdbtype.Map)
	if existingNames == nil {
		return place
	}
	names := shallowCopyMMDBMap(existingNames, 2)
	for lang, name := range place[keyNames].(mmdbtype.Map) {
		names[lang] = name
	}
	merged := shallowCopyMMDBMap(place, 0)
	merged[keyNames] = names
	return merged
}

// processQQWryChinaLocations overrides the location of every Chinese record
// with QQWry's province and city. It walks QQWry's own networks, which are far
// finer than GeoLite2's in China, so large GeoLite2 blocks get split by city.
// It must run after every phase that sets the country.
func (m *Merger) processQQWryChinaLocations() error {
	cache := make(map[string]*chinaLocation)
	var (
		pending     *chinaLocation
		start, last netip.Addr
		networks    int64
		records     int64
	)

	flush := func() error {
		if pending == nil {
			return nil
		}
		loc := pending
		pending = nil
		for _, prefix := range netipx.IPRangeFrom(start, last).Prefixes() {
			network := netipPrefixToIPNet(prefix)
			if network == nil {
				continue
			}
			err := m.tree.InsertFunc(network, func(existing mmdbtype.DataType) (mmdbtype.DataType, error) {
				existingMap, ok := existing.(mmdbtype.Map)
				if !ok {
					return existing, nil
				}
				result, applied := applyChinaLocation(existingMap, loc)
				if applied {
					records++
				}
				return result, nil
			})
			if err != nil {
				if isSkippableInsertError(err) {
					continue
				}
				return fmt.Errorf("failed to insert QQWry network %s: %w", prefix, err)
			}
			networks++
		}
		return nil
	}

	err := m.qqwry.Networks(func(network *net.IPNet, record *reader.QQWryRecord) error {
		if !record.IsChina() {
			return nil
		}
		key := record.RegionName + "\x00" + record.CityName
		loc, ok := cache[key]
		if !ok {
			loc = resolveChinaLocation(record.RegionName, record.CityName)
			cache[key] = loc
		}
		if loc == nil {
			return nil
		}

		prefix, ok := netipx.FromStdIPNet(network)
		if !ok {
			return nil
		}
		r := netipx.RangeOfPrefix(prefix)
		// Networks arrive in ascending order, so neighbours with the same
		// place are merged into one range before touching the tree.
		if pending == loc && last.Next() == r.From() {
			last = r.To()
			return nil
		}
		if err := flush(); err != nil {
			return err
		}
		pending, start, last = loc, r.From(), r.To()
		return nil
	})
	if err != nil {
		return err
	}
	if err := flush(); err != nil {
		return err
	}

	m.stats.QQWryHits = records
	fmt.Printf("QQWry China locations: %d networks inserted, %d records relocated, %d distinct places\n", networks, records, len(cache))
	return nil
}
