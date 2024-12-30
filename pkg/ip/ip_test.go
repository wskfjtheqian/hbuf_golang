package ip

import (
	"encoding/json"
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

	marshal, err := json.MarshalIndent(api, "", "\t")
	if err != nil {
		t.Error(err)
		return
	}
	t.Log(string(marshal))
}
