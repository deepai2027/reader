package config

import (
	"strings"
	"testing"
)

func lookupFromMap(m map[string]string) envLookup {
	return func(key string) (string, bool) {
		v, ok := m[key]
		return v, ok
	}
}

func validEnv() map[string]string {
	return map[string]string{
		"BATCH_SIZE":          "500",
		"MIN_USERNAME_LENGTH": "1",
		"MAX_USERNAME_LENGTH": "254",
		"MIN_PASSWORD_LENGTH": "1",
		"MAX_PASSWORD_LENGTH": "128",
		"RETRY_MAX_ATTEMPTS":  "5",
		"RETRY_BASE_DELAY_MS": "200",
		"LOG_LEVEL":           "info",
	}
}

func TestLoad_Valid(t *testing.T) {
	cfg, err := load(lookupFromMap(validEnv()))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.BatchSize != 500 {
		t.Errorf("BatchSize = %d, want 500", cfg.BatchSize)
	}
	if cfg.LogLevel != "info" {
		t.Errorf("LogLevel = %q, want %q", cfg.LogLevel, "info")
	}
}

func TestLoad_DefaultsLogLevel(t *testing.T) {
	env := validEnv()
	delete(env, "LOG_LEVEL")
	cfg, err := load(lookupFromMap(env))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.LogLevel != "info" {
		t.Errorf("LogLevel = %q, want default %q", cfg.LogLevel, "info")
	}
}

func TestLoad_MissingRequired(t *testing.T) {
	env := validEnv()
	delete(env, "BATCH_SIZE")
	_, err := load(lookupFromMap(env))
	if err == nil {
		t.Fatal("expected error for missing BATCH_SIZE, got nil")
	}
	if !strings.Contains(err.Error(), "BATCH_SIZE is required") {
		t.Errorf("error = %v, want mention of \"BATCH_SIZE is required\"", err)
	}
}

func TestLoad_NonIntegerValue(t *testing.T) {
	env := validEnv()
	env["BATCH_SIZE"] = "not-a-number"
	_, err := load(lookupFromMap(env))
	if err == nil {
		t.Fatal("expected error for non-integer BATCH_SIZE, got nil")
	}
	if !strings.Contains(err.Error(), "BATCH_SIZE must be an integer") {
		t.Errorf("error = %v, want mention of \"BATCH_SIZE must be an integer\"", err)
	}
}

func TestLoad_InvertedUsernameRange(t *testing.T) {
	env := validEnv()
	env["MIN_USERNAME_LENGTH"] = "5"
	env["MAX_USERNAME_LENGTH"] = "0"
	_, err := load(lookupFromMap(env))
	if err == nil {
		t.Fatal("expected error for inverted username length range, got nil")
	}
	if !strings.Contains(err.Error(), "MIN_USERNAME_LENGTH/MAX_USERNAME_LENGTH") {
		t.Errorf("error = %v, want mention of username length bounds", err)
	}
}

func TestLoad_InvalidLogLevel(t *testing.T) {
	env := validEnv()
	env["LOG_LEVEL"] = "verbose"
	_, err := load(lookupFromMap(env))
	if err == nil {
		t.Fatal("expected error for invalid LOG_LEVEL, got nil")
	}
	if !strings.Contains(err.Error(), "LOG_LEVEL") {
		t.Errorf("error = %v, want mention of LOG_LEVEL", err)
	}
}

func TestLoad_AggregatesMultipleErrors(t *testing.T) {
	_, err := load(lookupFromMap(map[string]string{}))
	if err == nil {
		t.Fatal("expected error for empty environment, got nil")
	}
	for _, key := range []string{"BATCH_SIZE", "MIN_USERNAME_LENGTH", "RETRY_MAX_ATTEMPTS"} {
		if !strings.Contains(err.Error(), key) {
			t.Errorf("error does not mention %s: %v", key, err)
		}
	}
}

func TestLoad_BatchSizeBounds(t *testing.T) {
	for _, tt := range []struct {
		value string
		valid bool
	}{
		{"0", false},
		{"1", true},
		{"1000", true},
		{"1001", false},
	} {
		t.Run(tt.value, func(t *testing.T) {
			env := validEnv()
			env["BATCH_SIZE"] = tt.value
			_, err := load(lookupFromMap(env))
			if tt.valid && err != nil {
				t.Fatal(err)
			}
			if !tt.valid && (err == nil || !strings.Contains(err.Error(), "BATCH_SIZE")) {
				t.Fatalf("wanted BATCH_SIZE error, got %v", err)
			}
		})
	}
}
