package hcfg

import (
	"bytes"
	"context"
	"flag"
	"html/template"

	"github.com/wskfjtheqian/hbuf_golang/pkg/herror"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hlog"
)

type OnChange func(ctx context.Context, value string) error

type Watch interface {
	Watch(ctx context.Context) error
	Close(ctx context.Context) error
	OnChange(onChange OnChange)
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

func NewWatch(ctx context.Context) Watch {
	if 0 == len(*hostname) {
		hlog.Exit(ctx, "Usage: -h <host name> -c <config.yaml file path> -e <etcd endpoints>")
	}

	keyVal := map[string]any{
		"HostName": hostname,
	}

	var c Watch
	if 0 != len(*endpoints) {
		hlog.Info(ctx, "Host name: %s", *hostname)
		hlog.Info(ctx, "Etcd endpoints: %s", *endpoints)
		c = NewEtcdConfig(ctx, *hostname, *endpoints, keyVal)
	} else if 0 != len(*path) {
		hlog.Info(ctx, "Host name: %s", *hostname)
		hlog.Info(ctx, "Config.yaml file path: %s", *path)
		c = NewFileConfig(ctx, *hostname, *path, keyVal)
	} else {
		hlog.Exit(ctx, "please input config.yaml file path or etcd endpoints")
	}
	return c
}

func generateConfig(ctx context.Context, config string, keyVal map[string]any) (string, error) {
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
	hlog.Debug(ctx, "config change:\n"+config)
	return config, nil
}
