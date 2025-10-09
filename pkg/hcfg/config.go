package hcfg

import (
	"bytes"
	"flag"
	"github.com/wskfjtheqian/hbuf_golang/pkg/herror"
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

var (
	hostname  = flag.String("h", "", "Host name")
	path      = flag.String("c", "", "Config.yaml file path")
	endpoints = flag.String("e", "", "Etcd endpoints")
)

func NewWatch() Watch {
	if 0 == len(*hostname) {
		hlog.Exit("Usage: -h <host name> -c <config.yaml file path> -e <etcd endpoints>")
	}

	keyVal := map[string]any{
		"HostName": hostname,
	}

	var c Watch
	if 0 != len(*endpoints) {
		hlog.Info("Host name: %s", *hostname)
		hlog.Info("Etcd endpoints: %s", *endpoints)
		c = NewEtcdConfig(*hostname, *endpoints, keyVal)
	} else if 0 != len(*path) {
		hlog.Info("Host name: %s", *hostname)
		hlog.Info("Config.yaml file path: %s", *path)
		c = NewFileConfig(*hostname, *path, keyVal)
	} else {
		hlog.Exit("please input config.yaml file path or etcd endpoints")
	}
	return c
}

func generateConfig(config string, keyVal map[string]any) (string, error) {
	parse := template.New("config")
	t, err := parse.Parse(config)
	if err != nil {
		return "", herror.Wrap(err)
	}
	w := bytes.NewBuffer(nil)
	err = t.Execute(w, keyVal)
	if err != nil {
		return "", herror.Wrap(err)
	}

	config = w.String()
	hlog.Debug("config change:\n" + config)
	return config, nil
}
