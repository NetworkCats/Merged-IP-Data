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

// OpenproxyDBReader reads and queries the OpenProxyDB CSV database
type OpenproxyDBReader struct {
	singleIPs  map[netip.Addr]OpenproxyDBRecord
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

	// Sort CIDR ranges by prefix for consistent lookup behavior
	sort.Slice(reader.cidrRanges, func(i, j int) bool {
		pi, pj := reader.cidrRanges[i].prefix, reader.cidrRanges[j].prefix
		// Sort by address first, then by prefix length (more specific first)
		addrCmp := pi.Addr().Compare(pj.Addr())
		if addrCmp != 0 {
			return addrCmp < 0
		}
		// More specific (larger prefix length) comes first
		return pi.Bits() > pj.Bits()
	})

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

// findInCIDR searches for the most specific CIDR match for the given address
func (r *OpenproxyDBReader) findInCIDR(addr netip.Addr) (OpenproxyDBRecord, bool) {
	var bestMatch *cidrEntry
	bestBits := -1

	for i := range r.cidrRanges {
		entry := &r.cidrRanges[i]
		if entry.prefix.Contains(addr) {
			if entry.prefix.Bits() > bestBits {
				bestMatch = entry
				bestBits = entry.prefix.Bits()
			}
		}
	}

	if bestMatch != nil {
		return bestMatch.record, true
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

// Stats returns the count of single IPs and CIDR ranges loaded
func (r *OpenproxyDBReader) Stats() (singleCount, cidrCount int) {
	return len(r.singleIPs), len(r.cidrRanges)
}
