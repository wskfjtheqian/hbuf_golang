package ip

import (
	"encoding/json"
	utl "github.com/wskfjtheqian/hbuf_golang/pkg/erro"
	"net/http"
	"net/url"
)

type IpApiInfo struct {
	Status      string  `json:"status"`
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
	As          string  `json:"as"`
	Query       string  `json:"query"`
}

var IpApiKey Key = "ip-api.com"
var IpApiSecret string = ""

func ipApi(ip string) (*Info, error) {
	parse, err := url.Parse("http://ip-api.com/json/" + ip)
	if err != nil {
		return nil, err
	}
	if len(IpApiSecret) != 0 {
		query := parse.Query()
		query.Add("key", IpApiSecret)
		parse.RawQuery = query.Encode()
	}
	get, err := http.Get(parse.String())
	if err != nil {
		return nil, err
	}
	defer get.Body.Close()
	var info IpApiInfo
	err = json.NewDecoder(get.Body).Decode(&info)
	if err != nil {
		return nil, utl.Wrap(err)
	}

	return &Info{
		Country:     info.Country,
		CountryCode: info.CountryCode,
		Region:      info.Region,
		RegionName:  info.RegionName,
		City:        info.City,
		Lat:         info.Lat,
		Lon:         info.Lon,
		Query:       info.Query,
	}, nil
}
