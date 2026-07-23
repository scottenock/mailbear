package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/laputalabs/mailbear"
	log "github.com/sirupsen/logrus"
)

// version is stamped at build time via -ldflags "-X main.version=...".
// It defaults to DEV for local/unversioned builds.
var version = "DEV"

func getenv(key, fallback string) string {
	value := os.Getenv(key)
	if len(value) == 0 {
		return fallback
	}
	return value
}

func main() {
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println(version)
		return
	}

	log.Infof("Starting MailBear %s", version)

	configFile := getenv("CONFIG_FILE", "config.yml")
	config, err := mailbear.GetConfigFromFile(configFile)
	if err != nil {
		log.Fatalf("couldn't read config: %v", err)
	}

	mailbear.Serve(config)
}
