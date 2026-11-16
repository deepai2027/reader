// Package config loads and validates Reader's Phase 0-3 runtime
// configuration: the parameters the core processing pipeline needs.
// It deliberately does not yet cover PostgreSQL, MinIO, RabbitMQ, or
// Agent connection settings - those are added as each Final Phase
// step lands (see specs/roadmap.md).
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// Config holds Reader's core-pipeline configuration.
type Config struct {
	// BatchSize is the number of lines read and processed per batch
	// (README section 5.1). Must be a positive integer.
	BatchSize int

	// MinUsernameLength and MaxUsernameLength bound valid extracted
	// usernames (README section 7.2).
	MinUsernameLength int
	MaxUsernameLength int

	// MinPasswordLength and MaxPasswordLength bound valid extracted
	// passwords (README section 7.2).
	MinPasswordLength int
	MaxPasswordLength int

	// RetryMaxAttempts is the maximum number of retry attempts for a
	// transient publish failure before it is treated as a hard failure
	// (Phase 2 reliability logic).
	RetryMaxAttempts int

	// RetryBaseDelayMS is the base delay, in milliseconds, used for
	// exponential backoff between retry attempts.
	RetryBaseDelayMS int

	// LogLevel controls the verbosity of structured logging: one of
	// "debug", "info", "warn", "error".
	LogLevel string
}

// envLookup is a seam for testing - tests substitute a map-backed
// lookup instead of the real process environment, so config tests
// never depend on the developer machine's actual environment state.
type envLookup func(key string) (string, bool)

// Load reads and validates configuration from the process environment.
// It aggregates all validation errors into a single returned error
// rather than failing on the first problem, so a misconfigured
// environment can be fixed in one pass instead of one error at a time.
func Load() (Config, error) {
	return load(os.LookupEnv)
}

func load(lookup envLookup) (Config, error) {
	var errs []string

	cfg := Config{
		BatchSize:         mustInt(lookup, "BATCH_SIZE", &errs),
		MinUsernameLength: mustInt(lookup, "MIN_USERNAME_LENGTH", &errs),
		MaxUsernameLength: mustInt(lookup, "MAX_USERNAME_LENGTH", &errs),
		MinPasswordLength: mustInt(lookup, "MIN_PASSWORD_LENGTH", &errs),
		MaxPasswordLength: mustInt(lookup, "MAX_PASSWORD_LENGTH", &errs),
		RetryMaxAttempts:  mustInt(lookup, "RETRY_MAX_ATTEMPTS", &errs),
		RetryBaseDelayMS:  mustInt(lookup, "RETRY_BASE_DELAY_MS", &errs),
		LogLevel:          stringOrDefault(lookup, "LOG_LEVEL", "info"),
	}

	if cfg.BatchSize <= 0 {
		errs = append(errs, "BATCH_SIZE must be a positive integer")
	}
	if cfg.MinUsernameLength < 0 || cfg.MaxUsernameLength < cfg.MinUsernameLength {
		errs = append(errs, "MIN_USERNAME_LENGTH/MAX_USERNAME_LENGTH must satisfy 0 <= min <= max")
	}
	if cfg.MinPasswordLength < 0 || cfg.MaxPasswordLength < cfg.MinPasswordLength {
		errs = append(errs, "MIN_PASSWORD_LENGTH/MAX_PASSWORD_LENGTH must satisfy 0 <= min <= max")
	}
	if cfg.RetryMaxAttempts <= 0 {
		errs = append(errs, "RETRY_MAX_ATTEMPTS must be a positive integer")
	}
	if cfg.RetryBaseDelayMS <= 0 {
		errs = append(errs, "RETRY_BASE_DELAY_MS must be a positive integer")
	}
	if !isValidLogLevel(cfg.LogLevel) {
		errs = append(errs, fmt.Sprintf("LOG_LEVEL must be one of debug, info, warn, error (got %q)", cfg.LogLevel))
	}

	if len(errs) > 0 {
		return Config{}, fmt.Errorf("invalid configuration:\n  - %s", strings.Join(errs, "\n  - "))
	}

	return cfg, nil
}

func mustInt(lookup envLookup, key string, errs *[]string) int {
	raw, ok := lookup(key)
	if !ok || raw == "" {
		*errs = append(*errs, fmt.Sprintf("%s is required", key))
		return 0
	}
	val, err := strconv.Atoi(raw)
	if err != nil {
		*errs = append(*errs, fmt.Sprintf("%s must be an integer (got %q)", key, raw))
		return 0
	}
	return val
}

func stringOrDefault(lookup envLookup, key, def string) string {
	if raw, ok := lookup(key); ok && raw != "" {
		return raw
	}
	return def
}

func isValidLogLevel(level string) bool {
	switch level {
	case "debug", "info", "warn", "error":
		return true
	default:
		return false
	}
}

// InfraConfig holds the connection settings introduced by the MVP
// phase (specs/2026-11-10-mvp/plan.md Groups 5-8): PostgreSQL, MinIO,
// RabbitMQ, and the Agent API.
//
// This is loaded separately from Config, via LoadInfra rather than
// Load, so that anything exercising only the core pipeline - unit
// tests, the manual -file CLI trigger - never requires infrastructure
// it isn't using. Only cmd/reader's "-mode=agent" path calls
// LoadInfra.
type InfraConfig struct {
	DatabaseDSN string

	MinIOEndpoint  string
	MinIOAccessKey string
	MinIOSecretKey string
	MinIOBucket    string
	MinIOUseSSL    bool

	RabbitMQURL             string
	RabbitMQFullRecordTopic string
	RabbitMQUniqueURLTopic  string

	AgentBaseURL             string
	AgentAPIKey              string
	AgentPollIntervalSeconds int
}

// LoadInfra reads and validates InfraConfig from the process
// environment, aggregating all validation errors the same way Load
// does for Config.
func LoadInfra() (InfraConfig, error) {
	return loadInfra(os.LookupEnv)
}

func loadInfra(lookup envLookup) (InfraConfig, error) {
	var errs []string

	cfg := InfraConfig{
		DatabaseDSN: mustString(lookup, "DATABASE_DSN", &errs),

		MinIOEndpoint:  mustString(lookup, "MINIO_ENDPOINT", &errs),
		MinIOAccessKey: mustString(lookup, "MINIO_ACCESS_KEY", &errs),
		MinIOSecretKey: mustString(lookup, "MINIO_SECRET_KEY", &errs),
		MinIOBucket:    mustString(lookup, "MINIO_BUCKET", &errs),
		MinIOUseSSL:    boolOrDefault(lookup, "MINIO_USE_SSL", false),

		RabbitMQURL:             mustString(lookup, "RABBITMQ_URL", &errs),
		RabbitMQFullRecordTopic: stringOrDefault(lookup, "RABBITMQ_FULL_RECORD_TOPIC", "reader.full_record"),
		RabbitMQUniqueURLTopic:  stringOrDefault(lookup, "RABBITMQ_UNIQUE_URL_TOPIC", "reader.unique_url"),

		AgentBaseURL:             mustString(lookup, "AGENT_BASE_URL", &errs),
		AgentAPIKey:              mustString(lookup, "AGENT_API_KEY", &errs),
		AgentPollIntervalSeconds: mustInt(lookup, "AGENT_POLL_INTERVAL_SECONDS", &errs),
	}

	if cfg.AgentPollIntervalSeconds <= 0 {
		errs = append(errs, "AGENT_POLL_INTERVAL_SECONDS must be a positive integer")
	}

	if len(errs) > 0 {
		return InfraConfig{}, fmt.Errorf("invalid infrastructure configuration:\n  - %s", strings.Join(errs, "\n  - "))
	}
	return cfg, nil
}

func mustString(lookup envLookup, key string, errs *[]string) string {
	raw, ok := lookup(key)
	if !ok || raw == "" {
		*errs = append(*errs, fmt.Sprintf("%s is required", key))
	}
	return raw
}

func boolOrDefault(lookup envLookup, key string, def bool) bool {
	raw, ok := lookup(key)
	if !ok || raw == "" {
		return def
	}
	return raw == "true" || raw == "1" || raw == "yes"
}
