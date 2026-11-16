package config

import (
	"strings"
	"testing"
)

func validInfraEnv() map[string]string {
	return map[string]string{
		"DATABASE_DSN":                "postgres://user:pass@localhost:5432/reader",
		"MINIO_ENDPOINT":              "localhost:9000",
		"MINIO_ACCESS_KEY":            "minioadmin",
		"MINIO_SECRET_KEY":            "minioadmin",
		"MINIO_BUCKET":                "reader-files",
		"RABBITMQ_URL":                "amqp://guest:guest@localhost:5672/",
		"AGENT_BASE_URL":              "https://agent.internal",
		"AGENT_API_KEY":               "test-key",
		"AGENT_POLL_INTERVAL_SECONDS": "10",
	}
}

func TestLoadInfra_Valid(t *testing.T) {
	cfg, err := loadInfra(lookupFromMap(validInfraEnv()))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.AgentPollIntervalSeconds != 10 {
		t.Errorf("AgentPollIntervalSeconds = %d, want 10", cfg.AgentPollIntervalSeconds)
	}
	if cfg.MinIOUseSSL != false {
		t.Errorf("MinIOUseSSL = %v, want false (default)", cfg.MinIOUseSSL)
	}
}

func TestLoadInfra_TopicsDefaultWhenUnset(t *testing.T) {
	cfg, err := loadInfra(lookupFromMap(validInfraEnv()))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.RabbitMQFullRecordTopic != "reader.full_record" {
		t.Errorf("RabbitMQFullRecordTopic = %q, want default", cfg.RabbitMQFullRecordTopic)
	}
	if cfg.RabbitMQUniqueURLTopic != "reader.unique_url" {
		t.Errorf("RabbitMQUniqueURLTopic = %q, want default", cfg.RabbitMQUniqueURLTopic)
	}
}

func TestLoadInfra_MissingRequired(t *testing.T) {
	env := validInfraEnv()
	delete(env, "DATABASE_DSN")
	_, err := loadInfra(lookupFromMap(env))
	if err == nil {
		t.Fatal("expected error for missing DATABASE_DSN")
	}
	if !strings.Contains(err.Error(), "DATABASE_DSN is required") {
		t.Errorf("error = %v, want mention of DATABASE_DSN", err)
	}
}

func TestLoadInfra_InvalidPollInterval(t *testing.T) {
	env := validInfraEnv()
	env["AGENT_POLL_INTERVAL_SECONDS"] = "0"
	_, err := loadInfra(lookupFromMap(env))
	if err == nil {
		t.Fatal("expected error for a zero poll interval")
	}
}

func TestLoadInfra_MinIOUseSSLParsesTrue(t *testing.T) {
	env := validInfraEnv()
	env["MINIO_USE_SSL"] = "true"
	cfg, err := loadInfra(lookupFromMap(env))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.MinIOUseSSL {
		t.Error("expected MinIOUseSSL = true")
	}
}
