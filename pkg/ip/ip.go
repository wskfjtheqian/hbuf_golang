package ip

import (
	utl "github.com/wskfjtheqian/hbuf_golang/pkg/utils"
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
		return utl.NewError("Setting the key to obtain IP information is invalid")
	}
	defaultCall = val
	return nil
}

func GetIpInfo(ip string) (*Info, error) {
	if nil != defaultCall {
		return defaultCall(ip)
	}
	return nil, utl.NewError("Must not find a way to get IP information")
}

func GetHttpIP(r *http.Request) (string, error) {
	if ip := r.Header.Get("X-Original-Forwarded-For"); ip != "" {
		temp := strings.Split(ip, ",")
		if 0 < len(temp) && 0 < len(temp[0]) {
			if net.ParseIP(temp[0]) != nil {
				return temp[0], nil
			}
		}
	}

	ip := r.Header.Get("X-Real-IP")
	if ip != "" && net.ParseIP(ip) != nil {
		return ip, nil
	}

	ip = r.Header.Get("X-Forward-For")
	for _, i := range strings.Split(ip, ",") {
		if i != "" && net.ParseIP(i) != nil {
			return i, nil
		}
	}

	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return "", err
	}

	if net.ParseIP(ip) != nil {
		return ip, nil
	}

	return "", utl.NewError("no valid ip found")
}
