package cfg

import (
	"github.com/fsnotify/fsnotify"
	"github.com/wskfjtheqian/hbuf_golang/pkg/erro"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hlog"
	"os"
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
		buffer, err := os.ReadFile(c.path)
		if err != nil {
			hlog.Error("config file read error:", err)
		}
		c.value = string(buffer)
		if nil != call {
			config, err := generateConfig(c.value, c.keyVal)
			if err != nil {
				erro.PrintStack(err)
				return err
			}
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
					hlog.Info("config file change:", c.path)
					buffer, err := os.ReadFile(c.path)
					if err != nil {
						hlog.Error("read config file error:", c.path)
						return
					}
					value := string(buffer)
					if value != c.value && nil != c.onChange {
						config, err := generateConfig(c.value, c.keyVal)
						if err != nil {
							erro.PrintStack(err)
							return
						}
						hlog.Debug("config change:" + config)
						c.onChange(config)
					}
					c.value = value
				}
			case err, ok := <-c.watcher.Errors:
				if !ok {
					return
				}
				hlog.Error("watch error:", err)
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
