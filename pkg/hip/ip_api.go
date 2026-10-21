package hip

import (
	"encoding/json/v2"
	"net/http"
	"net/url"

	"github.com/wskfjtheqian/hbuf_golang/pkg/herror"
)

// IpApiInfo 是ip-api.com的结构体
type IpApiInfo struct {
	Status      string  `json:"status"`
	Message     string  `json:"message"`
	Country     string  `json:"country"`
	CountryCode string  `json:"countryCode"`
	Region      string  `json:"region"`
	RegionName  string  `json:"regionName"`
	City        string  `json:"city"`
	Zip         string  `json:"zip"`
	Lat         float64 `json:"lat"`
	Lon         float64 `json:"lon"`
	Timezone    string  `json:"timezone"`
	Isp         string  `json:"isp"`
	Org         string  `json:"org"`
	Mobile      bool    `json:"mobile"`
	Proxy       bool    `json:"proxy"`
	Hosting     bool    `json:"hosting"`
	Query       string  `json:"query"`
}

var IpApiKey Key = "ip-api.com"
var IpApiSecret string = ""

// Info 是ip-api.com的返回值结构体
func ipApi(ip string) (*Info, error) {
	parse, err := url.Parse("http://ip-api.com/json/" + ip)
	if err != nil {
		return nil, herror.Wrap(err)
	}
	query := parse.Query()
	if len(IpApiSecret) != 0 {
		query.Add("key", IpApiSecret)
	}
	query.Add("fields", "status,message,country,countryCode,region,regionName,city,zip,lat,lon,timezone,isp,org,mobile,proxy,hosting,query")
	parse.RawQuery = query.Encode()
	get, err := http.Get(parse.String())
	if err != nil {
		return nil, herror.Wrap(err)
	}
	defer get.Body.Close()
	var info IpApiInfo

	err = json.UnmarshalRead(get.Body, &info)
	if err != nil {
		return nil, herror.Wrap(err)
	}

	return &Info{
		Country:     info.Country,
		CountryCode: info.CountryCode,
		City:        info.City,
		Lat:         info.Lat,
		Lon:         info.Lon,
		Query:       info.Query,
		Region:      info.Region,
		RegionName:  info.RegionName,
		Isp:         info.Isp,
		Org:         info.Org,
		Mobile:      info.Mobile,
		Proxy:       info.Proxy,
		Hosting:     info.Hosting,
	}, nil
}
