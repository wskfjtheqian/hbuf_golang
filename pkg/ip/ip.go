package ip

import (
	"errors"
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
		return errors.New("Setting the key to obtain IP information is invalid")
	}
	defaultCall = val
	return nil
}

func GetIpInfo(ip string) (*Info, error) {
	if nil != defaultCall {
		return defaultCall(ip)
	}
	return nil, errors.New("Must not find a way to get IP information")
}

func GetHttpIP(r *http.Request) (string, error) {
	ip := r.Header.Get("X-Real-IP")
	if net.ParseIP(ip) != nil {
		return ip, nil
	}

	ip = r.Header.Get("X-Forward-For")
	for _, i := range strings.Split(ip, ",") {
		if net.ParseIP(i) != nil {
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

	return "", errors.New("no valid ip found")
}
