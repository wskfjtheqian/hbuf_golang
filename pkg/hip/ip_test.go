package hip

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"testing"
)

// TestIpApi 测试ipApi函数
func TestIpApi(t *testing.T) {
	IpApiSecret = "ENofv1CDwDTUqAc"
	api, err := ipApi("125.71.133.128")
	if err != nil {
		t.Error(err)
		return
	}

	marshal, err := json.Marshal(api, jsontext.WithIndent("\t"))
	if err != nil {
		t.Error(err)
		return
	}
	t.Log(string(marshal))
}
