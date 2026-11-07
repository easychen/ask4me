package main

import "testing"

func TestLoadConfigFromEnv(t *testing.T) {
	t.Setenv("ASK4ME_BASE_URL", "http://127.0.0.1:8080")
	t.Setenv("ASK4ME_API_KEY", "env-test-key")
	t.Setenv("ASK4ME_SQLITE_PATH", ":memory:")
	t.Setenv("ASK4ME_MULTI_USER_ENABLED", "false")
	t.Setenv("ASK4ME_E2EE_ENABLED", "false")

	cfg, err := loadConfigFromEnv()
	if err != nil {
		t.Fatalf("loadConfigFromEnv: %v", err)
	}
	if cfg.BaseURL != "http://127.0.0.1:8080" || cfg.APIKey != "env-test-key" {
		t.Fatalf("unexpected config: %+v", cfg)
	}
}

func TestLoadConfigAutoFallsBackToEnv(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("ASK4ME_BASE_URL", "http://127.0.0.1:8080")
	t.Setenv("ASK4ME_API_KEY", "env-fallback-key")
	t.Setenv("ASK4ME_SQLITE_PATH", ":memory:")
	t.Setenv("ASK4ME_MULTI_USER_ENABLED", "false")
	t.Setenv("ASK4ME_E2EE_ENABLED", "false")

	cfg, used, err := loadConfigAuto("")
	if err != nil {
		t.Fatalf("loadConfigAuto: %v", err)
	}
	if used != "env" {
		t.Fatalf("used = %q, want env", used)
	}
	if cfg.APIKey != "env-fallback-key" {
		t.Fatalf("api key = %q", cfg.APIKey)
	}
}
