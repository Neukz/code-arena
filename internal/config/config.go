// Package config loads environment-based configuration shared by api and worker.
package config

import (
	"fmt"
	"os"
)

// Config holds environment variables shared by the API gateway and worker.
type Config struct {
	APIPort     string
	DatabaseURL string
	RabbitMQURL string
	LogLevel    string
}

// Load reads configuration from environment variables.
// Required variables return an error when missing; optional variables use sensible defaults.
func Load() (*Config, error) {
	port := os.Getenv("API_PORT")
	if port == "" {
		port = "8080"
	}

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		return nil, fmt.Errorf("DATABASE_URL environment variable is required")
	}

	logLevel := os.Getenv("LOG_LEVEL")
	if logLevel == "" {
		logLevel = "info"
	}

	return &Config{
		APIPort:     port,
		DatabaseURL: dbURL,
		RabbitMQURL: os.Getenv("RABBITMQ_URL"),
		LogLevel:    logLevel,
	}, nil
}
