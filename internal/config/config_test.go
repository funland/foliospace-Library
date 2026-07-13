package config

import "testing"

func TestLoadUsesNASDefaults(t *testing.T) {
	t.Setenv("FOLIOSPACE_CONFIG_DIR", "")
	t.Setenv("FOLIOSPACE_LIBRARY_DIR", "")
	t.Setenv("FOLIOSPACE_ADDR", "")
	t.Setenv("FOLIOSPACE_API_TOKEN", "")
	t.Setenv("FOLIOSPACE_MEMORY_LIMIT_MB", "")

	cfg := Load()

	if cfg.ConfigDir != "/config" {
		t.Fatalf("ConfigDir = %q, want /config", cfg.ConfigDir)
	}
	if cfg.LibraryDir != "/library" {
		t.Fatalf("LibraryDir = %q, want /library", cfg.LibraryDir)
	}
	if cfg.Addr != ":8080" {
		t.Fatalf("Addr = %q, want :8080", cfg.Addr)
	}
	if cfg.APIToken != "" {
		t.Fatalf("APIToken = %q, want empty", cfg.APIToken)
	}
	if cfg.MemoryLimitMB != 768 {
		t.Fatalf("MemoryLimitMB = %d, want 768", cfg.MemoryLimitMB)
	}
}

func TestLoadUsesEnvironmentOverrides(t *testing.T) {
	t.Setenv("FOLIOSPACE_CONFIG_DIR", "/tmp/config")
	t.Setenv("FOLIOSPACE_LIBRARY_DIR", "/tmp/library")
	t.Setenv("FOLIOSPACE_ADDR", "127.0.0.1:9090")
	t.Setenv("FOLIOSPACE_API_TOKEN", "secret")
	t.Setenv("FOLIOSPACE_MEMORY_LIMIT_MB", "1024")

	cfg := Load()

	if cfg.ConfigDir != "/tmp/config" {
		t.Fatalf("ConfigDir = %q, want /tmp/config", cfg.ConfigDir)
	}
	if cfg.LibraryDir != "/tmp/library" {
		t.Fatalf("LibraryDir = %q, want /tmp/library", cfg.LibraryDir)
	}
	if cfg.Addr != "127.0.0.1:9090" {
		t.Fatalf("Addr = %q, want 127.0.0.1:9090", cfg.Addr)
	}
	if cfg.APIToken != "secret" {
		t.Fatalf("APIToken = %q, want secret", cfg.APIToken)
	}
	if cfg.MemoryLimitMB != 1024 {
		t.Fatalf("MemoryLimitMB = %d, want 1024", cfg.MemoryLimitMB)
	}
}
