package reader

import (
	"bufio"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

// ManuallyAddedBadASNs are ASNs treated as bad/hosting beyond those in the
// upstream bad-asn-list. AS174 (Cogent) is a major transit/hosting provider
// that is absent from the upstream list but behaves as hosting for the
// proxy-detection fallback.
var ManuallyAddedBadASNs = []uint32{174}

// BadASNReader holds the set of ASNs flagged as bad/hosting. IPs whose ASN
// lookup resolves to an entry in this set are treated as hosting/proxy when
// OpenProxyDB does not already mark them as a proxy.
type BadASNReader struct {
	asns map[uint32]struct{}
}

// OpenBadASNList opens and parses the bad-asn-list CSV file at path. The file
// is expected to have a header row identifying an "ASN" column; if no such
// header exists the first column is used. Additional ASNs from
// ManuallyAddedBadASNs are merged into the set regardless of what the file
// contains.
func OpenBadASNList(path string) (*BadASNReader, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open bad ASN list: %w", err)
	}
	defer file.Close()

	r := &BadASNReader{
		asns: make(map[uint32]struct{}),
	}

	buffered := bufio.NewReaderSize(file, 64*1024)
	csvReader := csv.NewReader(buffered)
	csvReader.FieldsPerRecord = -1
	csvReader.Comment = '#'
	csvReader.ReuseRecord = true

	header, err := csvReader.Read()
	if err != nil {
		if errors.Is(err, io.EOF) {
			r.addManual()
			return r, nil
		}
		return nil, fmt.Errorf("failed to read bad ASN list header: %w", err)
	}

	asnCol := findASNColumn(header)

	// If the first row doesn't look like a header (first field is numeric),
	// treat it as a data row.
	if !looksLikeHeader(header) && asnCol < len(header) {
		if asn, ok := parseASNField(header[asnCol]); ok {
			r.asns[asn] = struct{}{}
		}
	}

	for {
		row, err := csvReader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			// Skip malformed lines but keep parsing.
			var parseErr *csv.ParseError
			if errors.As(err, &parseErr) {
				continue
			}
			return nil, fmt.Errorf("failed to read bad ASN list: %w", err)
		}
		if asnCol >= len(row) {
			continue
		}
		if asn, ok := parseASNField(row[asnCol]); ok {
			r.asns[asn] = struct{}{}
		}
	}

	r.addManual()
	return r, nil
}

func (r *BadASNReader) addManual() {
	for _, asn := range ManuallyAddedBadASNs {
		r.asns[asn] = struct{}{}
	}
}

// looksLikeHeader returns true when the row is likely a header — i.e. the
// first field isn't parseable as an ASN integer.
func looksLikeHeader(row []string) bool {
	if len(row) == 0 {
		return false
	}
	_, ok := parseASNField(row[0])
	return !ok
}

// findASNColumn returns the index of the column named "asn" (case-insensitive)
// in a header row; if no such column is found it returns 0 (first column).
func findASNColumn(header []string) int {
	for i, col := range header {
		if strings.EqualFold(strings.TrimSpace(col), "asn") {
			return i
		}
	}
	return 0
}

// parseASNField parses a raw CSV field into an ASN number, stripping an
// optional "AS" prefix and surrounding whitespace.
func parseASNField(s string) (uint32, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	if len(s) >= 2 {
		prefix := strings.ToUpper(s[:2])
		if prefix == "AS" {
			s = s[2:]
		}
	}
	asn, err := strconv.ParseUint(s, 10, 32)
	if err != nil {
		return 0, false
	}
	if asn == 0 {
		return 0, false
	}
	return uint32(asn), true
}

// Contains reports whether the given ASN is present in the bad ASN set.
// Returns false for a nil receiver or an unknown ASN (0).
func (r *BadASNReader) Contains(asn uint32) bool {
	if r == nil || asn == 0 {
		return false
	}
	_, ok := r.asns[asn]
	return ok
}

// Count returns the number of bad ASNs loaded, including manually added
// entries. Returns 0 for a nil receiver.
func (r *BadASNReader) Count() int {
	if r == nil {
		return 0
	}
	return len(r.asns)
}

// Close is a no-op; data is held entirely in memory.
func (r *BadASNReader) Close() error {
	return nil
}
