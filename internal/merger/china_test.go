package merger

import (
	"testing"

	"github.com/maxmind/mmdbwriter/mmdbtype"
)

func TestChinaDictionaryIsComplete(t *testing.T) {
	for province, cities := range chinaCities {
		p, ok := chinaProvinces[province]
		if !ok {
			t.Fatalf("cities listed under unknown province %q", province)
		}
		if p.En == "" || p.ISOCode == "" || p.GeonameID == 0 || p.TimeZone == "" {
			t.Fatalf("province %q is incomplete: %#v", province, p)
		}
		for name, city := range cities {
			if city.En == "" || city.GeonameID == 0 {
				t.Fatalf("city %s/%s is incomplete: %#v", province, name, city)
			}
			if city.Latitude < 18 || city.Latitude > 54 || city.Longitude < 73 || city.Longitude > 135 {
				t.Fatalf("city %s/%s has coordinates outside China: %#v", province, name, city)
			}
		}
	}
}

func TestResolveChinaLocation(t *testing.T) {
	tests := []struct {
		region, city   string
		wantProvinceEn string
		wantCityEn     string
		wantCityZh     string
	}{
		{"广东", "广州", "Guangdong", "Guangzhou", "广州"},
		{"吉林", "吉林市", "Jilin", "Jilin", "吉林市"},
		{"吉林", "延边朝鲜族自治州", "Jilin", "Yanbian", "延边朝鲜族自治州"},
		{"内蒙古", "兴安盟市", "Inner Mongolia", "Hinggan", "兴安盟"},
		{"山东", "青岛胶南市", "Shandong", "Qingdao", "青岛"},
		{"北京", "", "Beijing", "Beijing", "北京"},
		{"新疆乌鲁木齐", "", "Xinjiang", "Urumqi", "乌鲁木齐"},
		{"浙江", "", "Zhejiang", "", ""},
		{"广西", "柳州高新区", "Guangxi", "Liuzhou", "柳州"},
	}
	for _, tt := range tests {
		loc := resolveChinaLocation(tt.region, tt.city)
		if loc == nil {
			t.Fatalf("resolveChinaLocation(%q, %q) = nil", tt.region, tt.city)
		}
		if got := placeName(loc.subdivision, "en"); got != tt.wantProvinceEn {
			t.Fatalf("%s/%s province en = %q, want %q", tt.region, tt.city, got, tt.wantProvinceEn)
		}
		if got := placeName(loc.city, "en"); got != tt.wantCityEn {
			t.Fatalf("%s/%s city en = %q, want %q", tt.region, tt.city, got, tt.wantCityEn)
		}
		if got := placeName(loc.city, "zh-CN"); got != tt.wantCityZh {
			t.Fatalf("%s/%s city zh-CN = %q, want %q", tt.region, tt.city, got, tt.wantCityZh)
		}
	}

	for _, region := range []string{"", "台湾屏东市", "香港葵湾", "IANA"} {
		if loc := resolveChinaLocation(region, ""); loc != nil {
			t.Fatalf("resolveChinaLocation(%q) = %#v, want nil", region, loc)
		}
	}
}

func TestApplyChinaLocationReplacesDifferentCity(t *testing.T) {
	existing := testChinaRecord("SH", 1796231, "Shanghai", 1796236, "Shanghai")
	existing[keyPostal] = mmdbtype.Map{keyCode: mmdbtype.String("200000")}
	existing[keyASN] = mmdbtype.Map{keyASNumber: mmdbtype.Uint32(4134)}

	result, applied := applyChinaLocation(existing, resolveChinaLocation("浙江", "杭州"))
	if !applied {
		t.Fatal("applyChinaLocation did not apply to a CN record")
	}
	city := result[keyCity].(mmdbtype.Map)
	if placeName(city, "en") != "Hangzhou" || placeName(city, "zh-CN") != "杭州" || placeName(city, "de") != "" {
		t.Fatalf("city = %#v, want only the dictionary names for Hangzhou", city)
	}
	subdivision := firstSubdivision(result)
	if subdivision[keyISOCode] != mmdbtype.String("ZJ") || placeName(subdivision, "en") != "Zhejiang" {
		t.Fatalf("subdivision = %#v, want Zhejiang", subdivision)
	}
	location := result[keyLocation].(mmdbtype.Map)
	if location[keyLatitude] != mmdbtype.Float64(chinaCities["浙江"]["杭州"].Latitude) {
		t.Fatalf("location = %#v, want Hangzhou coordinates", location)
	}
	if _, ok := result[keyPostal]; ok {
		t.Fatal("postal code of the old city should be dropped")
	}
	if _, ok := result[keyASN]; !ok {
		t.Fatal("non-location fields should be kept")
	}
	if placeName(existing[keyCity].(mmdbtype.Map), "en") != "Shanghai" {
		t.Fatal("existing record was mutated")
	}
}

func TestApplyChinaLocationKeepsSameCityDetails(t *testing.T) {
	existing := testChinaRecord("GD", 1809935, "Guangdong", 1809858, "Guangzhou")
	existing[keyPostal] = mmdbtype.Map{keyCode: mmdbtype.String("510000")}

	result, _ := applyChinaLocation(existing, resolveChinaLocation("广东", "广州"))
	city := result[keyCity].(mmdbtype.Map)
	if placeName(city, "zh-CN") != "广州" || placeName(city, "de") != "Guangzhou-de" {
		t.Fatalf("city = %#v, want QQWry zh-CN plus existing languages", city)
	}
	if result[keyLocation].(mmdbtype.Map)[keyLatitude] != mmdbtype.Float64(1) {
		t.Fatal("existing coordinates of the same city should be kept")
	}
	if _, ok := result[keyPostal]; !ok {
		t.Fatal("postal code of the same city should be kept")
	}
}

func TestApplyChinaLocationProvinceOnly(t *testing.T) {
	sameProvince := testChinaRecord("ZJ", 1784764, "Zhejiang", 1808926, "Hangzhou")
	result, _ := applyChinaLocation(sameProvince, resolveChinaLocation("浙江", ""))
	if placeName(result[keyCity].(mmdbtype.Map), "en") != "Hangzhou" {
		t.Fatal("city inside the QQWry province should be kept")
	}

	otherProvince := testChinaRecord("SH", 1796231, "Shanghai", 1796236, "Shanghai")
	result, _ = applyChinaLocation(otherProvince, resolveChinaLocation("浙江", ""))
	if _, ok := result[keyCity]; ok {
		t.Fatal("city outside the QQWry province should be dropped")
	}
	if result[keyLocation].(mmdbtype.Map)[keyAccuracyRadius] != mmdbtype.Uint16(chinaProvinceAccuracyRadius) {
		t.Fatal("location should fall back to province coordinates")
	}
}

func TestApplyChinaLocationIgnoresOtherCountries(t *testing.T) {
	existing := mmdbtype.Map{keyCountry: mmdbtype.Map{keyISOCode: mmdbtype.String("HK")}}
	result, applied := applyChinaLocation(existing, resolveChinaLocation("广东", "深圳"))
	if applied || len(result) != 1 {
		t.Fatalf("applyChinaLocation changed a non-CN record: %#v", result)
	}
}

func testChinaRecord(provinceISO string, provinceID uint32, provinceEn string, cityID uint32, cityEn string) mmdbtype.Map {
	return mmdbtype.Map{
		keyCountry: mmdbtype.Map{keyISOCode: mmdbtype.String("CN")},
		keySubdivisions: mmdbtype.Slice{mmdbtype.Map{
			keyGeonameID: mmdbtype.Uint32(provinceID),
			keyISOCode:   mmdbtype.String(provinceISO),
			keyNames:     mmdbtype.Map{mmdbtype.String("en"): mmdbtype.String(provinceEn)},
		}},
		keyCity: mmdbtype.Map{
			keyGeonameID: mmdbtype.Uint32(cityID),
			keyNames: mmdbtype.Map{
				mmdbtype.String("en"): mmdbtype.String(cityEn),
				mmdbtype.String("de"): mmdbtype.String(cityEn + "-de"),
			},
		},
		keyLocation: mmdbtype.Map{
			keyLatitude:  mmdbtype.Float64(1),
			keyLongitude: mmdbtype.Float64(2),
		},
	}
}

func placeName(place mmdbtype.Map, lang string) string {
	names, _ := place[keyNames].(mmdbtype.Map)
	name, _ := names[mmdbtype.String(lang)].(mmdbtype.String)
	return string(name)
}
