package config

import (
	"github.com/fsnotify/fsnotify"
	"io/ioutil"
	"log"
)

type fileConfig struct {
	path     string
	value    string
	onChange func(c any)
	hostname string
}

func (c *fileConfig) Yaml() string {
	return c.value
}

func (c *fileConfig) CheckConfig() int {
	return 0
}

func (c *fileConfig) OnChange(f func(c any)) {
	c.onChange = f
}

func NewFileConfig(hostname string, path string) Watch {
	return &fileConfig{
		hostname: hostname,
		path:     path,
	}
}

func (c *fileConfig) Watch() {
	err := c.readConfig()
	if err != nil {
		log.Fatal(err)
		return
	}
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Fatal(err)
	}
	defer watcher.Close()
	done := make(chan bool)
	go func() {
		for {
			select {
			case event, ok := <-watcher.Events:
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
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				log.Println("error:", err)
			}
		}
	}()
	err = watcher.Add(c.path)
	if err != nil {
		log.Fatal(err)
	}
	<-done
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
