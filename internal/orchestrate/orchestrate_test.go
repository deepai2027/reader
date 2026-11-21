package orchestrate

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/yourorg/reader/internal/agent"
	"github.com/yourorg/reader/internal/config"
	"github.com/yourorg/reader/internal/domain"
	"github.com/yourorg/reader/internal/ports/stub"
	"github.com/yourorg/reader/internal/processing/pipeline"
)

func testDeps(progress *stub.ProgressStore) pipeline.Deps {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return pipeline.Deps{
		Progress:  progress,
		Publisher: stub.NewMessagePublisher(logger),
		Regexes:   stub.NewRegexSource(),
		Files:     stub.NewFileSource(),
		Config: config.Config{
			BatchSize:         10,
			MinUsernameLength: 1,
			MaxUsernameLength: 50,
			MinPasswordLength: 1,
			MaxPasswordLength: 50,
			RetryMaxAttempts:  1,
			RetryBaseDelayMS:  1,
			LogLevel:          "error",
		},
		Logger: logger,
	}
}

func TestPollOnce_ResumesBeforePollingAgent(t *testing.T) {
	progress := stub.NewProgressStore()
	ctx := context.Background()
	now := time.Now().UTC()
	if err := progress.Save(ctx, domain.FileState{
		FileID: "../processing/testdata/well_formed.txt", Status: domain.FileStatusReading,
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatalf("setup: %v", err)
	}

	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := agent.New(&http.Client{Timeout: 2 * time.Second}, server.URL, "k")
	processed, err := PollOnce(ctx, client, progress, nil, testDeps(progress))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !processed {
		t.Error("expected PollOnce to continue the saved assignment")
	}
	if called {
		t.Error("expected the Agent not to be polled at all while a file is already reading")
	}
}

func TestPollOnce_NoFileAvailable(t *testing.T) {
	progress := stub.NewProgressStore()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := agent.New(&http.Client{Timeout: 2 * time.Second}, server.URL, "k")
	processed, err := PollOnce(context.Background(), client, progress, nil, testDeps(progress))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if processed {
		t.Error("expected nothing processed when the Agent has no file available")
	}
}

func TestPollOnce_ProcessesAnAvailableFile(t *testing.T) {
	progress := stub.NewProgressStore()
	const fileRef = "../processing/testdata/well_formed.txt"

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(agent.PolledFile{FileID: fileRef, Priority: 3})
	}))
	defer server.Close()

	client := agent.New(&http.Client{Timeout: 2 * time.Second}, server.URL, "k")
	processed, err := PollOnce(context.Background(), client, progress, nil, testDeps(progress))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !processed {
		t.Error("expected PollOnce to process the available file")
	}

	state, ok, err := progress.Load(context.Background(), fileRef)
	if err != nil || !ok {
		t.Fatalf("expected a saved FileState, ok=%v err=%v", ok, err)
	}
	if state.MatchedCount != 3 {
		t.Errorf("MatchedCount = %d, want 3", state.MatchedCount)
	}
	if state.Priority != 3 {
		t.Errorf("Priority = %d, want 3 (carried from the Agent's response)", state.Priority)
	}
}
