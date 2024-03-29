package ip

import (
	"github.com/wskfjtheqian/hbuf_golang/pkg/erro"
	"net"
	"net/http"
	"strings"
)

type Info struct {
	Country     string
	CountryCode string
	City        string
	Lat         float64
	Lon         float64
	Query       string
	Region      string
	RegionName  string
}

type Key string

type GetIpInfoCall = func(ip string) (*Info, error)

var IpMap = make(map[Key]GetIpInfoCall)

var defaultCall GetIpInfoCall

func init() {
	IpMap[IpApiKey] = ipApi
	defaultCall = ipApi
}

func SetDefault(key Key) error {
	val, ok := IpMap[key]
	if !ok {
		return erro.NewError("Setting the key to obtain IP information is invalid")
	}
	defaultCall = val
	return nil
}

func GetIpInfo(ip string) (*Info, error) {
	if nil != defaultCall {
		return defaultCall(ip)
	}
	return nil, erro.NewError("Must not find a way to get IP information")
}

func GetHttpIP(r *http.Request, headers ...string) (string, error) {
	var h []string
	h = append(h, headers...)
	h = append(h, "X-Forwarded-For", "X-Real-IP")

	for _, item := range h {
		ip := r.Header.Get(item)
		for _, i := range strings.Split(ip, ",") {
			if i != "" && net.ParseIP(i) != nil {
				return i, nil
			}
		}
	}

	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return "", err
	}

	if net.ParseIP(ip) != nil {
		return ip, nil
	}

	return "", erro.NewError("no valid ip found")
}
