package app

import (
	"github.com/wskfjtheqian/hbuf_golang/pkg/etcd"
	"github.com/wskfjtheqian/hbuf_golang/pkg/nats"
)

// NewApp 新建一个App
func NewApp() *App {
	return &App{
		nats: nats.NewNats(),
	}
}

// App 应用
type App struct {
	nats *nats.Nats
	etcd *etcd.Etcd
}

// SetConfig 设置配置
func (a *App) SetConfig(conf *Config) error {
	err := a.nats.SetConfig(conf.Nats)
	if err != nil {
		return err
	}

	err = a.etcd.SetConfig(conf.Etcd)
	if err != nil {
		return err
	}
	return nil
}
