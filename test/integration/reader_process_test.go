//go:build integration

package integration

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
)

type readerProcess struct {
	address           string
	cmd               *exec.Cmd
	done              chan error
	pollingContended  atomic.Bool
	deliveryExhausted atomic.Bool
}

func buildReader(t *testing.T) string {
	t.Helper()
	name := "reader"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	exe := filepath.Join(t.TempDir(), name)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", "build", "-buildvcs=false", "-o", exe, "./cmd/reader")
	cmd.Dir = "../.."
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("building Reader: %v\n%s", err, output)
	}
	return exe
}

func startReader(t *testing.T, exe string, settings map[string]string) *readerProcess {
	t.Helper()
	cmd := exec.Command(exe, "-mode=agent")
	cmd.Dir = t.TempDir()
	cmd.Env = os.Environ()
	for key, value := range settings {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	logFile, err := os.CreateTemp(cmd.Dir, "reader-*.log")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if t.Failed() {
			if output, err := os.ReadFile(logFile.Name()); err == nil {
				t.Logf("Reader logs:\n%s", output)
			}
		}
	})
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		logFile.Close()
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		logFile.Close()
		t.Fatal(err)
	}
	process := &readerProcess{cmd: cmd, done: make(chan error, 1)}
	t.Cleanup(func() { process.stop(t) })
	listening := make(chan string, 1)
	go func() {
		defer close(listening)
		defer logFile.Close()
		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			line := scanner.Bytes()
			_, _ = fmt.Fprintln(logFile, string(line))
			var event struct {
				Message string `json:"msg"`
				Address string `json:"address"`
				Error   string `json:"error"`
			}
			if json.Unmarshal(line, &event) == nil {
				switch event.Message {
				case "status API listening":
					listening <- event.Address
				case "another Reader owns polling":
					process.pollingContended.Store(true)
				case "poll cycle failed":
					if strings.Contains(event.Error, "delivery: exhausted 3 attempts") {
						process.deliveryExhausted.Store(true)
					}
				}
			}
		}
		process.done <- cmd.Wait()
	}()
	select {
	case process.address = <-listening:
		if process.address == "" {
			t.Fatal("Reader exited before opening the Status API")
		}
	case <-time.After(30 * time.Second):
		t.Fatal("Reader did not start within 30 seconds")
	}
	return process
}

func (p *readerProcess) stop(t *testing.T) {
	t.Helper()
	if p.cmd == nil {
		return
	}
	_ = p.cmd.Process.Kill()
	select {
	case <-p.done:
		p.cmd = nil
	case <-time.After(10 * time.Second):
		t.Error("Reader did not exit after interruption")
	}
}

type fileStatus struct {
	ID       string `json:"file_id"`
	State    string `json:"state"`
	Lines    int    `json:"lines_read"`
	Accepted int    `json:"lines_accepted"`
}

func checkStatus(t *testing.T, address, key string, wantCode int) []fileStatus {
	t.Helper()
	req, err := http.NewRequest("POST", "http://"+address+"/status", strings.NewReader(`{"file_ids":["e2e-test-file.txt","no-match.txt"]}`))
	if err != nil {
		t.Fatal(err)
	}
	if key != "" {
		req.Header.Set("X-API-Key", key)
	}
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != wantCode {
		t.Fatalf("Status API returned %d, want %d", resp.StatusCode, wantCode)
	}
	if wantCode != http.StatusOK {
		return nil
	}
	var result struct {
		Files []fileStatus `json:"files"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	return result.Files
}

func eventually(t *testing.T, check func() bool) {
	t.Helper()
	deadline := time.Now().Add(25 * time.Second)
	for !check() {
		if time.Now().After(deadline) {
			t.Fatal("expected workflow state was not reached")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func assertMessages(t *testing.T, channel *amqp.Channel, records, urls string, modifiedAt time.Time) {
	t.Helper()
	want := map[string]string{
		"https://example.com": "alice|password123",
		"http://test.org":     "bob|hunter2pass",
	}
	for _, queue := range []string{records, urls} {
		count := make(map[string]int)
		for {
			msg, ok, err := channel.Get(queue, true)
			if err != nil {
				t.Fatal(err)
			}
			if !ok {
				break
			}
			var body map[string]any
			if err := json.Unmarshal(msg.Body, &body); err != nil {
				t.Fatal(err)
			}
			url, _ := body["url"].(string)
			if want[url] == "" {
				t.Fatalf("unexpected URL in %s: %q", queue, url)
			}
			count[url]++
			domainName := strings.SplitN(url, "://", 2)[1]
			if body["domain"] != domainName || body["subdomain"] != "" || body["suffix"] != domainName[strings.LastIndex(domainName, ".")+1:] {
				t.Fatalf("unexpected URL metadata for %s", url)
			}
			if queue == records {
				fields := strings.SplitN(want[url], "|", 2)
				hash := fmt.Sprintf("%x", sha256.Sum256([]byte(url+"|"+want[url])))
				if len(body) != 10 || body["record_hash"] != hash || body["username"] != fields[0] ||
					body["password"] != fields[1] || body["port"] != "" || body["created_at"] == nil || body["modified_date"] == nil {
					t.Fatalf("unexpected record for %s", url)
				}
				actual, err := time.Parse(time.RFC3339Nano, fmt.Sprint(body["modified_date"]))
				if err != nil || !actual.Equal(modifiedAt) {
					t.Fatalf("modified_date for %s: %v, want %s", url, body["modified_date"], modifiedAt)
				}
			} else if len(body) != 5 || body["priority"] != float64(5) || msg.Priority != 5 {
				t.Fatalf("unexpected URL payload or AMQP priority for %s: %s, %d", url, msg.Body, msg.Priority)
			}
		}
		if count["https://example.com"] != 1 || count["http://test.org"] < 2 {
			t.Fatalf("%s replayed the wrong records: %v", queue, count)
		}
	}
}
