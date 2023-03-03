package manage

type Config struct {
	Server Server `yaml:"server"` //服务配置
	Client Client `yaml:"client"` //客服配置
}

func (con *Config) CheckConfig() int {
	errCount := 0
	errCount += con.Server.CheckConfig()
	errCount += con.Client.CheckConfig()

	return errCount
}

//Http 服务配置
type Http struct {
	Address *string `yaml:"address"` //监听地址
	Crt     *string `yaml:"crt"`     //crt证书
	Key     *string `yaml:"key"`     //crt密钥
	Path    *string `yaml:"path"`    //路径
}

func (con *Http) CheckConfig() int {
	errCount := 0

	return errCount
}

// Server 服务配置
type Server struct {
	Local     *bool `yaml:"local"`      //是否开启本地服务
	Http      *Http `yaml:"http"`       //Http 服务配置
	WebSocket *Http `yaml:"web_socket"` //WebSocket 服务配置
}

func (con *Server) CheckConfig() int {
	errCount := 0

	return errCount
}

type ClientServer struct {
	Local     *bool   `yaml:"local"` //是否使用本地服务
	Http      *string `yaml:"http"`
	Websocket *string `yaml:"websocket"`
}

func (con *ClientServer) CheckConfig() int {
	errCount := 0

	return errCount
}

type Client struct {
	Server map[string]ClientServer `yaml:"server"`
	List   map[string][]string     `yaml:"list"`
}

func (con *Client) CheckConfig() int {
	errCount := 0

	return errCount
}
