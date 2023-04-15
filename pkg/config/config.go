package config

import (
	"flag"
	"log"
)

type Config interface {
	Watch()
	OnChange(func(c any))
}

func Watch() Config {
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

	var c Config
	if 0 != len(endpoints) {
		log.Println("Host name:" + hostname)
		log.Println("Etcd endpoints:" + endpoints)
		c = NewEtcdConfig(endpoints)
	} else if 0 != len(path) {
		log.Println("Host name:" + hostname)
		log.Println("Config.yaml file path:" + path)
		c = NewFileConfig(path)
	} else {
		log.Fatal("Please input config.yaml file path or etcd endpoints")
	}
	c.Watch()
	return c
}
