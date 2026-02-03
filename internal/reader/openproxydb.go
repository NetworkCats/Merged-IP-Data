package reader

import (
	"bufio"
	"encoding/csv"
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

// cidrEntry holds a CIDR prefix and its associated proxy record
type cidrEntry struct {
	prefix netip.Prefix
	record OpenproxyDBRecord
}

// OpenproxyDBReader reads and queries the OpenProxyDB CSV database.
// Uses optimized data structures for fast lookups:
// - Hash map for single IP addresses: O(1) lookup
// - IPSet for fast CIDR containment check: O(log n)
// - Sorted slice with binary search for CIDR record retrieval: O(log n)
type OpenproxyDBReader struct {
	singleIPs map[netip.Addr]OpenproxyDBRecord

	// cidrSet provides fast O(log n) containment check
	cidrSet *netipx.IPSet

	// cidrRanges stores records sorted by prefix for binary search lookup
	// after confirming containment via cidrSet
	cidrRanges []cidrEntry
}

// OpenOpenproxyDB opens and parses the OpenProxyDB CSV file
func OpenOpenproxyDB() (*OpenproxyDBReader, error) {
	file, err := os.Open(config.OpenproxyDBFile)
	if err != nil {
		return nil, fmt.Errorf("failed to open OpenProxyDB file: %w", err)
	}
	defer file.Close()

	reader := &OpenproxyDBReader{
		singleIPs:  make(map[netip.Addr]OpenproxyDBRecord),
		cidrRanges: make([]cidrEntry, 0),
	}

	if err := reader.parse(file); err != nil {
		return nil, fmt.Errorf("failed to parse OpenProxyDB: %w", err)
	}

	// Sort CIDR ranges by prefix for binary search lookup
	// Sort by address first, then by prefix length (more specific first)
	sort.Slice(reader.cidrRanges, func(i, j int) bool {
		pi, pj := reader.cidrRanges[i].prefix, reader.cidrRanges[j].prefix
		addrCmp := pi.Addr().Compare(pj.Addr())
		if addrCmp != 0 {
			return addrCmp < 0
		}
		// More specific (larger prefix length) comes first
		return pi.Bits() > pj.Bits()
	})

	// Build IPSet for fast O(log n) containment checks
	if len(reader.cidrRanges) > 0 {
		var builder netipx.IPSetBuilder
		for i := range reader.cidrRanges {
			builder.AddPrefix(reader.cidrRanges[i].prefix)
		}
		ipSet, err := builder.IPSet()
		if err != nil {
			return nil, fmt.Errorf("failed to build IPSet: %w", err)
		}
		reader.cidrSet = ipSet
	}

	return reader, nil
}

// parse reads the CSV file and populates the data structures
func (r *OpenproxyDBReader) parse(file *os.File) error {
	bufferedReader := bufio.NewReaderSize(file, 256*1024)
	csvReader := csv.NewReader(bufferedReader)
	csvReader.FieldsPerRecord = 10
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

	// Verify required columns exist
	requiredCols := []string{"ip", "anonblock", "proxy", "vpn", "cdn", "rangeblock", "school-block", "tor", "webhost"}
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
	torIdx := colIndex["tor"]
	webhostIdx := colIndex["webhost"]

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
		tor := parseBool(row[torIdx])
		webhost := parseBool(row[webhostIdx])

		// Build the record with computed fields
		isProxy := anonblock || proxy || rangeblock
		record := OpenproxyDBRecord{
			IsProxy:     isProxy,
			IsVPN:       vpn,
			IsTor:       tor,
			IsHosting:   webhost,
			IsCDN:       cdn,
			IsSchool:    school,
			IsAnonymous: isProxy || vpn || tor,
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
			r.cidrRanges = append(r.cidrRanges, cidrEntry{
				prefix: prefix,
				record: record,
			})
		} else {
			addr, err := netip.ParseAddr(ipStr)
			if err != nil {
				continue
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
	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}
	addr = addr.Unmap()

	// Priority 1: Check single IP map first (single IPs take priority)
	if rec, found := r.singleIPs[addr]; found {
		*record = rec
		return true
	}

	// Priority 2: Search CIDR ranges (find most specific match)
	if rec, found := r.findInCIDR(addr); found {
		*record = rec
		return true
	}

	return false
}

// findInCIDR searches for the most specific CIDR match for the given address.
// Uses binary search for O(log n) lookup performance.
func (r *OpenproxyDBReader) findInCIDR(addr netip.Addr) (OpenproxyDBRecord, bool) {
	if len(r.cidrRanges) == 0 {
		return OpenproxyDBRecord{}, false
	}

	// Fast path: use IPSet for quick containment check
	if r.cidrSet != nil && !r.cidrSet.Contains(addr) {
		return OpenproxyDBRecord{}, false
	}

	// Binary search to find a potential match region
	// Find the rightmost prefix whose start address <= addr
	idx := sort.Search(len(r.cidrRanges), func(i int) bool {
		return r.cidrRanges[i].prefix.Addr().Compare(addr) > 0
	})

	// Search backwards from idx to find matching prefixes
	// We need to find the most specific (highest bits) match
	var bestMatch *cidrEntry
	bestBits := -1

	// Check entries before idx (they have start addr <= our addr)
	for i := idx - 1; i >= 0; i-- {
		entry := &r.cidrRanges[i]

		// If this prefix's end is before our address, earlier entries won't match either
		// (for same prefix length). But we need to check all potential matches.
		if entry.prefix.Contains(addr) {
			if entry.prefix.Bits() > bestBits {
				bestMatch = entry
				bestBits = entry.prefix.Bits()
			}
		}

		// Optimization: if we've moved past addresses that could possibly contain addr,
		// and we have a match, we can stop. This happens when the entry's masked network
		// is completely before our address.
		if bestMatch != nil {
			// If we found a match and this entry's network end is before addr's network start
			// at the same prefix length, we can stop searching
			entryEnd := lastAddrInPrefix(entry.prefix)
			if entryEnd.Compare(addr) < 0 {
				break
			}
		}
	}

	if bestMatch != nil {
		return bestMatch.record, true
	}
	return OpenproxyDBRecord{}, false
}

// lastAddrInPrefix returns the last address in a prefix
func lastAddrInPrefix(p netip.Prefix) netip.Addr {
	addr := p.Addr()
	if addr.Is4() {
		bits := p.Bits()
		if bits == 32 {
			return addr
		}
		a4 := addr.As4()
		hostBits := 32 - bits
		mask := uint32((1 << hostBits) - 1)
		val := uint32(a4[0])<<24 | uint32(a4[1])<<16 | uint32(a4[2])<<8 | uint32(a4[3])
		val |= mask
		return netip.AddrFrom4([4]byte{byte(val >> 24), byte(val >> 16), byte(val >> 8), byte(val)})
	}
	// IPv6
	bits := p.Bits()
	if bits == 128 {
		return addr
	}
	a16 := addr.As16()
	hostBits := 128 - bits
	// Set all host bits to 1
	for i := 15; i >= 0 && hostBits > 0; i-- {
		if hostBits >= 8 {
			a16[i] = 0xFF
			hostBits -= 8
		} else {
			a16[i] |= byte((1 << hostBits) - 1)
			hostBits = 0
		}
	}
	return netip.AddrFrom16(a16)
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

// Stats returns the count of single IPs and CIDR ranges loaded
func (r *OpenproxyDBReader) Stats() (singleCount, cidrCount int) {
	return len(r.singleIPs), len(r.cidrRanges)
}
