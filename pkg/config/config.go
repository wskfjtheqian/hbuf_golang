package config

import (
	"bytes"
	"flag"
	"github.com/wskfjtheqian/hbuf_golang/pkg/erro"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hlog"
	"html/template"
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

func NewWatch() Watch {
	var hostname string
	flag.StringVar(&hostname, "h", "", "Host name")
	var path string
	flag.StringVar(&path, "c", "", "Config.yaml file path")
	var endpoints string
	flag.StringVar(&endpoints, "e", "", "Etcd endpoints")
	flag.Parse()

	if 0 == len(hostname) {
		hlog.Exitln("请输入 Host name")
	}

	keyVal := map[string]any{
		"HostName": hostname,
	}

	var c Watch
	if 0 != len(endpoints) {
		hlog.Infoln("Host name:" + hostname)
		hlog.Infoln("Etcd endpoints:" + endpoints)
		c = NewEtcdConfig(hostname, endpoints, keyVal)
	} else if 0 != len(path) {
		hlog.Infoln("Host name:" + hostname)
		hlog.Infoln("Config.yaml file path:" + path)
		c = NewFileConfig(hostname, path, keyVal)
	} else {
		hlog.Errorln("请输入 config.yaml file path or etcd endpoints")
	}
	return c
}

func generateConfig(config string, keyVal map[string]any) (string, error) {
	parse := template.New("config")
	t, err := parse.Parse(config)
	if err != nil {
		return "", erro.Wrap(err)
	}
	w := bytes.NewBuffer(nil)
	err = t.Execute(w, keyVal)
	if err != nil {
		return "", erro.Wrap(err)
	}
	return w.String(), nil
}
