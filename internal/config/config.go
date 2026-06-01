package config

import "os"

type Config struct {
	ConfigDir  string
	LibraryDir string
	Addr       string
	APIToken   string
}

func Load() Config {
	return Config{
		ConfigDir:  envOr("FOLIOSPACE_CONFIG_DIR", "/config"),
		LibraryDir: envOr("FOLIOSPACE_LIBRARY_DIR", "/library"),
		Addr:       envOr("FOLIOSPACE_ADDR", ":8080"),
		APIToken:   os.Getenv("FOLIOSPACE_API_TOKEN"),
	}
}

func envOr(key string, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
