package main

import (
	"flag"
	"fmt"
	"os"

	"ssw-logs-capture/internal/app"
)

func main() {
	// Parse command line flags
	var configFile string
	flag.StringVar(&configFile, "config", "", "Path to configuration file")
	flag.Parse()

	// Check environment variables for config file paths if not provided via flag
	if configFile == "" {
		if envConfigFile := os.Getenv("SSW_CONFIG_FILE"); envConfigFile != "" {
			configFile = envConfigFile
		} else {
			// Default config file path
			configFile = "/app/configs/config.yaml"
		}
	}

	fmt.Printf("Using configuration file: %s\n", configFile)

	// Create and run application
	application, err := app.New(configFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to create application: %v\n", err)
		os.Exit(1)
	}

	if err := application.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Application error: %v\n", err)
		os.Exit(1)
	}
}