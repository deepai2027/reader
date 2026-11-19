package statusapi

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/yourorg/reader/internal/domain"
)

const (
	maxBodyBytes = 64 * 1024
	maxFileIDs   = 500
)

type StatusLookup interface {
	LookupStatuses(context.Context, []string) (map[string]domain.FileState, error)
}

type fileStatus struct {
	FileID        string            `json:"file_id"`
	State         domain.FileStatus `json:"state"`
	LinesRead     int64             `json:"lines_read"`
	LinesAccepted int64             `json:"lines_accepted"`
}

type statusResponse struct {
	Files []fileStatus `json:"files"`
}

func New(store StatusLookup, apiKey string, logger *slog.Logger) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("POST /status", requireAPIKey(apiKey, logger, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		result, code, message := lookup(w, r, store)
		logger.InfoContext(r.Context(), "status API request", "status", code)
		if code != http.StatusOK {
			http.Error(w, message, code)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(result); err != nil {
			logger.WarnContext(r.Context(), "could not write status API response")
		}
	})))
	return mux
}

func requireAPIKey(apiKey string, logger *slog.Logger, next http.Handler) http.Handler {
	keyHash := sha256.Sum256([]byte(apiKey))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		keys := r.Header.Values("X-API-Key")
		if len(keys) == 1 && keys[0] != "" {
			providedHash := sha256.Sum256([]byte(keys[0]))
			if subtle.ConstantTimeCompare(providedHash[:], keyHash[:]) == 1 {
				next.ServeHTTP(w, r)
				return
			}
		}
		logger.InfoContext(r.Context(), "status API request", "status", http.StatusUnauthorized)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	})
}

func lookup(w http.ResponseWriter, r *http.Request, store StatusLookup) (statusResponse, int, string) {
	var response statusResponse
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	var limitError *http.MaxBytesError
	if errors.As(err, &limitError) {
		return response, http.StatusRequestEntityTooLarge, "request body exceeds 64 KiB"
	}
	if err != nil {
		return response, http.StatusBadRequest, "could not read request body"
	}
	var request struct {
		FileIDs []string `json:"file_ids"`
	}
	if err := json.Unmarshal(body, &request); err != nil {
		return response, http.StatusBadRequest, "invalid JSON request"
	}
	if len(request.FileIDs) == 0 || len(request.FileIDs) > maxFileIDs {
		return response, http.StatusBadRequest, "file_ids must contain 1 to 500 identifiers"
	}

	seen := make(map[string]struct{}, len(request.FileIDs))
	ids := make([]string, 0, len(request.FileIDs))
	for _, id := range request.FileIDs {
		if strings.TrimSpace(id) == "" || strings.ContainsRune(id, 0) {
			return response, http.StatusBadRequest, "invalid file identifier"
		}
		if _, exists := seen[id]; !exists {
			seen[id] = struct{}{}
			ids = append(ids, id)
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	states, err := store.LookupStatuses(ctx, ids)
	if err != nil {
		return response, http.StatusInternalServerError, "could not retrieve file statuses"
	}
	response.Files = make([]fileStatus, 0, len(states))
	for _, id := range ids {
		if state, exists := states[id]; exists {
			response.Files = append(response.Files, fileStatus{
				FileID: id, State: state.Status,
				LinesRead: state.CurrentLine, LinesAccepted: state.MatchedCount,
			})
		}
	}
	return response, http.StatusOK, ""
}
