package store

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// FSStore implements filesystem-based artifact storage.
type FSStore struct {
	baseDir string
	logger  *slog.Logger
}

// NewFSStore creates a new filesystem store, creating base directories on demand.
func NewFSStore(baseDir string, logger *slog.Logger) (*FSStore, error) {
	if baseDir == "" {
		home := os.Getenv("HOME")
		if home == "" {
			return nil, fmt.Errorf("HOME not set and no base_dir configured")
		}
		baseDir = filepath.Join(home, ".patchy")
	}

	// Expand $HOME in path
	if len(baseDir) > 0 && baseDir[0] == '$' {
		baseDir = os.ExpandEnv(baseDir)
	}

	dirs := []string{
		baseDir,
		filepath.Join(baseDir, "runs"),
		filepath.Join(baseDir, "pipelines"),
		filepath.Join(baseDir, "updates"),
		filepath.Join(baseDir, "manifests"),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0700); err != nil {
			return nil, fmt.Errorf("failed to create directory %s: %w", d, err)
		}
	}

	return &FSStore{
		baseDir: baseDir,
		logger:  logger,
	}, nil
}

// BaseDir returns the store's root directory.
func (s *FSStore) BaseDir() string {
	return s.baseDir
}

// SaveJSON atomically writes a JSON file (write-to-temp then rename).
func (s *FSStore) SaveJSON(path string, v interface{}) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}

	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return fmt.Errorf("write tmp: %w", err)
	}

	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("rename: %w", err)
	}

	return nil
}

// LoadJSON reads and unmarshals a JSON file.
func (s *FSStore) LoadJSON(path string, v interface{}) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

// SaveRunResult persists a RunResult to the runs directory.
func (s *FSStore) SaveRunResult(runID string, result interface{}) error {
	path := filepath.Join(s.baseDir, "runs", runID, "result.json")
	return s.SaveJSON(path, result)
}

// SaveRunInput writes target list to the run directory.
func (s *FSStore) SaveRunInput(runID string, targets []string) (string, error) {
	dir := filepath.Join(s.baseDir, "runs", runID)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "input.txt")
	content := strings.Join(targets, "\n") + "\n"
	return path, os.WriteFile(path, []byte(content), 0600)
}

// GetRunOutputPath returns the path for a run's output file.
func (s *FSStore) GetRunOutputPath(runID string) string {
	return filepath.Join(s.baseDir, "runs", runID)
}

// SaveRawOutput writes raw stdout/stderr to the run directory.
func (s *FSStore) SaveRawOutput(runID, stdout, stderr string) error {
	dir := filepath.Join(s.baseDir, "runs", runID)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "stdout.txt"), []byte(stdout), 0600); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "stderr.txt"), []byte(stderr), 0600)
}

// SavePipelineResult persists a pipeline result.
func (s *FSStore) SavePipelineResult(pipelineID string, result interface{}) error {
	path := filepath.Join(s.baseDir, "pipelines", pipelineID, "report.json")
	return s.SaveJSON(path, result)
}

// SaveCheckpoint persists a pipeline checkpoint.
func (s *FSStore) SaveCheckpoint(pipelineID string, checkpoint interface{}) error {
	path := filepath.Join(s.baseDir, "pipelines", pipelineID, "checkpoint.json")
	return s.SaveJSON(path, checkpoint)
}

// LoadCheckpoint loads a pipeline checkpoint.
func (s *FSStore) LoadCheckpoint(pipelineID string, v interface{}) error {
	path := filepath.Join(s.baseDir, "pipelines", pipelineID, "checkpoint.json")
	return s.LoadJSON(path, v)
}

// SaveStepResult persists an individual pipeline step result.
func (s *FSStore) SaveStepResult(pipelineID, stepName string, result interface{}) error {
	path := filepath.Join(s.baseDir, "pipelines", pipelineID, fmt.Sprintf("step_%s.json", stepName))
	return s.SaveJSON(path, result)
}

// SaveUpdateResult persists an update result with timestamp.
func (s *FSStore) SaveUpdateResult(runID string, result interface{}) error {
	ts := time.Now().Format("20060102-150405")
	dir := fmt.Sprintf("%s-%s", ts, runID)
	path := filepath.Join(s.baseDir, "updates", dir, "diff.json")
	return s.SaveJSON(path, result)
}

// SaveManifest persists the latest manifest snapshot.
func (s *FSStore) SaveManifest(manifest interface{}) error {
	path := filepath.Join(s.baseDir, "manifests", "latest.json")
	return s.SaveJSON(path, manifest)
}

// PruneRuns deletes run directories older than the given duration.
func (s *FSStore) PruneRuns(olderThan time.Duration) (int, error) {
	return s.pruneDir(filepath.Join(s.baseDir, "runs"), olderThan)
}

// PrunePipelines deletes pipeline directories older than the given duration.
func (s *FSStore) PrunePipelines(olderThan time.Duration) (int, error) {
	return s.pruneDir(filepath.Join(s.baseDir, "pipelines"), olderThan)
}

// PruneUpdates deletes update directories older than the given duration.
func (s *FSStore) PruneUpdates(olderThan time.Duration) (int, error) {
	return s.pruneDir(filepath.Join(s.baseDir, "updates"), olderThan)
}

func (s *FSStore) pruneDir(dir string, olderThan time.Duration) (int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, err
	}

	cutoff := time.Now().Add(-olderThan)
	pruned := 0

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if info.ModTime().Before(cutoff) {
			path := filepath.Join(dir, entry.Name())
			if err := os.RemoveAll(path); err != nil {
				s.logger.Warn("prune_failed", "path", path, "error", err)
				continue
			}
			pruned++
		}
	}

	return pruned, nil
}

// ParseRetention parses a retention string like "7d", "30d" into time.Duration.
func ParseRetention(s string) time.Duration {
	if s == "" {
		return 7 * 24 * time.Hour // default 7 days
	}
	// Simple parser: Nd format
	if len(s) > 1 && s[len(s)-1] == 'd' {
		var days int
		if _, err := fmt.Sscanf(s, "%dd", &days); err == nil {
			return time.Duration(days) * 24 * time.Hour
		}
	}
	// Try standard Go duration
	d, err := time.ParseDuration(s)
	if err != nil {
		return 7 * 24 * time.Hour
	}
	return d
}
