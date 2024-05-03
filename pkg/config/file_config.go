package config

import (
	"github.com/fsnotify/fsnotify"
	"github.com/wskfjtheqian/hbuf_golang/pkg/erro"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hlog"
	"io/ioutil"
)

type fileConfig struct {
	path     string
	value    string
	onChange func(c string)
	hostname string
	watcher  *fsnotify.Watcher
	keyVal   map[string]any
}

func (c *fileConfig) Yaml() string {
	return c.value
}

func (c *fileConfig) CheckConfig() int {
	return 0
}

func (c *fileConfig) OnChange(call func(value string)) error {
	if 0 == len(c.value) {
		hlog.Info("配置文件路径:", c.path)
		buffer, err := ioutil.ReadFile(c.path)
		if err != nil {
			hlog.Info("读取配置文件失败:", c.path)
		}
		c.value = string(buffer)
		if nil != call {
			config, err := generateConfig(c.value, c.keyVal)
			if err != nil {
				erro.PrintStack(err)
				return err
			}
			hlog.Info("读取配置文件：" + config)
			call(config)
		}
	}
	c.onChange = call
	return nil
}

func NewFileConfig(hostname string, path string, val map[string]any) Watch {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		hlog.Error(err)
	}
	return &fileConfig{
		watcher:  watcher,
		hostname: hostname,
		path:     path,
		keyVal:   val,
	}
}
func (c *fileConfig) Close() error {
	return c.watcher.Close()
}
func (c *fileConfig) Watch() error {
	done := make(chan bool)
	go func() {
		for {
			select {
			case event, ok := <-c.watcher.Events:
				if !ok {
					return
				}
				if event.Op&fsnotify.Write == fsnotify.Write {
					hlog.Info("配置文件路径:", c.path)
					buffer, err := ioutil.ReadFile(c.path)
					if err != nil {
						hlog.Info("读取配置文件失败:", c.path)
					}
					value := string(buffer)
					if value != c.value && nil != c.onChange {
						config, err := generateConfig(c.value, c.keyVal)
						if err != nil {
							erro.PrintStack(err)
							return
						}
						hlog.Info("配置文件改变：" + config)
						c.onChange(config)
					}
					c.value = value
				}
			case err, ok := <-c.watcher.Errors:
				if !ok {
					return
				}
				hlog.Info("error:", err)
			}
		}
	}()
	err := c.watcher.Add(c.path)
	if err != nil {
		return err
	}
	<-done
	hlog.Flush()
	return nil
}
