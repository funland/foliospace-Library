package config

import (
	"os"
	"strings"
)

type Config struct {
	ConfigDir     string
	LibraryDir    string
	Addr          string
	APIToken      string
	WebTTSEnabled bool
}

func Load() Config {
	return Config{
		ConfigDir:     envOr("FOLIOSPACE_CONFIG_DIR", "/config"),
		LibraryDir:    envOr("FOLIOSPACE_LIBRARY_DIR", "/library"),
		Addr:          envOr("FOLIOSPACE_ADDR", ":8080"),
		APIToken:      os.Getenv("FOLIOSPACE_API_TOKEN"),
		WebTTSEnabled: envBool("FOLIOSPACE_WEB_TTS_ENABLED"),
	}
}

func envOr(key string, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func envBool(key string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
