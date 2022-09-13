package manage

import (
	"gopkg.in/yaml.v2"
	"log"
	"os"
	"testing"
)

func Test_ReadConfig(t *testing.T) {
	f, err := os.Open("config.yaml")
	if err != nil {
		log.Fatalf("os.Open() failed with '%s'\n", err)
	}
	defer f.Close()

	var dec = yaml.NewDecoder(f)
	var config Config
	err = dec.Decode(&config)
	if err != nil {
		log.Fatalf("dec.Decode() failed with '%s'\n", err)
	}
}
