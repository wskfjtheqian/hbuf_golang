package hip

import (
	"net"
	"net/http"
	"strings"

	"github.com/wskfjtheqian/hbuf_golang/pkg/herror"
)

// Info IP信息的结构体
type Info struct {
	Country     string  // 国家名称
	CountryCode string  // 国家代码（如 CN、US）
	City        string  // 城市名称
	Lat         float64 // 纬度
	Lon         float64 // 经度
	Query       string  // 查询的IP地址
	Region      string  // 地区/省份代码
	RegionName  string  // 地区/省份名称
	Isp         string  // 互联网服务提供商
	Org         string  // 组织机构为
	Mobile      bool    // 是否为手机移动基站网络
	Proxy       bool    // 是否为公开代理
	Hosting     bool    // 托管机房 / 云服务器
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
