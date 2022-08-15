package base

import (
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
}
