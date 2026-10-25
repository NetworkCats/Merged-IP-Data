package reader

import (
	"iter"
	"net/netip"

	"github.com/oschwald/maxminddb-golang/v2"
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
func (r *Reader) Lookup(ip netip.Addr, result any) error {
	return r.db.Lookup(ip).Decode(result)
}

// LookupNetwork looks up an IP and returns the network and whether a record was found
func (r *Reader) LookupNetwork(ip netip.Addr, result any) (netip.Prefix, bool, error) {
	res := r.db.Lookup(ip)
	if err := res.Err(); err != nil {
		return netip.Prefix{}, false, err
	}
	if !res.Found() {
		return res.Prefix(), false, nil
	}
	return res.Prefix(), true, res.Decode(result)
}

// Networks returns an iterator over all networks in the database. IPv4
// networks are yielded once, as IPv4 prefixes, rather than at every alias in
// an IPv6 database. Networks that share a record share its Offset, so callers
// can decode and convert each distinct record once with Decode.
func (r *Reader) Networks() iter.Seq[maxminddb.Result] {
	return r.db.Networks()
}

// Decode decodes the record at a data section offset from a Networks result
func (r *Reader) Decode(offset uintptr, result any) error {
	return r.db.LookupOffset(offset).Decode(result)
}

// NetworksWithin returns an iterator over networks within the specified network
func (r *Reader) NetworksWithin(prefix netip.Prefix) iter.Seq[maxminddb.Result] {
	return r.db.NetworksWithin(prefix)
}

// Metadata returns the database metadata
func (r *Reader) Metadata() maxminddb.Metadata {
	return r.db.Metadata
}
