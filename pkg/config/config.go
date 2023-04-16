package config

import (
	"flag"
	"log"
)

type Watch interface {
	Watch()
}

type Value interface {
	Yaml() string
	CheckConfig() int
}

type Config struct {
	value Value
	call  func(Value)
}

func (c *Config) Value() Value {
	return c.value
}

func (c *Config) OnChange(call func(value Value)) {
	c.call = call
	if nil != c.call {
		c.call(c.value)
	}
}

func (c *Config) Change(v Value) {
	if c.value != v {
		if nil == c.value {
			c.value = v
			if nil != c.call {
				c.call(v)
			}
		} else if nil == c.value {
			c.value = v
			if nil != c.call {
				c.call(v)
			}
		} else if c.value.Yaml() != v.Yaml() {
			c.value = v
			if nil != c.call {
				c.call(v)
			}
		}
	}
}

func WatchConfig() Watch {
	var hostname string
	flag.StringVar(&hostname, "h", "", "Host name")
	var path string
	flag.StringVar(&path, "c", "", "Config.yaml file path")
	var endpoints string
	flag.StringVar(&endpoints, "e", "", "Etcd endpoints")
	flag.Parse()
	if 0 == len(hostname) {
		log.Fatal("Please input Host name")
	}

	var c Watch
	if 0 != len(endpoints) {
		log.Println("Host name:" + hostname)
		log.Println("Etcd endpoints:" + endpoints)
		c = NewEtcdConfig(hostname, endpoints)
	} else if 0 != len(path) {
		log.Println("Host name:" + hostname)
		log.Println("Config.yaml file path:" + path)
		c = NewFileConfig(hostname, path)
	} else {
		log.Fatal("Please input config.yaml file path or etcd endpoints")
	}
	go c.Watch()
	return c
}

//func ReadConfig(r io.Reader, config Config) *Config {
//	var dec = yaml.NewDecoder(r)
//	err := dec.Decode(config)
//	if err != nil {
//		log.Fatalf("解析配置文件失败，请检查配置文件书写是否有误 '%s'\n", err)
//	}
//	errCount := config.CheckConfig()
//	if 0 < errCount {
//		os.Exit(1)
//	}
//	return &config
//}
