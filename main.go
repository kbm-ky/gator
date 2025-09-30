package main

import (
	"fmt"
	"log"

	"github.com/kbm-ky/gator/internal/config"
)

func main() {
	configFile, err := config.Read()
	if err != nil {
		log.Fatalf("unable to read config: %v", err)
	}
	configFile.SetUser("kyle")
	configFile, err = config.Read()
	if err != nil {
		log.Fatalf("unable to read config: %v", err)
	}
	fmt.Printf("%v\n", configFile)
}
