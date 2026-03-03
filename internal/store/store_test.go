package store

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func TestNewFSStore(t *testing.T) {
	tmp := t.TempDir()
	s, err := NewFSStore(tmp, testLogger())
	if err != nil {
		t.Fatalf("NewFSStore: %v", err)
	}
	if s.BaseDir() != tmp {
		t.Fatalf("expected base dir %s, got %s", tmp, s.BaseDir())
	}
	// Verify subdirs created
	for _, sub := range []string{"runs", "pipelines", "updates", "manifests"} {
		p := filepath.Join(tmp, sub)
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("expected dir %s to exist: %v", p, err)
		}
	}
}

func TestSaveAndLoadJSON(t *testing.T) {
	tmp := t.TempDir()
	s, _ := NewFSStore(tmp, testLogger())

	type testData struct {
		Name  string `json:"name"`
		Value int    `json:"value"`
	}

	path := filepath.Join(tmp, "test", "data.json")
	input := testData{Name: "test", Value: 42}

	if err := s.SaveJSON(path, input); err != nil {
		t.Fatalf("SaveJSON: %v", err)
	}

	var output testData
	if err := s.LoadJSON(path, &output); err != nil {
		t.Fatalf("LoadJSON: %v", err)
	}
	if output.Name != "test" || output.Value != 42 {
		t.Fatalf("unexpected: %+v", output)
	}
}

func TestSaveRunResult(t *testing.T) {
	tmp := t.TempDir()
	s, _ := NewFSStore(tmp, testLogger())

	err := s.SaveRunResult("run-123", map[string]string{"status": "ok"})
	if err != nil {
		t.Fatalf("SaveRunResult: %v", err)
	}

	path := filepath.Join(tmp, "runs", "run-123", "result.json")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected result file: %v", err)
	}
}

func TestSaveRunInput(t *testing.T) {
	tmp := t.TempDir()
	s, _ := NewFSStore(tmp, testLogger())

	path, err := s.SaveRunInput("run-456", []string{"a.com", "b.com"})
	if err != nil {
		t.Fatalf("SaveRunInput: %v", err)
	}
	data, _ := os.ReadFile(path)
	if string(data) != "a.com\nb.com\n" {
		t.Fatalf("unexpected input: %q", string(data))
	}
}

func TestPruneRuns(t *testing.T) {
	tmp := t.TempDir()
	s, _ := NewFSStore(tmp, testLogger())

	// Create an old run directory
	oldDir := filepath.Join(tmp, "runs", "old-run")
	os.MkdirAll(oldDir, 0700)
	// Set mod time to the past
	oldTime := time.Now().Add(-48 * time.Hour)
	os.Chtimes(oldDir, oldTime, oldTime)

	// Create a new run
	newDir := filepath.Join(tmp, "runs", "new-run")
	os.MkdirAll(newDir, 0700)

	pruned, err := s.PruneRuns(24 * time.Hour)
	if err != nil {
		t.Fatalf("PruneRuns: %v", err)
	}
	if pruned != 1 {
		t.Fatalf("expected 1 pruned, got %d", pruned)
	}
	if _, err := os.Stat(oldDir); !os.IsNotExist(err) {
		t.Fatal("old dir should be removed")
	}
	if _, err := os.Stat(newDir); err != nil {
		t.Fatal("new dir should still exist")
	}
}

func TestParseRetention(t *testing.T) {
	cases := []struct {
		input    string
		expected time.Duration
	}{
		{"7d", 7 * 24 * time.Hour},
		{"30d", 30 * 24 * time.Hour},
		{"", 7 * 24 * time.Hour},
		{"1h", 1 * time.Hour},
	}
	for _, tc := range cases {
		got := ParseRetention(tc.input)
		if got != tc.expected {
			t.Errorf("ParseRetention(%q) = %v, want %v", tc.input, got, tc.expected)
		}
	}
}
