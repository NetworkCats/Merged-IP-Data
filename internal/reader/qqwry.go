package reader

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"

	"merged-ip-data/internal/config"
)

// QQWry fields as named in the IPDB metadata.
var qqwryFieldNames = []string{
	"country_name",
	"region_name",
	"city_name",
	"district_name",
	"owner_domain",
	"isp_domain",
	"country_code",
	"continent_code",
}

// QQWryRecord represents a record from the QQWry (Chunzhen) database.
// All names are Chinese; the database has no other language.
type QQWryRecord struct {
	CountryName   string // Country name in Chinese
	RegionName    string // Province/Region name in Chinese
	CityName      string // City name in Chinese
	DistrictName  string // District name (if available)
	OwnerDomain   string // Network owner or usage note (e.g. IDC机房)
	ISPDomain     string // ISP name (e.g. 电信, 联通, 移动)
	CountryCode   string // ISO 3166-1 country code
	ContinentCode string // Continent code
}

// QQWryReader reads the QQWry IPDB database. The whole file is held in memory
// so that every network can be walked, which the ipdb-go library does not expose.
type QQWryReader struct {
	data       []byte // node and data sections, after the metadata header
	nodeCount  int
	v4Node     int
	ipVersion  int
	fieldIndex []int // position of each qqwryFieldNames entry in a record, or -1
	fieldCount int
	langOffset int
}

type qqwryMeta struct {
	IPVersion int            `json:"ip_version"`
	Languages map[string]int `json:"languages"`
	NodeCount int            `json:"node_count"`
	TotalSize int            `json:"total_size"`
	Fields    []string       `json:"fields"`
}

// OpenQQWry opens the QQWry IPDB database
func OpenQQWry() (*QQWryReader, error) {
	return OpenQQWryFile(config.QQWryFile)
}

// OpenQQWryFile opens a QQWry IPDB database at the given path
func OpenQQWryFile(path string) (*QQWryReader, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return newQQWryReader(raw)
}

func newQQWryReader(raw []byte) (*QQWryReader, error) {
	if len(raw) < 4 {
		return nil, errors.New("qqwry: file too short")
	}
	metaLen := int(binary.BigEndian.Uint32(raw[:4]))
	if metaLen <= 0 || 4+metaLen > len(raw) {
		return nil, errors.New("qqwry: invalid metadata length")
	}
	var meta qqwryMeta
	if err := json.Unmarshal(raw[4:4+metaLen], &meta); err != nil {
		return nil, fmt.Errorf("qqwry: invalid metadata: %w", err)
	}
	data := raw[4+metaLen:]
	if meta.NodeCount <= 0 || len(data) != meta.TotalSize || meta.NodeCount*8 > len(data) {
		return nil, errors.New("qqwry: database size mismatch")
	}
	if len(meta.Fields) == 0 {
		return nil, errors.New("qqwry: no fields in metadata")
	}
	langOffset, ok := meta.Languages["CN"]
	if !ok {
		return nil, errors.New("qqwry: CN language not found")
	}

	r := &QQWryReader{
		data:       data,
		nodeCount:  meta.NodeCount,
		ipVersion:  meta.IPVersion,
		fieldIndex: make([]int, len(qqwryFieldNames)),
		fieldCount: len(meta.Fields),
		langOffset: langOffset,
	}
	for i, name := range qqwryFieldNames {
		r.fieldIndex[i] = -1
		for j, field := range meta.Fields {
			if field == name {
				r.fieldIndex[i] = j
				break
			}
		}
	}

	// IPv4 addresses live under ::ffff:0:0/96 (80 zero bits, then 16 one bits).
	node := 0
	for i := 0; i < 96 && node < r.nodeCount; i++ {
		bit := 0
		if i >= 80 {
			bit = 1
		}
		node = r.readNode(node, bit)
	}
	r.v4Node = node
	return r, nil
}

// Close releases the database (the data is held in memory)
func (r *QQWryReader) Close() error {
	r.data = nil
	return nil
}

// IsIPv4Supported returns whether the database supports IPv4
func (r *QQWryReader) IsIPv4Supported() bool {
	return r.ipVersion&0x01 == 0x01
}

// IsIPv6Supported returns whether the database supports IPv6
func (r *QQWryReader) IsIPv6Supported() bool {
	return r.ipVersion&0x02 == 0x02
}

func (r *QQWryReader) readNode(node, bit int) int {
	off := node*8 + bit*4
	return int(binary.BigEndian.Uint32(r.data[off : off+4]))
}

// Lookup looks up an IP address in the QQWry database
func (r *QQWryReader) Lookup(ip net.IP) (*QQWryRecord, error) {
	var record QQWryRecord
	if err := r.LookupTo(ip, &record); err != nil {
		return nil, err
	}
	return &record, nil
}

// LookupTo looks up an IP address into a pre-allocated record to reduce allocations
func (r *QQWryReader) LookupTo(ip net.IP, record *QQWryRecord) error {
	node, bits := 0, 128
	if ip4 := ip.To4(); ip4 != nil {
		if !r.IsIPv4Supported() {
			return errors.New("qqwry: IPv4 not supported")
		}
		ip, node, bits = ip4, r.v4Node, 32
	} else if ip = ip.To16(); ip == nil || !r.IsIPv6Supported() {
		return errors.New("qqwry: unsupported address")
	}

	for i := 0; i < bits && node < r.nodeCount; i++ {
		node = r.readNode(node, int(ip[i>>3]>>(7-uint(i%8)))&1)
	}
	if node <= r.nodeCount {
		return errors.New("qqwry: address not found")
	}
	return r.decode(node, record)
}

// Networks walks every network in the database that has data and calls fn for
// each one. The record passed to fn is reused between calls.
func (r *QQWryReader) Networks(fn func(network *net.IPNet, record *QQWryRecord) error) error {
	var record QQWryRecord
	if r.IsIPv4Supported() {
		if err := r.walk(r.v4Node, 32, -1, &record, fn); err != nil {
			return err
		}
	}
	if r.IsIPv6Supported() {
		// Skip the IPv4 subtree, which was walked above in its 32-bit form.
		return r.walk(0, 128, r.v4Node, &record, fn)
	}
	return nil
}

func (r *QQWryReader) walk(root, bits, skip int, record *QQWryRecord, fn func(*net.IPNet, *QQWryRecord) error) error {
	type frame struct {
		node  int
		depth int
		ip    [16]byte
	}
	stack := []frame{{node: root}}
	for len(stack) > 0 {
		f := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

		if f.node > r.nodeCount {
			if err := r.decode(f.node, record); err != nil {
				return err
			}
			ip := make(net.IP, bits/8)
			copy(ip, f.ip[:bits/8])
			if err := fn(&net.IPNet{IP: ip, Mask: net.CIDRMask(f.depth, bits)}, record); err != nil {
				return err
			}
			continue
		}
		if f.node == r.nodeCount || f.node == skip || f.depth >= bits {
			continue
		}

		right := f
		right.node = r.readNode(f.node, 1)
		right.depth = f.depth + 1
		right.ip[f.depth>>3] |= 0x80 >> uint(f.depth%8)
		left := f
		left.node = r.readNode(f.node, 0)
		left.depth = f.depth + 1
		// Push right first so networks come out in ascending order.
		stack = append(stack, right, left)
	}
	return nil
}

func (r *QQWryReader) decode(node int, record *QQWryRecord) error {
	off := node - r.nodeCount + r.nodeCount*8
	if off+2 > len(r.data) {
		return errors.New("qqwry: invalid data offset")
	}
	size := int(binary.BigEndian.Uint16(r.data[off : off+2]))
	if off+2+size > len(r.data) {
		return errors.New("qqwry: invalid data size")
	}
	values := strings.Split(string(r.data[off+2:off+2+size]), "\t")
	if len(values) < r.langOffset+r.fieldCount {
		return errors.New("qqwry: record has too few fields")
	}
	values = values[r.langOffset : r.langOffset+r.fieldCount]

	field := func(i int) string {
		if j := r.fieldIndex[i]; j >= 0 {
			return values[j]
		}
		return ""
	}
	record.CountryName = field(0)
	record.RegionName = field(1)
	record.CityName = field(2)
	record.DistrictName = field(3)
	record.OwnerDomain = field(4)
	record.ISPDomain = field(5)
	record.CountryCode = field(6)
	record.ContinentCode = field(7)
	return nil
}

// HasGeoData checks if the record has geographic data
func (r *QQWryRecord) HasGeoData() bool {
	return r.CountryName != "" || r.RegionName != "" || r.CityName != ""
}

// HasCityData checks if the record has city-level data
func (r *QQWryRecord) HasCityData() bool {
	return r.CityName != ""
}

// HasRegionData checks if the record has region/province data
func (r *QQWryRecord) HasRegionData() bool {
	return r.RegionName != ""
}

// IsChina checks if the record is for mainland China. QQWry also names Hong
// Kong, Macao and Taiwan "中国", so only the country code is trusted here.
func (r *QQWryRecord) IsChina() bool {
	return r.CountryCode == "CN"
}

// Reset clears all fields for reuse, reducing allocations
func (r *QQWryRecord) Reset() {
	*r = QQWryRecord{}
}
