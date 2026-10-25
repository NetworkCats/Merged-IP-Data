package reader

import (
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/maxmind/mmdbwriter"
	"github.com/maxmind/mmdbwriter/mmdbtype"
)

func TestNetworkOffsetSharesOffsetForSharedRecord(t *testing.T) {
	tree, err := mmdbwriter.New(mmdbwriter.Options{DatabaseType: "test", IPVersion: 4})
	if err != nil {
		t.Fatal(err)
	}
	shared := mmdbtype.Map{"country_code": mmdbtype.String("US")}
	for _, cidr := range []string{"1.0.0.0/24", "1.0.2.0/24"} {
		_, network, _ := net.ParseCIDR(cidr)
		if err := tree.Insert(network, shared); err != nil {
			t.Fatal(err)
		}
	}
	_, other, _ := net.ParseCIDR("1.0.4.0/24")
	if err := tree.Insert(other, mmdbtype.Map{"country_code": mmdbtype.String("CN")}); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(t.TempDir(), "test.mmdb")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tree.WriteTo(file); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	r, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	offsets := map[string]uintptr{}
	networks := r.Networks()
	for networks.Next() {
		network, offset, err := NetworkOffset(networks)
		if err != nil {
			t.Fatal(err)
		}
		offsets[network.String()] = offset
	}
	if err := networks.Err(); err != nil {
		t.Fatal(err)
	}

	if len(offsets) != 3 {
		t.Fatalf("networks = %v, want 3", offsets)
	}
	if offsets["1.0.0.0/24"] != offsets["1.0.2.0/24"] {
		t.Fatalf("networks with the same record have offsets %v", offsets)
	}
	if offsets["1.0.0.0/24"] == offsets["1.0.4.0/24"] {
		t.Fatalf("networks with different records share offset %v", offsets)
	}

	var record GeoWhoisCountryRecord
	if err := r.Decode(offsets["1.0.4.0/24"], &record); err != nil {
		t.Fatal(err)
	}
	if record.CountryCode != "CN" {
		t.Fatalf("decoded country = %q, want CN", record.CountryCode)
	}
}
