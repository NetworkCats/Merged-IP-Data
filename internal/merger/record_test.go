package merger

import (
	"net/netip"
	"testing"

	"merged-ip-data/internal/reader"

	"github.com/maxmind/mmdbwriter/mmdbtype"
	"go4.org/netipx"
)

func TestWithNameClonesInput(t *testing.T) {
	original := map[string]string{"en": "Beijing"}
	updated := withName(original, "zh-CN", "Beijing CN")

	if _, ok := original["zh-CN"]; ok {
		t.Fatal("withName mutated the input map")
	}
	if updated["en"] != "Beijing" || updated["zh-CN"] != "Beijing CN" {
		t.Fatalf("updated = %#v, want original and added names", updated)
	}
}

func TestWithNameIgnoresEmptyName(t *testing.T) {
	original := map[string]string{"en": "China"}
	updated := withName(original, "zh-CN", "")

	if len(updated) != 1 || updated["en"] != "China" {
		t.Fatalf("updated = %#v, want original names only", updated)
	}
}

func TestToMMDBTypeSkipsEmptyNames(t *testing.T) {
	record := MergedRecord{
		City: CityRecord{
			Names: map[string]string{"en": "", "zh-CN": "Beijing"},
		},
	}

	mmdbRecord := record.ToMMDBType()
	city, ok := mmdbRecord[keyCity].(mmdbtype.Map)
	if !ok {
		t.Fatalf("city = %#v, want map", mmdbRecord[keyCity])
	}
	names, ok := city[keyNames].(mmdbtype.Map)
	if !ok {
		t.Fatalf("names = %#v, want map", city[keyNames])
	}
	if _, ok := names[mmdbtype.String("en")]; ok {
		t.Fatalf("names = %#v, want empty English name omitted", names)
	}
	if got := names[mmdbtype.String("zh-CN")]; got != mmdbtype.String("Beijing") {
		t.Fatalf("zh-CN name = %#v, want Beijing", got)
	}
}

func TestMergedRecordIsEmptyIncludesNonPrimaryFields(t *testing.T) {
	tests := []struct {
		name   string
		record MergedRecord
	}{
		{
			name: "registered country",
			record: MergedRecord{
				RegisteredCountry: CountryRecord{ISOCode: "US"},
			},
		},
		{
			name: "postal",
			record: MergedRecord{
				Postal: PostalRecord{Code: "10001"},
			},
		},
		{
			name: "subdivision",
			record: MergedRecord{
				Subdivisions: []SubdivisionRecord{{ISOCode: "CA"}},
			},
		},
		{
			name: "proxy",
			record: MergedRecord{
				Proxy: ProxyRecord{IsCDN: true},
			},
		},
		{
			name: "asn organization only",
			record: MergedRecord{
				ASN: ASNRecord{Organization: "Example ASN"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.record.IsEmpty() {
				t.Fatalf("record = %+v, want non-empty", tt.record)
			}
		})
	}
}

func TestMergeMMDBMapsRecursivelyMergesAndUnionsProxy(t *testing.T) {
	existing := mmdbtype.Map{
		keyCountry: mmdbtype.Map{
			keyISOCode: mmdbtype.String("US"),
		},
		keyProxy: mmdbtype.Map{
			keyIsCDN: mmdbtype.Bool(true),
		},
	}
	newMap := mmdbtype.Map{
		keyCountry: mmdbtype.Map{
			keyNames: mmdbtype.Map{
				mmdbtype.String("en"): mmdbtype.String("United States"),
			},
		},
		keyProxy: mmdbtype.Map{
			keyIsProxy: mmdbtype.Bool(true),
		},
	}

	merged := mergeMMDBMaps(existing, newMap)

	country := merged[keyCountry].(mmdbtype.Map)
	if country[keyISOCode] != mmdbtype.String("US") {
		t.Fatalf("country iso = %#v, want US", country[keyISOCode])
	}
	if _, ok := country[keyNames].(mmdbtype.Map); !ok {
		t.Fatalf("country = %#v, want nested names merged", country)
	}

	proxy := merged[keyProxy].(mmdbtype.Map)
	if proxy[keyIsCDN] != mmdbtype.Bool(true) || proxy[keyIsProxy] != mmdbtype.Bool(true) {
		t.Fatalf("proxy = %#v, want union of CDN and proxy flags", proxy)
	}
}

func TestMergeMMDBMapsReturnsExistingMapWhenNothingChanges(t *testing.T) {
	existing := mmdbtype.Map{
		keyCountry: mmdbtype.Map{keyISOCode: mmdbtype.String("US")},
		keyProxy:   mmdbtype.Map{keyIsVPN: mmdbtype.Bool(true)},
	}
	newMap := mmdbtype.Map{
		keyCountry: mmdbtype.Map{keyISOCode: mmdbtype.String("CA")},
		keyProxy:   mmdbtype.Map{keyIsVPN: mmdbtype.Bool(true)},
	}

	_, changed := mergeMMDBMapsChanged(existing, newMap)
	if changed {
		t.Fatal("merge allocated a new map even though priority rules made no changes")
	}
}

func TestApplySchoolASNMatchMarksUniversityOrganization(t *testing.T) {
	record := MergedRecord{
		ASN: ASNRecord{
			Organization: "Example State University",
		},
	}

	applySchoolASNMatch(&record)

	if !record.Proxy.IsSchool {
		t.Fatalf("proxy = %+v, want school", record.Proxy)
	}
}

func TestApplySchoolASNMatchMarksSchoolOrganizationCaseInsensitive(t *testing.T) {
	record := MergedRecord{
		ASN: ASNRecord{
			Organization: "EXAMPLE SCHOOL DISTRICT",
		},
	}

	applySchoolASNMatch(&record)

	if !record.Proxy.IsSchool {
		t.Fatalf("proxy = %+v, want school", record.Proxy)
	}
}

func TestApplySchoolASNMatchIgnoresOtherOrganizations(t *testing.T) {
	record := MergedRecord{
		ASN: ASNRecord{
			Organization: "Example Hosting LLC",
		},
	}

	applySchoolASNMatch(&record)

	if record.Proxy.IsSchool {
		t.Fatalf("proxy = %+v, want not school", record.Proxy)
	}
}

func TestNetipPrefixToIPNetHandlesIPv4AndIPv6(t *testing.T) {
	tests := []struct {
		name   string
		prefix netip.Prefix
		want   string
	}{
		{
			name:   "ipv4",
			prefix: netip.MustParsePrefix("192.0.2.0/24"),
			want:   "192.0.2.0/24",
		},
		{
			name:   "ipv4 mapped",
			prefix: netip.MustParsePrefix("::ffff:192.0.2.0/120"),
			want:   "192.0.2.0/24",
		},
		{
			name:   "ipv6",
			prefix: netip.MustParsePrefix("2001:db8:abcd::/48"),
			want:   "2001:db8:abcd::/48",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			network := netipPrefixToIPNet(tt.prefix)
			if network == nil {
				t.Fatal("network is nil")
			}
			if got := network.String(); got != tt.want {
				t.Fatalf("network = %s, want %s", got, tt.want)
			}
		})
	}
}

func TestGeoUncoveredPrefixesReturnsOnlyCoverageGaps(t *testing.T) {
	m := Merger{geoPrimaryRanges: []netipx.IPRange{
		netipx.RangeOfPrefix(netip.MustParsePrefix("10.0.0.0/25")),
		netipx.RangeOfPrefix(netip.MustParsePrefix("10.0.0.192/26")),
	}}
	got := m.geoUncoveredPrefixes(netip.MustParsePrefix("10.0.0.0/24"))
	want := []netip.Prefix{netip.MustParsePrefix("10.0.0.128/26")}
	if len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("uncovered prefixes = %v, want %v", got, want)
	}
}

func TestAppendCoalescedIPRangeMergesAdjacentCoverage(t *testing.T) {
	var ranges []netipx.IPRange
	ranges = appendCoalescedIPRange(ranges, netipx.RangeOfPrefix(netip.MustParsePrefix("10.0.0.0/25")))
	ranges = appendCoalescedIPRange(ranges, netipx.RangeOfPrefix(netip.MustParsePrefix("10.0.0.128/25")))
	if len(ranges) != 1 || ranges[0] != netipx.RangeOfPrefix(netip.MustParsePrefix("10.0.0.0/24")) {
		t.Fatalf("coalesced ranges = %v, want 10.0.0.0/24", ranges)
	}
}

func TestSubdivisionsFromDBIPPreservesBothLevels(t *testing.T) {
	got := subdivisionsFromDBIP(&reader.DBIPCityRecord{State1: "California", State2: "Santa Clara County"})
	if len(got) != 2 || got[0].Names["en"] != "California" || got[1].Names["en"] != "Santa Clara County" {
		t.Fatalf("subdivisions = %#v, want both DB-IP subdivision levels", got)
	}
}
