package manage

import "gopkg.in/yaml.v3"

type Config struct {
	Server *Server `yaml:"server"` //服务配置
	Client *Client `yaml:"client"` //客服配置
}

func (c *Config) Yaml() string {
	bytes, err := yaml.Marshal(c)
	if err != nil {
		return ""
	}
	return string(bytes)
}

func (con *Config) CheckConfig() int {
	errCount := 0
	errCount += con.Server.CheckConfig()
	errCount += con.Client.CheckConfig()

	return errCount
}

//Http 服务配置
type Http struct {
	Hostname *string `yaml:"hostname"` //主机名
	Address  *string `yaml:"address"`  //监听地址
	Crt      *string `yaml:"crt"`      //crt证书
	Key      *string `yaml:"key"`      //crt密钥
	Path     *string `yaml:"path"`     //路径
}

func (h *Http) Yaml() string {
	bytes, err := yaml.Marshal(h)
	if err != nil {
		return ""
	}
	return string(bytes)
}

func (h *Http) CheckConfig() int {
	errCount := 0

	return errCount
}

// Server 服务配置
type Server struct {
	Register  bool      `yaml:"register"`   //是否注册服务到注册中心
	Local     bool      `yaml:"local"`      //是否开启本地服务
	Http      *Http     `yaml:"http"`       //Http 服务配置
	WebSocket *Http     `yaml:"web_socket"` //WebSocket 服务配置
	List      *[]string `yaml:"list"`       //开始的服务列表
}

func (s *Server) Yaml() string {
	bytes, err := yaml.Marshal(s)
	if err != nil {
		return ""
	}
	return string(bytes)
}

func (s *Server) CheckConfig() int {
	errCount := 0

	return errCount
}

type Client struct {
	Find   bool                `yaml:"find"` //是否开启服务发现功能
	Server map[string][]string `yaml:"server"`
}

func (c *Client) Yaml() string {
	bytes, err := yaml.Marshal(c)
	if err != nil {
		return ""
	}
	return string(bytes)
}

func (c *Client) CheckConfig() int {
	errCount := 0

	return errCount
}
