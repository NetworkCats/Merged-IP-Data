package reader

import (
	"math/big"
	"net"

	"github.com/oschwald/maxminddb-golang"
)

// Reader wraps a maxminddb.Reader with helper methods
type Reader struct {
	db   *maxminddb.Reader
	path string
}

// Open opens a MaxMind DB file for reading
func Open(path string) (*Reader, error) {
	db, err := maxminddb.Open(path)
	if err != nil {
		return nil, err
	}
	return &Reader{db: db, path: path}, nil
}

// Close closes the database reader
func (r *Reader) Close() error {
	return r.db.Close()
}

// Path returns the path to the database file
func (r *Reader) Path() string {
	return r.path
}

// Lookup looks up an IP address and decodes into the provided result
func (r *Reader) Lookup(ip net.IP, result interface{}) error {
	return r.db.Lookup(ip, result)
}

// LookupNetwork looks up an IP and returns the network and whether a record was found
func (r *Reader) LookupNetwork(ip net.IP, result interface{}) (*net.IPNet, bool, error) {
	return r.db.LookupNetwork(ip, result)
}

// Networks returns an iterator over all networks in the database
func (r *Reader) Networks() *maxminddb.Networks {
	return r.db.Networks(maxminddb.SkipAliasedNetworks)
}

// Decode decodes the record at a data section offset returned by NetworkOffset
func (r *Reader) Decode(offset uintptr, result interface{}) error {
	return r.db.Decode(offset, result)
}

// NetworkOffset returns the iterator's current network and the data section
// offset of its record, without decoding the record. Networks that share a
// record share an offset, so callers can decode and convert each distinct
// record once and reuse the result.
func NetworkOffset(networks *maxminddb.Networks) (*net.IPNet, uintptr, error) {
	var record recordOffset
	network, err := networks.Network(&record)
	return network, record.offset, err
}

// recordOffset is a maxminddb deserializer that records the offset of the
// value it is asked to decode and skips decoding it.
type recordOffset struct {
	offset uintptr
}

func (r *recordOffset) ShouldSkip(offset uintptr) (bool, error) {
	r.offset = offset
	return true, nil
}

func (*recordOffset) StartSlice(uint) error  { return nil }
func (*recordOffset) StartMap(uint) error    { return nil }
func (*recordOffset) End() error             { return nil }
func (*recordOffset) String(string) error    { return nil }
func (*recordOffset) Float64(float64) error  { return nil }
func (*recordOffset) Bytes([]byte) error     { return nil }
func (*recordOffset) Uint16(uint16) error    { return nil }
func (*recordOffset) Uint32(uint32) error    { return nil }
func (*recordOffset) Int32(int32) error      { return nil }
func (*recordOffset) Uint64(uint64) error    { return nil }
func (*recordOffset) Uint128(*big.Int) error { return nil }
func (*recordOffset) Bool(bool) error        { return nil }
func (*recordOffset) Float32(float32) error  { return nil }

// NetworksWithin returns an iterator over networks within the specified network
func (r *Reader) NetworksWithin(network *net.IPNet) *maxminddb.Networks {
	return r.db.NetworksWithin(network, maxminddb.SkipAliasedNetworks)
}

// Metadata returns the database metadata
func (r *Reader) Metadata() maxminddb.Metadata {
	return r.db.Metadata
}
