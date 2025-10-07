package hip

import (
	"github.com/wskfjtheqian/hbuf_golang/pkg/herror"
	"net"
	"net/http"
	"strings"
)

// Info IP信息的结构体
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

var ipMap = make(map[Key]GetIpInfoCall)

var defaultCall GetIpInfoCall

func init() {
	ipMap[IpApiKey] = ipApi
	defaultCall = ipApi
}

// SetDefault 设置默认的IP信息获取方式
func SetDefault(key Key) error {
	val, ok := ipMap[key]
	if !ok {
		return herror.NewError("Setting the key to obtain IP information is invalid")
	}
	defaultCall = val
	return nil
}

// GetIpInfo 获取IP信息
func GetIpInfo(ip string) (*Info, error) {
	if nil != defaultCall {
		return defaultCall(ip)
	}
	return nil, herror.NewError("Must not find a way to get IP information")
}

// AddIpInfoGetter 添加IP信息获取方式
func AddIpInfoGetter(key Key, call GetIpInfoCall) {
	ipMap[key] = call
}

// GetHttpIP 从http请求中获取IP地址
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
		return "", herror.Wrap(err)
	}

	if net.ParseIP(ip) != nil {
		return ip, nil
	}

	return "", herror.NewError("no valid ip found")
}
