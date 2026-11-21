//go:build integration

package integration

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func checkCompose(t *testing.T, settings map[string]string) {
	t.Helper()
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-Key") != settings["AGENT_API_KEY"] {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = io.WriteString(w, `{"file_id":"e2e-test-file.txt","priority":5}`)
	}))
	_ = server.Listener.Close()
	listener, err := net.Listen("tcp4", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	server.Listener = listener
	server.Start()
	defer server.Close()

	const containerHost = "host.docker.internal"
	_, agentPort, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	_, minioPort, err := net.SplitHostPort(settings["MINIO_ENDPOINT"])
	if err != nil {
		t.Fatal(err)
	}
	broker, err := url.Parse(settings["RABBITMQ_URL"])
	if err != nil {
		t.Fatal(err)
	}
	broker.Host = net.JoinHostPort(containerHost, broker.Port())

	dir := t.TempDir()
	envFile := filepath.Join(dir, "reader.env")
	var env strings.Builder
	for key, value := range settings {
		switch key {
		case "DATABASE_DSN", "READER_WORK_DIR", "READER_API_ADDR":
			continue
		case "AGENT_BASE_URL":
			value = "http://" + net.JoinHostPort(containerHost, agentPort)
		case "MINIO_ENDPOINT":
			value = net.JoinHostPort(containerHost, minioPort)
		case "RABBITMQ_URL":
			value = broker.String()
		}
		fmt.Fprintf(&env, "%s=%s\n", key, value)
	}
	if err := os.WriteFile(envFile, []byte(env.String()), 0600); err != nil {
		t.Fatal(err)
	}
	override := filepath.Join(dir, "compose.override.yml")
	if err := os.WriteFile(override, []byte("services:\n  reader:\n    extra_hosts:\n      - host.docker.internal:host-gateway\n"), 0600); err != nil {
		t.Fatal(err)
	}
	project := fmt.Sprintf("reader-smoke-%d", time.Now().UnixNano())
	compose := func(timeout time.Duration, args ...string) ([]byte, error) {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		base := []string{"compose", "-p", project, "-f", "docker-compose.yml", "-f", override}
		cmd := exec.CommandContext(ctx, "docker", append(base, args...)...)
		cmd.Dir = "../.."
		cmd.Env = append(os.Environ(), "READER_ENV_FILE="+envFile, "READER_HTTP_PORT=0", "READER_POSTGRES_PORT=0")
		return cmd.CombinedOutput()
	}
	run := func(timeout time.Duration, args ...string) string {
		t.Helper()
		output, err := compose(timeout, args...)
		if err != nil {
			t.Fatalf("Compose %v: %v\n%s", args, err, output)
		}
		return strings.TrimSpace(string(output))
	}
	if services := strings.Fields(run(30*time.Second, "config", "--services")); len(services) != 2 ||
		!((services[0] == "postgres" && services[1] == "reader") || (services[0] == "reader" && services[1] == "postgres")) {
		t.Fatalf("unexpected Compose services: %v", services)
	}
	defer func() {
		if t.Failed() {
			logs, _ := compose(30*time.Second, "logs", "--no-color")
			t.Logf("Compose logs:\n%s", logs)
		}
		if output, err := compose(time.Minute, "down", "--volumes", "--rmi", "local"); err != nil {
			t.Errorf("removing the isolated Compose project: %v\n%s", err, output)
		}
	}()
	run(6*time.Minute, "up", "--build", "-d")
	_, port, err := net.SplitHostPort(run(30*time.Second, "port", "reader", "8080"))
	if err != nil {
		t.Fatal(err)
	}
	address := net.JoinHostPort("127.0.0.1", port)
	eventually(t, func() bool {
		conn, err := net.DialTimeout("tcp", address, 250*time.Millisecond)
		if err != nil {
			return false
		}
		_ = conn.Close()
		return true
	})
	eventually(t, func() bool {
		status := checkStatus(t, address, settings["READER_API_KEY"], http.StatusOK)
		return len(status) == 1 && status[0] == (fileStatus{"e2e-test-file.txt", "done", 3, 2})
	})
}
