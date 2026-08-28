package hcfg

import (
	"context"
	"os"

	"github.com/fsnotify/fsnotify"
	"github.com/wskfjtheqian/hbuf_golang/pkg/herror"
	"github.com/wskfjtheqian/hbuf_golang/pkg/hlog"
)

type fileConfig struct {
	path     string
	value    string
	onChange OnChange
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

func (c *fileConfig) OnChange(onChange OnChange) {
	c.onChange = onChange
}

func NewFileConfig(ctx context.Context, hostname string, path string, val map[string]any) Watch {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		hlog.Error(ctx, "watch error: %s", err)
	}
	return &fileConfig{
		watcher:  watcher,
		hostname: hostname,
		path:     path,
		keyVal:   val,
	}
}
func (c *fileConfig) Close(ctx context.Context) error {
	return c.watcher.Close()
}

func (c *fileConfig) Watch(ctx context.Context) error {
	if 0 == len(c.value) {
		buffer, err := os.ReadFile(c.path)
		if err != nil {
			return herror.Wrap(err)
		}
		c.value = string(buffer)
		if nil != c.onChange {
			config, err := generateConfig(ctx, c.value, c.keyVal)
			if err != nil {
				return herror.Wrap(err)
			}
			err = c.onChange(ctx, config)
			if err != nil {
				return err
			}
		}
	}
	go func() {
		hlog.Info(ctx, "start watch config file: %s", c.path)
		for {
			ctx = hlog.WithContext(ctx, "")
			select {
			case event, ok := <-c.watcher.Events:
				if !ok {
					return
				}
				if event.Op&event.Op == fsnotify.Write {
					hlog.Info(ctx, "config file change: %s", c.path)
					buffer, err := os.ReadFile(c.path)
					if err != nil {
						hlog.Error(ctx, "read config file error: %s", c.path)
						return
					}
					value := string(buffer)
					if value != c.value && nil != c.onChange {
						config, err := generateConfig(ctx, value, c.keyVal)
						if err != nil {
							herror.PrintStack(ctx, err)
							return
						}
						c.onChange(ctx, config)
					}
					c.value = value
				}
			case err, ok := <-c.watcher.Errors:
				if !ok {
					return
				}
				hlog.Error(ctx, "watch error: %s", err)
			case <-ctx.Done():
				return
			}
		}
	}()
	err := c.watcher.Add(c.path)
	if err != nil {
		return err
	}
	return nil
}
