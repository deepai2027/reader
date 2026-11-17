package agent

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestPoll_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("unexpected method %q", r.Method)
		}
		if r.URL.Path != "/api/reader/new_file/" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		if r.Header.Get("X-API-Key") != "test-key" {
			t.Errorf("missing/incorrect X-API-Key header: %q", r.Header.Get("X-API-Key"))
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(PolledFile{FileID: "abc123", Priority: 5})
	}))
	defer server.Close()

	client := New(&http.Client{Timeout: 5 * time.Second}, server.URL, "test-key")
	file, err := client.Poll(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if file.FileID != "abc123" || file.Priority != 5 {
		t.Errorf("got %+v, want FileID=abc123 Priority=5", file)
	}
}

func TestPoll_NoFileAvailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := New(&http.Client{Timeout: 5 * time.Second}, server.URL, "test-key")
	_, err := client.Poll(context.Background())
	if !errors.Is(err, ErrNoFileAvailable) {
		t.Fatalf("got %v, want ErrNoFileAvailable", err)
	}
}

func TestPoll_FileInReadingIsNotAHardError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
	}))
	defer server.Close()

	client := New(&http.Client{Timeout: 5 * time.Second}, server.URL, "test-key")
	_, err := client.Poll(context.Background())
	if !errors.Is(err, ErrFileInReading) {
		t.Fatalf("got %v, want ErrFileInReading", err)
	}
}

func TestPoll_UnexpectedStatusIsAnError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := New(&http.Client{Timeout: 5 * time.Second}, server.URL, "test-key")
	_, err := client.Poll(context.Background())
	if err == nil {
		t.Fatal("expected an error for an unexpected status code")
	}
	if errors.Is(err, ErrNoFileAvailable) || errors.Is(err, ErrFileInReading) {
		t.Error("a genuine server error should not be mistaken for an expected wait condition")
	}
}

func TestPoll_MalformedJSONIsAnError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("not json"))
	}))
	defer server.Close()

	client := New(&http.Client{Timeout: 5 * time.Second}, server.URL, "test-key")
	_, err := client.Poll(context.Background())
	if err == nil {
		t.Fatal("expected an error for a malformed response body")
	}
}
