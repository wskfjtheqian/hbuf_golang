package ip

import "testing"

func TestIpApi(t *testing.T) {
	IpApiSecret = "ENofv1CDwDTUqAc"
	ipApi("125.71.133.128")
}
