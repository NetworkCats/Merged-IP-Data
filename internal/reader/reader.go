package reader

import (
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

// NetworksWithin returns an iterator over networks within the specified network
func (r *Reader) NetworksWithin(network *net.IPNet) *maxminddb.Networks {
	return r.db.NetworksWithin(network, maxminddb.SkipAliasedNetworks)
}

// Metadata returns the database metadata
func (r *Reader) Metadata() maxminddb.Metadata {
	return r.db.Metadata
}
