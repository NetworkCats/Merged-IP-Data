package merger

import (
	"bytes"
	"reflect"
	"testing"

	"github.com/maxmind/mmdbwriter/mmdbtype"
)

func TestTreeKeyGeneratorMatchesEquality(t *testing.T) {
	values := []mmdbtype.DataType{
		mmdbtype.Map{keyCountry: mmdbtype.Map{keyISOCode: mmdbtype.String("US")}, keyPostal: mmdbtype.Map{keyCode: mmdbtype.String("1")}},
		mmdbtype.Map{keyPostal: mmdbtype.Map{keyCode: mmdbtype.String("1")}, keyCountry: mmdbtype.Map{keyISOCode: mmdbtype.String("US")}},
		mmdbtype.Map{keyCountry: mmdbtype.Map{keyISOCode: mmdbtype.String("CN")}},
		mmdbtype.Map{keyCountry: mmdbtype.Map{keyISOCode: mmdbtype.Bytes("CN")}},
		mmdbtype.Slice{mmdbtype.String("ab")},
		mmdbtype.Slice{mmdbtype.String("a"), mmdbtype.String("b")},
		mmdbtype.Slice{mmdbtype.Slice{}, mmdbtype.Slice{mmdbtype.Slice{}}},
		mmdbtype.Slice{mmdbtype.Slice{mmdbtype.Slice{}}, mmdbtype.Slice{}},
		mmdbtype.Uint16(1),
		mmdbtype.Uint32(1),
		mmdbtype.Uint64(1),
		mmdbtype.Int32(1),
		mmdbtype.Float32(1),
		mmdbtype.Float64(1),
		mmdbtype.Float64(2),
		mmdbtype.Bool(true),
		mmdbtype.Bool(false),
	}

	var generator treeKeyGenerator
	keys := make([][]byte, len(values))
	for i, value := range values {
		key, err := generator.Key(value)
		if err != nil {
			t.Fatal(err)
		}
		keys[i] = bytes.Clone(key)
	}
	for i := range values {
		for j := range values {
			sameKey := bytes.Equal(keys[i], keys[j])
			if equal := values[i].Equal(values[j]); sameKey != equal {
				t.Errorf("values %d and %d: same key = %v, Equal = %v", i, j, sameKey, equal)
			}
		}
	}
}

func TestSharedValuesShareEqualFields(t *testing.T) {
	newRecord := func(city string) mmdbtype.Map {
		return (&MergedRecord{
			City:    CityRecord{Names: map[string]string{"en": city}},
			Country: CountryRecord{ISOCode: "US", Names: map[string]string{"en": "United States"}},
		}).ToMMDBType()
	}
	first, second := newRecord("Austin"), newRecord("Boston")

	shared := newSharedValues()
	for _, record := range []mmdbtype.Map{first, second} {
		if err := shared.shareFields(record); err != nil {
			t.Fatal(err)
		}
	}

	if !sameMap(first[keyCountry], second[keyCountry]) {
		t.Fatal("equal country values are not shared")
	}
	if sameMap(first[keyCity], second[keyCity]) {
		t.Fatal("different city values are shared")
	}
	if !first.Equal(newRecord("Austin")) || !second.Equal(newRecord("Boston")) {
		t.Fatal("sharing changed the records")
	}
}

func sameMap(a, b mmdbtype.DataType) bool {
	return reflect.ValueOf(a).UnsafePointer() == reflect.ValueOf(b).UnsafePointer()
}
