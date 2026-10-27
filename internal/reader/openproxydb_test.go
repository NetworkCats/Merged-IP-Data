package reader

import (
	"net"
	"net/netip"
	"os"
	"testing"
)

func TestOpenproxyDBParseCanonicalizesCIDRPrefixes(t *testing.T) {
	path := writeTempFile(t, "proxy-*.csv", "ip,anonblock,proxy,vpn,cdn,rangeblock,school-block,tor,webhost,extra\n10.0.0.1/24,false,false,false,false,true,false,false,true,ignored\n")

	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	r := &OpenproxyDBReader{
		singleIPs:  make(map[netip.Addr]OpenproxyDBRecord),
		cidrRanges: make([]cidrEntry, 0),
	}
	if err := r.parse(file); err != nil {
		t.Fatal(err)
	}
	if got, want := r.cidrRanges[0].prefix, netip.MustParsePrefix("10.0.0.0/24"); got != want {
		t.Fatalf("prefix = %s, want %s", got, want)
	}

	var record OpenproxyDBRecord
	if !r.LookupTo(net.ParseIP("10.0.0.42"), &record) {
		t.Fatal("expected CIDR lookup to match canonicalized /24")
	}
	if !record.IsProxy || !record.IsHosting {
		t.Fatalf("record = %+v, want proxy and hosting flags", record)
	}

	record = OpenproxyDBRecord{IsTor: true, IsAnonymous: true}
	if r.LookupTo(net.ParseIP("198.51.100.1"), &record) {
		t.Fatal("unexpected lookup match")
	}
	if record.HasData() || record.IsAnonymous {
		t.Fatalf("record = %+v, want stale flags cleared on miss", record)
	}
}

func TestOpenproxyDBCIDRLookupUsesMostSpecificMatch(t *testing.T) {
	r := &OpenproxyDBReader{
		cidrRecords: make(map[netip.Prefix]OpenproxyDBRecord),
	}
	r.addCIDRRange(netip.MustParsePrefix("10.0.0.0/8"), OpenproxyDBRecord{IsHosting: true})
	r.addCIDRRange(netip.MustParsePrefix("10.1.2.0/24"), OpenproxyDBRecord{IsVPN: true, IsAnonymous: true})

	record, ok := r.findInCIDR(netip.MustParseAddr("10.1.2.3"))
	if !ok {
		t.Fatal("expected CIDR match")
	}
	if !record.IsVPN || record.IsHosting {
		t.Fatalf("record = %+v, want most-specific VPN record only", record)
	}
}

func TestOpenproxyDBDuplicateCIDRRecordsAreUnioned(t *testing.T) {
	r := &OpenproxyDBReader{
		cidrRecords: make(map[netip.Prefix]OpenproxyDBRecord),
	}
	r.addCIDRRange(netip.MustParsePrefix("203.0.113.0/24"), OpenproxyDBRecord{IsHosting: true})
	r.addCIDRRange(netip.MustParsePrefix("203.0.113.0/24"), OpenproxyDBRecord{IsTor: true, IsAnonymous: true})

	record, ok := r.findInCIDR(netip.MustParseAddr("203.0.113.8"))
	if !ok {
		t.Fatal("expected CIDR match")
	}
	if !record.IsHosting || !record.IsTor || !record.IsAnonymous {
		t.Fatalf("record = %+v, want unioned hosting and tor flags", record)
	}

	ranges := r.CIDRRanges()
	if len(ranges) != 1 {
		t.Fatalf("CIDRRanges length = %d, want duplicate prefix coalesced", len(ranges))
	}
	if !ranges[0].Record.IsHosting || !ranges[0].Record.IsTor || !ranges[0].Record.IsAnonymous {
		t.Fatalf("CIDRRanges[0] = %+v, want unioned hosting and tor flags", ranges[0].Record)
	}
}

func TestOpenproxyDBDuplicateSingleIPRecordsAreUnioned(t *testing.T) {
	path := writeTempFile(t, "proxy-*.csv", "ip,anonblock,proxy,vpn,cdn,rangeblock,school-block,tor,webhost\n203.0.113.8,false,false,true,false,false,false,false,false\n203.0.113.8,false,false,false,false,false,false,false,true\n")

	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	r := &OpenproxyDBReader{singleIPs: make(map[netip.Addr]OpenproxyDBRecord)}
	if err := r.parse(file); err != nil {
		t.Fatal(err)
	}

	var record OpenproxyDBRecord
	if !r.LookupTo(net.ParseIP("203.0.113.8"), &record) {
		t.Fatal("expected single-IP lookup match")
	}
	if !record.IsVPN || !record.IsHosting || !record.IsAnonymous {
		t.Fatalf("record = %+v, want unioned VPN, hosting, and anonymous flags", record)
	}
}

func TestCanonicalPrefixDoesNotBroadenMappedIPv4Supernet(t *testing.T) {
	prefix := canonicalPrefix(netip.MustParsePrefix("::ffff:192.0.2.1/80"))
	want := netip.MustParsePrefix("::/80")
	if prefix != want {
		t.Fatalf("canonicalPrefix() = %s, want %s", prefix, want)
	}
}

func TestLoadBadIPListUnmapsIPv4AndInheritsCIDRFlags(t *testing.T) {
	path := writeTempFile(t, "badip-*.txt", "::ffff:192.0.2.1\n")
	r := &OpenproxyDBReader{
		singleIPs:   make(map[netip.Addr]OpenproxyDBRecord),
		cidrRecords: make(map[netip.Prefix]OpenproxyDBRecord),
	}
	r.addCIDRRange(netip.MustParsePrefix("192.0.2.0/24"), OpenproxyDBRecord{IsHosting: true})

	count, err := r.LoadBadIPList(path)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("count = %d, want 1", count)
	}

	var record OpenproxyDBRecord
	if !r.LookupTo(net.ParseIP("192.0.2.1"), &record) {
		t.Fatal("expected IPv4 lookup to match IPv4-mapped bad IP")
	}
	if !record.IsProxy || !record.IsAnonymous || !record.IsHosting {
		t.Fatalf("record = %+v, want proxy, anonymous, and inherited hosting flags", record)
	}
}

func TestLoadICloudPrivateRelayRangesMarksProxyAndVPN(t *testing.T) {
	path := writeTempFile(t, "icloud-*.csv", "192.0.2.0/24,US,US-CA,Cupertino,\n2001:db8:abcd::/48,US,US-CA,Cupertino,\n")
	r := &OpenproxyDBReader{
		singleIPs:   make(map[netip.Addr]OpenproxyDBRecord),
		cidrRanges:  make([]cidrEntry, 0),
		cidrRecords: make(map[netip.Prefix]OpenproxyDBRecord),
	}
	r.addCIDRRange(netip.MustParsePrefix("198.51.100.0/24"), OpenproxyDBRecord{IsHosting: true})
	if err := r.rebuildCIDRSet(); err != nil {
		t.Fatal(err)
	}

	count, err := r.LoadICloudPrivateRelayRanges(path)
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("count = %d, want 2", count)
	}
	if got := len(r.ICloudPrivateRelayRanges()); got != 2 {
		t.Fatalf("iCloud range count = %d, want 2", got)
	}

	for _, ip := range []string{"192.0.2.42", "2001:db8:abcd::1234"} {
		var record OpenproxyDBRecord
		if !r.LookupTo(net.ParseIP(ip), &record) {
			t.Fatalf("expected lookup for %s to match iCloud Private Relay range", ip)
		}
		if !record.IsProxy || !record.IsVPN || !record.IsAnonymous {
			t.Fatalf("record for %s = %+v, want proxy, VPN, and anonymous", ip, record)
		}
	}
}

func TestCIDRRangesExcludeSupplementaryICloudRanges(t *testing.T) {
	path := writeTempFile(t, "icloud-*.csv", "192.0.2.0/24,US,US-CA,Cupertino,\n")
	r := &OpenproxyDBReader{
		singleIPs:   make(map[netip.Addr]OpenproxyDBRecord),
		cidrRanges:  make([]cidrEntry, 0),
		cidrRecords: make(map[netip.Prefix]OpenproxyDBRecord),
	}
	r.addCIDRRange(netip.MustParsePrefix("198.51.100.0/24"), OpenproxyDBRecord{IsHosting: true})

	if _, err := r.LoadICloudPrivateRelayRanges(path); err != nil {
		t.Fatal(err)
	}

	ranges := r.CIDRRanges()
	if len(ranges) != 1 {
		t.Fatalf("CIDRRanges length = %d, want only original OpenProxyDB range", len(ranges))
	}
	if ranges[0].Prefix != netip.MustParsePrefix("198.51.100.0/24") {
		t.Fatalf("CIDRRanges[0] = %s, want original range", ranges[0].Prefix)
	}
	if got := len(r.ICloudPrivateRelayRanges()); got != 1 {
		t.Fatalf("iCloud range count = %d, want 1", got)
	}
}

func TestLoadVPNProviderCIDRRangesMarksVPNAndHosting(t *testing.T) {
	path := writeTempFile(t, "vpn-provider-*.txt", "# comment\n198.51.100.1/24 2001:db8:abcd::/48 # trailing comment\n::ffff:203.0.113.9\n")
	r := &OpenproxyDBReader{
		singleIPs:   make(map[netip.Addr]OpenproxyDBRecord),
		cidrRanges:  make([]cidrEntry, 0),
		cidrRecords: make(map[netip.Prefix]OpenproxyDBRecord),
	}

	count, err := r.LoadVPNProviderCIDRRanges(path)
	if err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Fatalf("count = %d, want 3", count)
	}
	if got := len(r.VPNProviderRanges()); got != 3 {
		t.Fatalf("VPN provider range count = %d, want 3", got)
	}

	for _, ip := range []string{"198.51.100.42", "2001:db8:abcd::1234", "203.0.113.9"} {
		var record OpenproxyDBRecord
		if !r.LookupTo(net.ParseIP(ip), &record) {
			t.Fatalf("expected lookup for %s to match VPN provider range", ip)
		}
		if record.IsProxy || !record.IsVPN || !record.IsHosting || !record.IsAnonymous {
			t.Fatalf("record for %s = %+v, want VPN, hosting, and anonymous only", ip, record)
		}
	}
}

func TestLoadVPNProviderCIDRRangesWithRecordMarksVPNOnly(t *testing.T) {
	path := writeTempFile(t, "vpn-provider-*.txt", "198.51.100.42\n2001:db8:abcd::/48\n")
	r := &OpenproxyDBReader{
		singleIPs:   make(map[netip.Addr]OpenproxyDBRecord),
		cidrRanges:  make([]cidrEntry, 0),
		cidrRecords: make(map[netip.Prefix]OpenproxyDBRecord),
	}

	count, err := r.LoadVPNProviderCIDRRangesWithRecord(OpenproxyDBRecord{IsVPN: true}, path)
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("count = %d, want 2", count)
	}

	ranges := r.VPNProviderRanges()
	if got := len(ranges); got != 2 {
		t.Fatalf("VPN provider range count = %d, want 2", got)
	}
	for _, cidrRange := range ranges {
		if !cidrRange.Record.IsVPN || cidrRange.Record.IsHosting || !cidrRange.Record.IsAnonymous {
			t.Fatalf("VPNProviderRanges record = %+v, want VPN and anonymous only", cidrRange.Record)
		}
	}

	for _, ip := range []string{"198.51.100.42", "2001:db8:abcd::1234"} {
		var record OpenproxyDBRecord
		if !r.LookupTo(net.ParseIP(ip), &record) {
			t.Fatalf("expected lookup for %s to match VPN provider range", ip)
		}
		if record.IsProxy || !record.IsVPN || record.IsHosting || !record.IsAnonymous {
			t.Fatalf("record for %s = %+v, want VPN and anonymous only", ip, record)
		}
	}
}

func TestCIDRRangesExcludeSupplementaryVPNProviderRanges(t *testing.T) {
	path := writeTempFile(t, "vpn-provider-*.txt", "192.0.2.0/24\n")
	r := &OpenproxyDBReader{
		singleIPs:   make(map[netip.Addr]OpenproxyDBRecord),
		cidrRanges:  make([]cidrEntry, 0),
		cidrRecords: make(map[netip.Prefix]OpenproxyDBRecord),
	}
	r.addCIDRRange(netip.MustParsePrefix("198.51.100.0/24"), OpenproxyDBRecord{IsHosting: true})

	if _, err := r.LoadVPNProviderCIDRRanges(path); err != nil {
		t.Fatal(err)
	}

	ranges := r.CIDRRanges()
	if len(ranges) != 1 {
		t.Fatalf("CIDRRanges length = %d, want only original OpenProxyDB range", len(ranges))
	}
	if ranges[0].Prefix != netip.MustParsePrefix("198.51.100.0/24") {
		t.Fatalf("CIDRRanges[0] = %s, want original range", ranges[0].Prefix)
	}
	if got := len(r.VPNProviderRanges()); got != 1 {
		t.Fatalf("VPN provider range count = %d, want 1", got)
	}
}

func TestLoadAnycastPrefixesExposesNormalizedPrefixes(t *testing.T) {
	path := writeTempFile(t, "anycast-*.txt", "::ffff:203.0.113.0/120\n2001:db8::/32\n")
	r := &OpenproxyDBReader{
		singleIPs: make(map[netip.Addr]OpenproxyDBRecord),
	}

	count, err := r.LoadAnycastPrefixes(path)
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("count = %d, want 2", count)
	}

	prefixes := r.AnycastPrefixes()
	want := map[netip.Prefix]bool{
		netip.MustParsePrefix("203.0.113.0/24"): true,
		netip.MustParsePrefix("2001:db8::/32"):  true,
	}
	for _, prefix := range prefixes {
		delete(want, prefix)
	}
	if len(want) != 0 {
		t.Fatalf("missing normalized anycast prefixes: %#v (got %#v)", want, prefixes)
	}
}

func writeTempFile(t *testing.T, pattern, content string) string {
	t.Helper()
	file, err := os.CreateTemp(t.TempDir(), pattern)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(content); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	return file.Name()
}

func TestOpenproxyDBParseIgnoresTorColumn(t *testing.T) {
	path := writeTempFile(t, "proxy-*.csv", "ip,anonblock,proxy,vpn,cdn,rangeblock,school-block,tor,webhost\n203.0.113.8,false,false,false,false,false,false,true,false\n203.0.113.9,false,false,false,false,false,false,true,true\n")

	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()

	r := &OpenproxyDBReader{singleIPs: make(map[netip.Addr]OpenproxyDBRecord)}
	if err := r.parse(file); err != nil {
		t.Fatal(err)
	}

	if _, ok := r.singleIPs[netip.MustParseAddr("203.0.113.8")]; ok {
		t.Fatal("tor-only row should be skipped")
	}
	record := r.singleIPs[netip.MustParseAddr("203.0.113.9")]
	if record.IsTor || record.IsAnonymous || !record.IsHosting {
		t.Fatalf("record = %+v, want hosting only", record)
	}
}

func TestLoadTorRelaysMarksAnonymous(t *testing.T) {
	path := writeTempFile(t, "tor-*.json", `{"version":"8.0","relays":[{"or_addresses":["203.0.113.8:9001","[2001:db8::1]:9001"],"exit_addresses":["198.51.100.7"]}]}`)

	r := &OpenproxyDBReader{singleIPs: map[netip.Addr]OpenproxyDBRecord{
		netip.MustParseAddr("203.0.113.8"): {IsHosting: true},
	}}
	count, err := r.LoadTorRelays(path)
	if err != nil {
		t.Fatal(err)
	}
	if count != 3 {
		t.Fatalf("count = %d, want 3", count)
	}

	for _, ip := range []string{"203.0.113.8", "2001:db8::1", "198.51.100.7"} {
		record := r.singleIPs[netip.MustParseAddr(ip)]
		if !record.IsTor || !record.IsAnonymous {
			t.Fatalf("%s record = %+v, want tor and anonymous", ip, record)
		}
	}
	if !r.singleIPs[netip.MustParseAddr("203.0.113.8")].IsHosting {
		t.Fatal("existing hosting flag should be kept")
	}
}
