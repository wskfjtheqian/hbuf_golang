package config

import (
	"github.com/fsnotify/fsnotify"
	"io/ioutil"
	"log"
)

type fileConfig struct {
	path     string
	onChange func(c any)
}

func (c *fileConfig) OnChange(f func(c any)) {
	c.onChange = f
}

func NewFileConfig(path string) Config {
	return &fileConfig{
		path: path,
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
					log.Println("modified file:", event.Name)
					c.readConfig()
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
	if nil != c.onChange {
		c.onChange(string(buffer))
	}
	println(string(buffer))
	return nil
}
