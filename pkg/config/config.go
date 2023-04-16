package config

import (
	"flag"
	"log"
)

type Watch interface {
	Watch() error
	Close() error
	OnChange(call func(value string)) error
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

func NewWatch() Watch {
	var hostname string
	flag.StringVar(&hostname, "h", "", "Host name")
	var path string
	flag.StringVar(&path, "c", "", "Config.yaml file path")
	var endpoints string
	flag.StringVar(&endpoints, "e", "", "Etcd endpoints")
	flag.Parse()
	if 0 == len(hostname) {
		log.Fatal("请输入 Host name")
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
		log.Fatal("请输入 config.yaml file path or etcd endpoints")
	}
	return c
}
