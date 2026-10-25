package merger

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math"
	"math/big"
	"slices"

	"github.com/maxmind/mmdbwriter/mmdbtype"
)

// valueEncoder writes an unambiguous binary encoding of an MMDB value: equal
// values encode identically and different values differently. Unlike
// mmdbwriter's default key generator it boxes no values and reuses its
// buffers, which matters because a key is generated for every tree insert.
type valueEncoder struct {
	buf  []byte
	keys [][]mmdbtype.String // sorted map keys, one scratch slice per depth
}

// Type tags. Each is followed by a fixed-size value or a length prefix, so
// no encoding is a prefix of another.
const (
	tagBool byte = iota + 1
	tagBytes
	tagFloat32
	tagFloat64
	tagInt32
	tagMap
	tagPointer
	tagSlice
	tagString
	tagUint16
	tagUint32
	tagUint64
	tagUint128
)

// sum returns the SHA-256 digest of v's encoding.
func (e *valueEncoder) sum(v mmdbtype.DataType) ([sha256.Size]byte, error) {
	e.buf = e.buf[:0]
	if err := e.encode(v, 0); err != nil {
		return [sha256.Size]byte{}, err
	}
	return sha256.Sum256(e.buf), nil
}

func (e *valueEncoder) encode(v mmdbtype.DataType, depth int) error {
	switch v := v.(type) {
	case mmdbtype.Map:
		e.buf = binary.AppendUvarint(append(e.buf, tagMap), uint64(len(v)))
		if depth == len(e.keys) {
			e.keys = append(e.keys, nil)
		}
		keys := e.keys[depth][:0]
		for k := range v {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		e.keys[depth] = keys
		for _, k := range keys {
			e.appendString(tagString, string(k))
			if err := e.encode(v[k], depth+1); err != nil {
				return err
			}
		}
	case mmdbtype.Slice:
		e.buf = binary.AppendUvarint(append(e.buf, tagSlice), uint64(len(v)))
		for _, elem := range v {
			if err := e.encode(elem, depth+1); err != nil {
				return err
			}
		}
	case mmdbtype.String:
		e.appendString(tagString, string(v))
	case mmdbtype.Bytes:
		e.appendString(tagBytes, string(v))
	case mmdbtype.Bool:
		b := byte(0)
		if v {
			b = 1
		}
		e.buf = append(e.buf, tagBool, b)
	case mmdbtype.Uint16:
		e.buf = binary.BigEndian.AppendUint16(append(e.buf, tagUint16), uint16(v))
	case mmdbtype.Uint32:
		e.buf = binary.BigEndian.AppendUint32(append(e.buf, tagUint32), uint32(v))
	case mmdbtype.Uint64:
		e.buf = binary.BigEndian.AppendUint64(append(e.buf, tagUint64), uint64(v))
	case mmdbtype.Int32:
		e.buf = binary.BigEndian.AppendUint32(append(e.buf, tagInt32), uint32(v))
	case mmdbtype.Float32:
		e.buf = binary.BigEndian.AppendUint32(append(e.buf, tagFloat32), math.Float32bits(float32(v)))
	case mmdbtype.Float64:
		e.buf = binary.BigEndian.AppendUint64(append(e.buf, tagFloat64), math.Float64bits(float64(v)))
	case mmdbtype.Pointer:
		e.buf = binary.BigEndian.AppendUint32(append(e.buf, tagPointer), uint32(v))
	case *mmdbtype.Uint128:
		e.appendString(tagUint128, string((*big.Int)(v).Bytes()))
	default:
		return fmt.Errorf("unsupported MMDB type %T", v)
	}
	return nil
}

func (e *valueEncoder) appendString(tag byte, s string) {
	e.buf = binary.AppendUvarint(append(e.buf, tag), uint64(len(s)))
	e.buf = append(e.buf, s...)
}

// treeKeyGenerator is the mmdbwriter.KeyGenerator for the merged tree.
type treeKeyGenerator struct {
	encoder valueEncoder
	key     [sha256.Size]byte
}

func (g *treeKeyGenerator) Key(v mmdbtype.DataType) ([]byte, error) {
	key, err := g.encoder.sum(v)
	if err != nil {
		return nil, err
	}
	g.key = key
	return g.key[:], nil
}

// sharedValues deduplicates the nested values of source records. Distinct
// records still repeat most of their parts (a few hundred countries and
// continents cover millions of records), so sharing one instance of each part
// keeps only one copy in memory.
type sharedValues struct {
	encoder valueEncoder
	values  map[[sha256.Size]byte]mmdbtype.DataType
}

func newSharedValues() *sharedValues {
	return &sharedValues{values: make(map[[sha256.Size]byte]mmdbtype.DataType)}
}

// shareFields replaces each top-level value of record, which must not be
// shared yet, with the shared instance of an equal value. A nil sharedValues
// leaves the record unchanged.
func (s *sharedValues) shareFields(record mmdbtype.Map) error {
	if s == nil {
		return nil
	}
	for k, v := range record {
		key, err := s.encoder.sum(v)
		if err != nil {
			return err
		}
		if shared, ok := s.values[key]; ok {
			record[k] = shared
		} else {
			s.values[key] = v
		}
	}
	return nil
}
