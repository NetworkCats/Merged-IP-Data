package reader

import (
	"bufio"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/netip"
	"os"
	"sort"
	"strings"

	"merged-ip-data/internal/config"

	"go4.org/netipx"
)

// OpenproxyDBRecord represents proxy/anonymity flags for an IP address
type OpenproxyDBRecord struct {
	IsProxy     bool // anonblock OR proxy OR rangeblock
	IsVPN       bool
	IsTor       bool
	IsHosting   bool // webhost
	IsCDN       bool
	IsSchool    bool // school-block
	IsAnonymous bool // computed: IsProxy OR IsVPN OR IsTor
}

type cidrRangeSource uint8

const (
	cidrRangeSourceOpenproxy cidrRangeSource = iota
	cidrRangeSourceICloud
	cidrRangeSourceVPNProvider
)

// cidrEntry holds a CIDR prefix and its associated proxy record
type cidrEntry struct {
	prefix netip.Prefix
	record OpenproxyDBRecord
	source cidrRangeSource
}

// CIDRRange is an exported snapshot of a CIDR overlay range and its proxy
// flags. It is returned by accessors so callers cannot mutate reader internals.
type CIDRRange struct {
	Prefix netip.Prefix
	Record OpenproxyDBRecord
}

// OpenproxyDBReader reads and queries the OpenProxyDB CSV database.
// Uses optimized data structures for fast lookups:
// - Hash map for single IP addresses: O(1) lookup
// - IPSet for fast CIDR containment check: O(log n)
// - Prefix map for bounded CIDR record retrieval: O(address bit length)
type OpenproxyDBReader struct {
	singleIPs map[netip.Addr]OpenproxyDBRecord

	// cidrSet provides fast O(log n) containment check
	cidrSet *netipx.IPSet

	// cidrRanges stores all CIDR entries for deterministic stats and IPSet
	// construction.
	cidrRanges []cidrEntry

	// cidrRecords maps canonical prefixes to records. It makes record retrieval
	// bounded by address bit length after cidrSet confirms a positive match.
	cidrRecords map[netip.Prefix]OpenproxyDBRecord

	// Track which prefix lengths actually occur so positive CIDR lookups do
	// not construct and probe every possible prefix length.
	cidrBits4 [33]bool
	cidrBits6 [129]bool

	// icloudPrivateRelayRanges stores the source CIDRs from Apple's iCloud
	// Private Relay egress list so the merger can overlay them exactly.
	icloudPrivateRelayRanges []netip.Prefix

	// vpnProviderRanges stores third-party VPN provider CIDR feeds that should
	// be overlaid exactly with their source-specific proxy flags.
	vpnProviderRanges []CIDRRange

	// anycastSet holds the union of bgp.tools anycast prefixes (v4 + v6).
	// Any IP contained in this set gets IsCDN=true OR'd onto its OpenProxyDB
	// record during lookup so the CDN tag coexists with any existing tags
	// (Hosting, Proxy, VPN, Tor, ...) rather than overriding them.
	anycastSet *netipx.IPSet
}

// OpenOpenproxyDB opens and parses the OpenProxyDB CSV file
func OpenOpenproxyDB() (*OpenproxyDBReader, error) {
	file, err := os.Open(config.OpenproxyDBFile)
	if err != nil {
		return nil, fmt.Errorf("failed to open OpenProxyDB file: %w", err)
	}
	defer file.Close()

	reader := &OpenproxyDBReader{
		singleIPs:   make(map[netip.Addr]OpenproxyDBRecord),
		cidrRanges:  make([]cidrEntry, 0),
		cidrRecords: make(map[netip.Prefix]OpenproxyDBRecord),
	}

	if err := reader.parse(file); err != nil {
		return nil, fmt.Errorf("failed to parse OpenProxyDB: %w", err)
	}

	if err := reader.rebuildCIDRSet(); err != nil {
		return nil, fmt.Errorf("failed to build IPSet: %w", err)
	}

	return reader, nil
}

// parse reads the CSV file and populates the data structures
func (r *OpenproxyDBReader) parse(file *os.File) error {
	bufferedReader := bufio.NewReaderSize(file, 256*1024)
	csvReader := csv.NewReader(bufferedReader)
	csvReader.FieldsPerRecord = -1
	csvReader.ReuseRecord = true

	// Read and validate header
	header, err := csvReader.Read()
	if err != nil {
		return fmt.Errorf("failed to read CSV header: %w", err)
	}

	colIndex := make(map[string]int)
	for i, col := range header {
		colIndex[strings.TrimSpace(col)] = i
	}

	// Verify required columns exist. The "tor" column is ignored: Tor flags
	// come only from the Onionoo relay list loaded by LoadTorRelays.
	requiredCols := []string{"ip", "anonblock", "proxy", "vpn", "cdn", "rangeblock", "school-block", "webhost"}
	for _, col := range requiredCols {
		if _, ok := colIndex[col]; !ok {
			return fmt.Errorf("missing required column: %s", col)
		}
	}

	ipIdx := colIndex["ip"]
	anonblockIdx := colIndex["anonblock"]
	proxyIdx := colIndex["proxy"]
	vpnIdx := colIndex["vpn"]
	cdnIdx := colIndex["cdn"]
	rangeblockIdx := colIndex["rangeblock"]
	schoolIdx := colIndex["school-block"]
	webhostIdx := colIndex["webhost"]
	maxRequiredIdx := ipIdx
	for _, idx := range []int{anonblockIdx, proxyIdx, vpnIdx, cdnIdx, rangeblockIdx, schoolIdx, webhostIdx} {
		if idx > maxRequiredIdx {
			maxRequiredIdx = idx
		}
	}

	lineNum := 1
	for {
		lineNum++
		row, err := csvReader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to read CSV line %d: %w", lineNum, err)
		}
		if len(row) == 1 && strings.TrimSpace(row[0]) == "" {
			continue
		}
		if len(row) <= maxRequiredIdx {
			return fmt.Errorf("CSV line %d has %d columns, need at least %d", lineNum, len(row), maxRequiredIdx+1)
		}

		ipStr := strings.TrimSpace(row[ipIdx])
		if ipStr == "" {
			continue
		}

		// Parse boolean flags
		anonblock := parseBool(row[anonblockIdx])
		proxy := parseBool(row[proxyIdx])
		vpn := parseBool(row[vpnIdx])
		cdn := parseBool(row[cdnIdx])
		rangeblock := parseBool(row[rangeblockIdx])
		school := parseBool(row[schoolIdx])
		webhost := parseBool(row[webhostIdx])

		// Build the record with computed fields
		isProxy := anonblock || proxy || rangeblock
		record := OpenproxyDBRecord{
			IsProxy:     isProxy,
			IsVPN:       vpn,
			IsHosting:   webhost,
			IsCDN:       cdn,
			IsSchool:    school,
			IsAnonymous: isProxy || vpn,
		}

		// Skip records with no flags set
		if !record.HasData() {
			continue
		}

		// Check if it's a CIDR range or single IP
		if strings.Contains(ipStr, "/") {
			prefix, err := netip.ParsePrefix(ipStr)
			if err != nil {
				continue
			}
			r.addCIDRRange(prefix, record)
		} else {
			addr, err := netip.ParseAddr(ipStr)
			if err != nil {
				continue
			}
			addr = addr.Unmap()
			if existing, ok := r.singleIPs[addr]; ok {
				record = mergeOpenproxyRecords(existing, record)
			}
			r.singleIPs[addr] = record
		}
	}

	return nil
}

// parseBool parses a boolean string (True/False) to bool
func parseBool(s string) bool {
	s = strings.TrimSpace(strings.ToLower(s))
	return s == "true" || s == "1"
}

func canonicalPrefix(prefix netip.Prefix) netip.Prefix {
	// Mask first. An address that happens to be IPv4-mapped does not imply a
	// prefix shorter than /96 is an IPv4 prefix; unmapping such a prefix would
	// incorrectly broaden it (for example, /80 to all IPv4 addresses).
	prefix = prefix.Masked()
	addr := prefix.Addr()
	bits := prefix.Bits()
	if addr.Is4In6() && bits >= 96 {
		addr = addr.Unmap()
		bits -= 96
	}
	return netip.PrefixFrom(addr, bits).Masked()
}

func (r *OpenproxyDBReader) addCIDRRange(prefix netip.Prefix, record OpenproxyDBRecord) {
	r.addCIDRRangeWithSource(prefix, record, cidrRangeSourceOpenproxy)
}

func (r *OpenproxyDBReader) addCIDRRangeWithSource(prefix netip.Prefix, record OpenproxyDBRecord, source cidrRangeSource) {
	prefix = canonicalPrefix(prefix)
	r.cidrRanges = append(r.cidrRanges, cidrEntry{
		prefix: prefix,
		record: record,
		source: source,
	})
	if r.cidrRecords == nil {
		r.cidrRecords = make(map[netip.Prefix]OpenproxyDBRecord)
	}
	if existing, ok := r.cidrRecords[prefix]; ok {
		record = mergeOpenproxyRecords(existing, record)
	}
	r.cidrRecords[prefix] = record
	if prefix.Addr().Is4() {
		r.cidrBits4[prefix.Bits()] = true
	} else {
		r.cidrBits6[prefix.Bits()] = true
	}
}

func (r *OpenproxyDBReader) rebuildCIDRSet() error {
	if len(r.cidrRanges) == 0 {
		r.cidrSet = nil
		return nil
	}

	var builder netipx.IPSetBuilder
	for i := range r.cidrRanges {
		builder.AddPrefix(r.cidrRanges[i].prefix)
	}
	ipSet, err := builder.IPSet()
	if err != nil {
		return err
	}
	r.cidrSet = ipSet
	return nil
}

func mergeOpenproxyRecords(a, b OpenproxyDBRecord) OpenproxyDBRecord {
	return OpenproxyDBRecord{
		IsProxy:     a.IsProxy || b.IsProxy,
		IsVPN:       a.IsVPN || b.IsVPN,
		IsTor:       a.IsTor || b.IsTor,
		IsHosting:   a.IsHosting || b.IsHosting,
		IsCDN:       a.IsCDN || b.IsCDN,
		IsSchool:    a.IsSchool || b.IsSchool,
		IsAnonymous: a.IsAnonymous || b.IsAnonymous || a.IsProxy || b.IsProxy || a.IsVPN || b.IsVPN || a.IsTor || b.IsTor,
	}
}

// Close closes the reader (no-op as data is in memory)
func (r *OpenproxyDBReader) Close() error {
	return nil
}

// Lookup looks up an IP address and returns the proxy record if found
func (r *OpenproxyDBReader) Lookup(ip net.IP) *OpenproxyDBRecord {
	var record OpenproxyDBRecord
	if r.LookupTo(ip, &record) {
		return &record
	}
	return nil
}

// LookupTo looks up an IP address into a pre-allocated record to reduce allocations.
// Returns true if a record was found.
func (r *OpenproxyDBReader) LookupTo(ip net.IP, record *OpenproxyDBRecord) bool {
	if record == nil {
		return false
	}
	record.Reset()

	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}
	addr = addr.Unmap()

	found := false

	// Priority 1: Check single IP map first (single IPs take priority)
	if rec, ok := r.singleIPs[addr]; ok {
		*record = rec
		found = true
	} else if rec, ok := r.findInCIDR(addr); ok {
		// Priority 2: Search CIDR ranges (find most specific match)
		*record = rec
		found = true
	}

	// Overlay: bgp.tools anycast prefixes always contribute IsCDN=true on
	// top of any existing OpenProxyDB tags. This lets CDN coexist with
	// Hosting/Proxy/VPN/Tor rather than being shadowed when a more-specific
	// OpenProxyDB CIDR match would otherwise omit the CDN flag.
	if r.anycastSet != nil && r.anycastSet.Contains(addr) {
		record.IsCDN = true
		found = true
	}

	return found
}

// findInCIDR searches for the most specific CIDR match for the given address.
// Negative lookups use cidrSet's O(log n) containment check. Positive lookups
// walk prefix lengths from most-specific to least-specific, which caps record
// retrieval at 33 map probes for IPv4 and 129 for IPv6.
func (r *OpenproxyDBReader) findInCIDR(addr netip.Addr) (OpenproxyDBRecord, bool) {
	if len(r.cidrRanges) == 0 {
		return OpenproxyDBRecord{}, false
	}

	// Fast path: use IPSet for quick containment check
	if r.cidrSet != nil && !r.cidrSet.Contains(addr) {
		return OpenproxyDBRecord{}, false
	}

	if addr.Is4() {
		for bits := 32; bits >= 0; bits-- {
			if !r.cidrBits4[bits] {
				continue
			}
			if record, ok := r.cidrRecords[netip.PrefixFrom(addr, bits).Masked()]; ok {
				return record, true
			}
		}
	} else {
		for bits := 128; bits >= 0; bits-- {
			if !r.cidrBits6[bits] {
				continue
			}
			if record, ok := r.cidrRecords[netip.PrefixFrom(addr, bits).Masked()]; ok {
				return record, true
			}
		}
	}
	return OpenproxyDBRecord{}, false
}

// HasData checks if the record has any proxy/anonymity flags set
func (r *OpenproxyDBRecord) HasData() bool {
	return r.IsProxy || r.IsVPN || r.IsTor || r.IsHosting || r.IsCDN || r.IsSchool
}

// Reset clears all fields for reuse
func (r *OpenproxyDBRecord) Reset() {
	r.IsProxy = false
	r.IsVPN = false
	r.IsTor = false
	r.IsHosting = false
	r.IsCDN = false
	r.IsSchool = false
	r.IsAnonymous = false
}

// LoadBadIPList reads a plain-text file of IPs (one per line) and merges them
// into the single IP lookup map with IsProxy=true and IsAnonymous=true.
// Existing OpenProxyDB records are preserved; only the proxy/anonymous flags
// are ensured set for IPs that already exist.
func (r *OpenproxyDBReader) LoadBadIPList(path string) (int, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, fmt.Errorf("failed to open bad IP list: %w", err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	count := 0

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		addr, err := netip.ParseAddr(line)
		if err != nil {
			continue
		}
		addr = addr.Unmap()

		if existing, found := r.singleIPs[addr]; found {
			// Merge: ensure proxy and anonymous flags are set
			existing.IsProxy = true
			existing.IsAnonymous = true
			r.singleIPs[addr] = existing
		} else {
			rec := OpenproxyDBRecord{
				IsProxy:     true,
				IsAnonymous: true,
			}
			// Inherit any CIDR-level flags covering this IP (e.g. Hosting)
			// so they coexist with the proxy flag on the /32 record.
			if cidr, ok := r.findInCIDR(addr); ok {
				rec.IsVPN = rec.IsVPN || cidr.IsVPN
				rec.IsTor = rec.IsTor || cidr.IsTor
				rec.IsHosting = rec.IsHosting || cidr.IsHosting
				rec.IsCDN = rec.IsCDN || cidr.IsCDN
				rec.IsSchool = rec.IsSchool || cidr.IsSchool
			}
			r.singleIPs[addr] = rec
		}
		count++
	}

	if err := scanner.Err(); err != nil {
		return count, fmt.Errorf("error reading bad IP list: %w", err)
	}

	return count, nil
}

// LoadTorRelays reads the Onionoo JSON file of running Tor relays and merges
// their IP addresses into the single IP lookup map with IsTor=true and
// IsAnonymous=true. The JSON is expected to have been fetched with the
// fields=or_addresses,exit_addresses parameter so that only address data is
// present, keeping the download size manageable.
//
// or_addresses entries are in "ip:port" format (IPv6 in brackets, e.g.
// "[2001:db8::1]:9001"), and exit_addresses entries are plain IP strings.
// The method uses a streaming JSON decoder to handle large responses
// efficiently without loading the entire array into memory at once.
func (r *OpenproxyDBReader) LoadTorRelays(path string) (int, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, fmt.Errorf("failed to open Tor relays file: %w", err)
	}
	defer file.Close()

	buffered := bufio.NewReaderSize(file, 256*1024)
	decoder := json.NewDecoder(buffered)

	// Read opening brace of top-level object
	if _, err := decoder.Token(); err != nil {
		return 0, fmt.Errorf("failed to read JSON start: %w", err)
	}

	uniqueIPs := make(map[netip.Addr]struct{})

	// Stream through top-level keys until we find "relays"
	for decoder.More() {
		tok, err := decoder.Token()
		if err != nil {
			return 0, fmt.Errorf("failed to read JSON token: %w", err)
		}

		key, ok := tok.(string)
		if !ok {
			continue
		}

		if key == "relays" {
			// Read opening bracket of relays array
			if _, err := decoder.Token(); err != nil {
				return 0, fmt.Errorf("failed to read relays array start: %w", err)
			}

			// Stream each relay object
			var relay struct {
				ORAddresses   []string `json:"or_addresses"`
				ExitAddresses []string `json:"exit_addresses"`
			}

			for decoder.More() {
				relay.ORAddresses = relay.ORAddresses[:0]
				relay.ExitAddresses = relay.ExitAddresses[:0]

				if err := decoder.Decode(&relay); err != nil {
					continue
				}

				// Parse or_addresses: format is "ip:port" or "[ipv6]:port"
				for _, orAddr := range relay.ORAddresses {
					ip := parseTorORAddress(orAddr)
					if ip.IsValid() {
						uniqueIPs[ip] = struct{}{}
					}
				}

				// Parse exit_addresses: plain IP strings
				for _, exitAddr := range relay.ExitAddresses {
					addr, err := netip.ParseAddr(strings.TrimSpace(exitAddr))
					if err == nil {
						uniqueIPs[addr.Unmap()] = struct{}{}
					}
				}
			}

			// Read closing bracket of relays array
			if _, err := decoder.Token(); err != nil {
				return 0, fmt.Errorf("failed to read relays array end: %w", err)
			}
		} else {
			// Skip values for keys we don't care about (bridges, version, etc.)
			// We need to consume the value so the decoder can advance
			var skip json.RawMessage
			if err := decoder.Decode(&skip); err != nil {
				return 0, fmt.Errorf("failed to skip JSON value for key %q: %w", key, err)
			}
		}
	}

	// Merge unique IPs into the single IP map
	count := 0
	for addr := range uniqueIPs {
		if existing, found := r.singleIPs[addr]; found {
			// Merge: ensure Tor and anonymous flags are set
			existing.IsTor = true
			existing.IsAnonymous = true
			r.singleIPs[addr] = existing
		} else {
			rec := OpenproxyDBRecord{
				IsTor:       true,
				IsAnonymous: true,
			}
			// Inherit any CIDR-level flags covering this IP (e.g. Hosting,
			// Proxy, VPN) so the Tor tag coexists with them on the /32 record
			// rather than overriding them.
			if cidr, ok := r.findInCIDR(addr); ok {
				rec.IsProxy = rec.IsProxy || cidr.IsProxy
				rec.IsVPN = rec.IsVPN || cidr.IsVPN
				rec.IsHosting = rec.IsHosting || cidr.IsHosting
				rec.IsCDN = rec.IsCDN || cidr.IsCDN
				rec.IsSchool = rec.IsSchool || cidr.IsSchool
			}
			r.singleIPs[addr] = rec
		}
		count++
	}

	return count, nil
}

// parseTorORAddress extracts the IP address from a Tor OR address string.
// Formats: "1.2.3.4:9001" for IPv4, "[2001:db8::1]:9001" for IPv6.
func parseTorORAddress(orAddr string) netip.Addr {
	orAddr = strings.TrimSpace(orAddr)
	if orAddr == "" {
		return netip.Addr{}
	}

	// IPv6: "[addr]:port"
	if strings.HasPrefix(orAddr, "[") {
		bracketEnd := strings.LastIndex(orAddr, "]")
		if bracketEnd < 0 {
			return netip.Addr{}
		}
		ipStr := orAddr[1:bracketEnd]
		addr, err := netip.ParseAddr(ipStr)
		if err != nil {
			return netip.Addr{}
		}
		return addr.Unmap()
	}

	// IPv4: "addr:port"
	lastColon := strings.LastIndex(orAddr, ":")
	if lastColon < 0 {
		// No port — try parsing as plain IP
		addr, err := netip.ParseAddr(orAddr)
		if err != nil {
			return netip.Addr{}
		}
		return addr.Unmap()
	}

	ipStr := orAddr[:lastColon]
	addr, err := netip.ParseAddr(ipStr)
	if err != nil {
		return netip.Addr{}
	}
	return addr.Unmap()
}

// SingleIPs returns the single IP map for direct iteration.
// This allows the merger to insert each single IP as a /32 or /128 network
// directly into the MMDB tree, ensuring complete proxy coverage.
func (r *OpenproxyDBReader) SingleIPs() map[netip.Addr]OpenproxyDBRecord {
	return r.singleIPs
}

// CIDRRanges returns the OpenProxyDB CIDR ranges loaded from the source CSV.
// Ranges from supplementary feeds, such as iCloud Private Relay, are exposed
// through their dedicated accessors.
func (r *OpenproxyDBReader) CIDRRanges() []CIDRRange {
	merged := make(map[netip.Prefix]OpenproxyDBRecord)
	for _, entry := range r.cidrRanges {
		if entry.source != cidrRangeSourceOpenproxy {
			continue
		}
		if existing, ok := merged[entry.prefix]; ok {
			merged[entry.prefix] = mergeOpenproxyRecords(existing, entry.record)
		} else {
			merged[entry.prefix] = entry.record
		}
	}

	ranges := make([]CIDRRange, 0, len(merged))
	for prefix, record := range merged {
		ranges = append(ranges, CIDRRange{
			Prefix: prefix,
			Record: record,
		})
	}
	sort.Slice(ranges, func(i, j int) bool {
		addrCmp := ranges[i].Prefix.Addr().Compare(ranges[j].Prefix.Addr())
		if addrCmp != 0 {
			return addrCmp < 0
		}
		return ranges[i].Prefix.Bits() > ranges[j].Prefix.Bits()
	})
	return ranges
}

// LoadAnycastPrefixes reads one or more plain-text CIDR prefix list files (as
// published by bgp.tools anycast-prefixes) and builds the anycast lookup set.
// Blank lines and '#'-prefixed comments are skipped. Bare IP addresses are
// treated as /32 or /128 prefixes. Call this after any LoadBadIPList or
// LoadTorRelays calls so the sweep at the end picks up every single-IP entry.
//
// After the set is built, every single-IP record that falls inside an anycast
// prefix has IsCDN OR'd true in place so the merger's direct /32 and /128
// insertion path carries the CDN flag alongside the existing tags. CIDR-level
// lookups pick up the CDN overlay automatically via LookupTo.
func (r *OpenproxyDBReader) LoadAnycastPrefixes(paths ...string) (int, error) {
	var builder netipx.IPSetBuilder
	loaded := 0

	for _, path := range paths {
		n, err := r.parseAnycastFile(path, &builder)
		if err != nil {
			return loaded, err
		}
		loaded += n
	}

	if loaded == 0 {
		return 0, nil
	}

	ipSet, err := builder.IPSet()
	if err != nil {
		return loaded, fmt.Errorf("failed to build anycast IPSet: %w", err)
	}
	r.anycastSet = ipSet

	for addr, rec := range r.singleIPs {
		if !rec.IsCDN && ipSet.Contains(addr) {
			rec.IsCDN = true
			r.singleIPs[addr] = rec
		}
	}

	return loaded, nil
}

// parseAnycastFile reads a single prefix-list file and adds each entry to the
// supplied builder. Returns the number of prefixes successfully added.
func (r *OpenproxyDBReader) parseAnycastFile(path string, builder *netipx.IPSetBuilder) (int, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, fmt.Errorf("failed to open anycast prefix file %s: %w", path, err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	count := 0
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		prefix, err := netip.ParsePrefix(line)
		if err != nil {
			addr, addrErr := netip.ParseAddr(line)
			if addrErr != nil {
				continue
			}
			addr = addr.Unmap()
			bits := 32
			if addr.Is6() {
				bits = 128
			}
			prefix = netip.PrefixFrom(addr, bits)
		}

		builder.AddPrefix(canonicalPrefix(prefix))
		count++
	}

	if err := scanner.Err(); err != nil {
		return count, fmt.Errorf("error reading anycast prefix file %s: %w", path, err)
	}

	return count, nil
}

// AnycastPrefixCount returns the number of anycast prefixes currently loaded.
// Returns 0 if no anycast set has been built.
func (r *OpenproxyDBReader) AnycastPrefixCount() int {
	if r.anycastSet == nil {
		return 0
	}
	return len(r.anycastSet.Prefixes())
}

// AnycastPrefixes returns a copy of the normalized anycast prefix set.
func (r *OpenproxyDBReader) AnycastPrefixes() []netip.Prefix {
	if r.anycastSet == nil {
		return nil
	}
	prefixes := r.anycastSet.Prefixes()
	return append([]netip.Prefix(nil), prefixes...)
}

// Stats returns the count of single IPs and CIDR ranges loaded
func (r *OpenproxyDBReader) Stats() (singleCount, cidrCount int) {
	return len(r.singleIPs), len(r.cidrRanges)
}
