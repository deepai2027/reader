package pipeline

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/yourorg/reader/internal/config"
	"github.com/yourorg/reader/internal/domain"
	"github.com/yourorg/reader/internal/ports/stub"
	"github.com/yourorg/reader/internal/processing/regexengine"
)

type benchmarkPatterns []domain.RegexPattern

func (p benchmarkPatterns) ActivePatterns(context.Context) ([]domain.RegexPattern, error) {
	return p, nil
}

func seededPatterns(t testing.TB) benchmarkPatterns {
	t.Helper()
	sql, err := os.ReadFile(filepath.Join("..", "..", "..", "migrations", "0002_seed_regex.up.sql"))
	if err != nil {
		t.Fatal(err)
	}
	rows := regexp.MustCompile(`(?i)\(\s*'((?:[^']|'')*)'\s*,\s*'((?:[^']|'')*)'\s*,\s*(TRUE|FALSE)\s*\)`)
	var patterns benchmarkPatterns
	for i, row := range rows.FindAllStringSubmatch(string(sql), -1) {
		if !strings.EqualFold(row[3], "true") {
			continue
		}
		pattern := strings.ReplaceAll(row[2], "''", "'")
		if _, err := regexengine.Compile(pattern); err != nil {
			t.Fatal(err)
		}
		patterns = append(patterns, domain.RegexPattern{
			ID: int64(i + 1), Pattern: pattern, IsActive: true,
			Name: strings.ReplaceAll(row[1], "''", "'"),
		})
	}
	if len(patterns) == 0 {
		t.Fatal("no active patterns in seed migration")
	}
	return patterns
}

type countingPublisher struct {
	records int64
	urls    int64
}

func (p *countingPublisher) PublishRecord(context.Context, domain.ValidatedRecord) error {
	p.records++
	return nil
}

func (p *countingPublisher) PublishUniqueURL(context.Context, domain.UniqueURLMessage) error {
	p.urls++
	return nil
}

// TestGeneratePerformanceFixture writes a deterministic fixture to a new file.
// Each pair contains one valid synthetic record and one invalid long line.
func TestGeneratePerformanceFixture(t *testing.T) {
	path := os.Getenv("READER_BENCH_GENERATE")
	if path == "" {
		t.Skip("set READER_BENCH_GENERATE to a new fixture path")
	}
	size, err := strconv.ParseInt(os.Getenv("READER_BENCH_BYTES"), 10, 64)
	if err != nil || size < 1 {
		t.Fatal("READER_BENCH_BYTES must be positive")
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	writer := bufio.NewWriterSize(file, 1<<20)
	padding := strings.Repeat("a", 640)
	invalid := " " + strings.Repeat("?", 8190) + "\n"
	distinct := os.Getenv("READER_BENCH_DISTINCT") == "1"
	var count, written int64
	for written < size {
		id := int64(0)
		if distinct {
			id = count
		}
		n, err := fmt.Fprintf(writer, "https://fixture.example.test/%s/%016x:sample-user:sample-password\n%s", padding, id, invalid)
		if err != nil {
			t.Fatal(err)
		}
		written += int64(n)
		count++
	}
	if err := writer.Flush(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	t.Logf("bytes=%d pairs=%d distinct=%t", written, count, distinct)
}

func TestPipelinePerformance(t *testing.T) {
	path := os.Getenv("READER_BENCH_FILE")
	if path == "" {
		t.Skip("set READER_BENCH_FILE to a generated fixture")
	}
	size, err := strconv.Atoi(os.Getenv("READER_BENCH_BATCH"))
	if err != nil || size < 1 {
		t.Fatal("READER_BENCH_BATCH must be positive")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	const pairSize = len("https://fixture.example.test/") + 640 + len("/0000000000000000:sample-user:sample-password\n") + 8192
	if info.Size() == 0 || info.Size()%int64(pairSize) != 0 {
		t.Fatal("fixture has unexpected size")
	}
	pairs := info.Size() / int64(pairSize)
	wantURLs := int64(1)
	if os.Getenv("READER_BENCH_DISTINCT") == "1" {
		wantURLs = pairs
	}

	publisher := &countingPublisher{}
	progress := stub.NewProgressStore()
	deps := Deps{
		Progress: progress, Publisher: publisher, Regexes: seededPatterns(t), Files: stub.NewFileSource(),
		Config: config.Config{
			BatchSize: size, MinUsernameLength: 2, MaxUsernameLength: 50,
			MinPasswordLength: 6, MaxPasswordLength: 50,
		},
	}
	runtime.GC()
	var startMem, endMem runtime.MemStats
	runtime.ReadMemStats(&startMem)
	start := time.Now()
	stats, err := Run(t.Context(), "performance", path, 7, deps)
	elapsed := time.Since(start)
	runtime.ReadMemStats(&endMem)
	if err != nil {
		t.Fatal(err)
	}
	state, found, err := progress.Load(t.Context(), "performance")
	if err != nil || !found || state.Status != domain.FileStatusDone || state.CurrentLine != pairs*2 ||
		stats.MatchedCount != pairs || stats.InvalidCount != pairs || stats.UniqueURLCount != wantURLs ||
		publisher.records != pairs || publisher.urls != wantURLs {
		t.Fatalf("unexpected result: stats=%+v state=%+v publisher=%+v err=%v", stats, state, publisher, err)
	}
	t.Logf("bytes=%d lines=%d batch=%d elapsed=%s MiB/s=%.2f lines/s=%.0f heap-at-return-MiB=%.2f allocations=%d",
		info.Size(), pairs*2, size, elapsed, float64(info.Size())/(1<<20)/elapsed.Seconds(),
		float64(pairs*2)/elapsed.Seconds(), float64(endMem.HeapAlloc)/(1<<20), endMem.Mallocs-startMem.Mallocs)
}

func BenchmarkSeededRegex(b *testing.B) {
	for _, pattern := range seededPatterns(b) {
		compiled, err := regexengine.Compile(pattern.Pattern)
		if err != nil {
			b.Fatal(err)
		}
		for _, length := range []int{1024, 16 << 10, 256 << 10, (1 << 20) - 1} {
			for _, suffix := range []struct {
				name    string
				tail    string
				matches bool
			}{
				{"match", ":user:secret", true},
				{"near-miss", ":user:secre ", false},
				{"non-match", " ", false},
			} {
				b.Run(fmt.Sprintf("%s/%s/%d", pattern.Name, suffix.name, length), func(b *testing.B) {
					const prefix = "https://fixture.example.test/"
					line := prefix + strings.Repeat("a", length-len(prefix)-len(suffix.tail)) + suffix.tail
					if compiled.MatchString(line) != suffix.matches {
						b.Fatal("fixture does not exercise the intended match path")
					}
					b.SetBytes(int64(len(line)))
					b.ReportAllocs()
					for b.Loop() {
						compiled.MatchString(line)
					}
				})
			}
		}
	}
}
