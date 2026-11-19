package statusapi

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/yourorg/reader/internal/domain"
)

type lookupFunc func(context.Context, []string) (map[string]domain.FileState, error)

func (f lookupFunc) LookupStatuses(ctx context.Context, ids []string) (map[string]domain.FileState, error) {
	return f(ctx, ids)
}

func TestStatus(t *testing.T) {
	const validBody = `{"file_ids":["done","missing","reading","done"]}`
	tests := []struct {
		name, method, key, body string
		code                    int
		wantBody                string
		ids                     []string
		lookupError             error
	}{
		{"success", "POST", "test-key", validBody, 200, `{"files":[{"file_id":"done","state":"done","lines_read":0,"lines_accepted":0},{"file_id":"reading","state":"reading","lines_read":120,"lines_accepted":100}]}`, []string{"done", "missing", "reading"}, nil},
		{"unknown", "POST", "test-key", `{"file_ids":["missing"]}`, 200, `{"files":[]}`, []string{"missing"}, nil},
		{"no key", "POST", "", validBody, 401, "unauthorized", nil, nil},
		{"invalid key precedes invalid body", "POST", "wrong-key", "{", 401, "unauthorized", nil, nil},
		{"malformed JSON", "POST", "test-key", "{", 400, "invalid JSON request", nil, nil},
		{"trailing JSON", "POST", "test-key", validBody + " {}", 400, "invalid JSON request", nil, nil},
		{"missing array", "POST", "test-key", "{}", 400, "file_ids must contain 1 to 500 identifiers", nil, nil},
		{"empty array", "POST", "test-key", `{"file_ids":[]}`, 400, "file_ids must contain 1 to 500 identifiers", nil, nil},
		{"blank ID", "POST", "test-key", `{"file_ids":[" "]}`, 400, "invalid file identifier", nil, nil},
		{"too many IDs", "POST", "test-key", `{"file_ids":[` + strings.Repeat(`"a",`, 500) + `"a"]}`, 400, "file_ids must contain 1 to 500 identifiers", nil, nil},
		{"oversized body", "POST", "test-key", validBody + strings.Repeat(" ", maxBodyBytes), 413, "request body exceeds 64 KiB", nil, nil},
		{"wrong method", "GET", "test-key", "", 405, "Method Not Allowed", nil, nil},
		{"database error", "POST", "test-key", `{"file_ids":["missing"]}`, 500, "could not retrieve file statuses", []string{"missing"}, errors.New("private connection details")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var queried []string
			store := lookupFunc(func(_ context.Context, ids []string) (map[string]domain.FileState, error) {
				queried = ids
				states := map[string]domain.FileState{}
				for _, id := range ids {
					switch id {
					case "done":
						states[id] = domain.FileState{Status: domain.FileStatusDone}
					case "reading":
						states[id] = domain.FileState{Status: domain.FileStatusReading, CurrentLine: 120, MatchedCount: 100}
					}
				}
				return states, tt.lookupError
			})
			var logs strings.Builder
			handler := New(store, "test-key", slog.New(slog.NewTextHandler(&logs, nil)))
			req := httptest.NewRequest(tt.method, "/status", strings.NewReader(tt.body))
			if tt.key != "" {
				req.Header.Set("X-API-Key", tt.key)
			}
			res := httptest.NewRecorder()
			handler.ServeHTTP(res, req)

			if res.Code != tt.code || strings.TrimSpace(res.Body.String()) != tt.wantBody {
				t.Fatalf("got HTTP %d with %q, want HTTP %d with %q", res.Code, res.Body.String(), tt.code, tt.wantBody)
			}
			if !slices.Equal(queried, tt.ids) {
				t.Fatalf("queried %q, want %q", queried, tt.ids)
			}
			if tt.code == http.StatusOK && res.Header().Get("Content-Type") != "application/json" {
				t.Errorf("content type = %q", res.Header().Get("Content-Type"))
			}
			if strings.Contains(logs.String(), "test-key") || strings.Contains(logs.String(), "wrong-key") {
				t.Fatal("request log contains API key")
			}
		})
	}
}

func TestStatusRejectsMultipleAPIKeys(t *testing.T) {
	store := lookupFunc(func(context.Context, []string) (map[string]domain.FileState, error) {
		t.Fatal("unexpected lookup")
		return nil, nil
	})
	req := httptest.NewRequest(http.MethodPost, "/status", strings.NewReader(`{"file_ids":["file"]}`))
	req.Header.Add("X-API-Key", "test-key")
	req.Header.Add("X-API-Key", "wrong-key")
	res := httptest.NewRecorder()
	New(store, "test-key", slog.New(slog.NewTextHandler(io.Discard, nil))).ServeHTTP(res, req)
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("got HTTP %d, want 401", res.Code)
	}
}
