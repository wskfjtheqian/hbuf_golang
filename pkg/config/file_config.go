package config

import (
	"github.com/fsnotify/fsnotify"
	"io/ioutil"
	"log"
)

type fileConfig struct {
	path     string
	value    string
	onChange func(c string)
	hostname string
	watcher  *fsnotify.Watcher
}

func (c *fileConfig) Yaml() string {
	return c.value
}

func (c *fileConfig) CheckConfig() int {
	return 0
}

func (c *fileConfig) OnChange(call func(value string)) error {
	if 0 == len(c.value) {
		err := c.readConfig()
		if err != nil {
			return err
		}
		if nil != call {
			call(c.value)
		}
	}
	c.onChange = call
	return nil
}

func NewFileConfig(hostname string, path string) Watch {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Fatal(err)
	}
	return &fileConfig{
		watcher:  watcher,
		hostname: hostname,
		path:     path,
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
					log.Println("配置文件忆 file:", event.Name)
					err := c.readConfig()
					if err != nil {
						log.Println("读取配置文件失败:", c.path)
					}
				}
			case err, ok := <-c.watcher.Errors:
				if !ok {
					return
				}
				log.Println("error:", err)
			}
		}
	}()
	err := c.watcher.Add(c.path)
	if err != nil {
		return err
	}
	<-done
	return nil
}

func (c *fileConfig) readConfig() error {
	buffer, err := ioutil.ReadFile(c.path)
	if err != nil {
		return err
	}
	value := string(buffer)
	if c.value != value {
		if nil != c.onChange {
			c.onChange(string(buffer))
		}
	}
	return nil
}
