package reader

import (
	"encoding/binary"
	"encoding/json"
	"net"
	"strings"
	"testing"
)

type testQQWryEntry struct {
	cidr   string
	fields []string
}

// buildTestQQWry builds an in-memory IPDB file with the given networks.
// IPv4 networks are stored under ::ffff:0:0/96, as real IPDB files do.
func buildTestQQWry(t *testing.T, entries []testQQWryEntry) []byte {
	t.Helper()
	const empty = -1
	type node struct{ child [2]int } // >=0 node index, empty, or <= -2 for data
	nodes := []node{{child: [2]int{empty, empty}}}
	var payload []byte
	payload = append(payload, 0, 0) // data offset 0 would collide with "empty"

	for _, entry := range entries {
		_, network, err := net.ParseCIDR(entry.cidr)
		if err != nil {
			t.Fatal(err)
		}
		ones, bits := network.Mask.Size()
		ip := network.IP.To16()
		if bits == 32 {
			ones += 96
		}
		data := strings.Join(entry.fields, "\t")
		dataRef := -2 - len(payload)
		payload = binary.BigEndian.AppendUint16(payload, uint16(len(data)))
		payload = append(payload, data...)

		current := 0
		for i := 0; i < ones; i++ {
			bit := int(ip[i>>3]>>(7-uint(i%8))) & 1
			if i == ones-1 {
				nodes[current].child[bit] = dataRef
				break
			}
			if nodes[current].child[bit] < 0 {
				nodes = append(nodes, node{child: [2]int{empty, empty}})
				nodes[current].child[bit] = len(nodes) - 1
			}
			current = nodes[current].child[bit]
		}
	}

	nodeCount := len(nodes)
	var body []byte
	for _, n := range nodes {
		for _, child := range n.child {
			value := child
			switch {
			case child == empty:
				value = nodeCount
			case child <= -2:
				value = nodeCount + (-2 - child)
			}
			body = binary.BigEndian.AppendUint32(body, uint32(value))
		}
	}
	body = append(body, payload...)

	meta, err := json.Marshal(map[string]any{
		"build":      1,
		"ip_version": 3,
		"languages":  map[string]int{"CN": 0},
		"fields":     qqwryFieldNames,
		"node_count": nodeCount,
		"total_size": len(body),
	})
	if err != nil {
		t.Fatal(err)
	}
	raw := binary.BigEndian.AppendUint32(nil, uint32(len(meta)))
	raw = append(raw, meta...)
	return append(raw, body...)
}

func TestQQWryNetworksAndLookup(t *testing.T) {
	raw := buildTestQQWry(t, []testQQWryEntry{
		{"1.0.0.0/24", []string{"中国", "广东", "广州", "", "", "电信", "CN", "AP"}},
		{"1.0.1.0/24", []string{"中国", "浙江", "", "", "IDC机房", "阿里云", "CN", "AP"}},
		{"2001:db8::/32", []string{"美国", "", "", "", "", "", "US", "NA"}},
	})
	r, err := newQQWryReader(raw)
	if err != nil {
		t.Fatal(err)
	}

	var got []string
	err = r.Networks(func(network *net.IPNet, record *QQWryRecord) error {
		got = append(got, network.String()+" "+record.RegionName+"/"+record.CityName+"/"+record.ISPDomain+"/"+record.CountryCode)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"1.0.0.0/24 广东/广州/电信/CN",
		"1.0.1.0/24 浙江//阿里云/CN",
		"2001:db8::/32 ///US",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("networks =\n%s\nwant\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}

	record, err := r.Lookup(net.ParseIP("1.0.1.200"))
	if err != nil {
		t.Fatal(err)
	}
	if record.RegionName != "浙江" || record.OwnerDomain != "IDC机房" || !record.IsChina() {
		t.Fatalf("Lookup = %#v, want 浙江 IDC record", record)
	}
	if _, err := r.Lookup(net.ParseIP("1.0.2.1")); err == nil {
		t.Fatal("Lookup of a missing network should fail")
	}
}

func TestQQWryIsChinaExcludesHongKongMacaoTaiwan(t *testing.T) {
	for _, code := range []string{"HK", "MO", "TW"} {
		record := QQWryRecord{CountryName: "中国", CountryCode: code}
		if record.IsChina() {
			t.Fatalf("IsChina() = true for %s", code)
		}
	}
}
