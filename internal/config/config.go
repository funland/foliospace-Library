package config

import (
	"os"
	"strconv"
)

type Config struct {
	ConfigDir     string
	LibraryDir    string
	Addr          string
	APIToken      string
	MemoryLimitMB int
}

func Load() Config {
	return Config{
		ConfigDir:     envOr("FOLIOSPACE_CONFIG_DIR", "/config"),
		LibraryDir:    envOr("FOLIOSPACE_LIBRARY_DIR", "/library"),
		Addr:          envOr("FOLIOSPACE_ADDR", ":8080"),
		APIToken:      os.Getenv("FOLIOSPACE_API_TOKEN"),
		MemoryLimitMB: envIntOr("FOLIOSPACE_MEMORY_LIMIT_MB", 768),
	}
}

func envOr(key string, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func envIntOr(key string, fallback int) int {
	value, err := strconv.Atoi(os.Getenv(key))
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}
