package utl

import (
	"gopkg.in/yaml.v2"
	"io"
	"log"
	"os"
)

type Config interface {
	CheckConfig() int
}

func ReadConfig(r io.Reader, config Config) *Config {
	var dec = yaml.NewDecoder(r)
	err := dec.Decode(config)
	if err != nil {
		log.Fatalf("解析配置文件失败，请检查配置文件书写是否有误 '%s'\n", err)
	}
	errCount := config.CheckConfig()
	if 0 < errCount {
		os.Exit(1)
	}
	return &config
}
