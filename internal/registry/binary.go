package registry

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var versionRegex = regexp.MustCompile(`v?\d+\.\d+\.\d+`)

const versionTimeout = 5 * time.Second

// ResolveBinary finds the absolute path for a tool binary.
// Search order:
//  1. Explicit path from config overrides
//  2. pdtm managed path ($HOME/.pdtm/go/bin/<tool>)
//  3. configSearchPath/<tool>
//  4. exec.LookPath fallback
func ResolveBinary(name string, overrides map[string]string, searchPath string) (string, error) {
	// 1. Check explicit override
	if p, ok := overrides[name]; ok && p != "" {
		abs, err := filepath.Abs(p)
		if err != nil {
			return "", fmt.Errorf("invalid override path for %s: %w", name, err)
		}
		if _, err := os.Stat(abs); err != nil {
			return "", fmt.Errorf("override binary not found for %s at %s: %w", name, abs, err)
		}
		return abs, nil
	}

	// 2. pdtm default path
	home := os.Getenv("HOME")
	if home != "" {
		pdtmPath := filepath.Join(home, ".pdtm", "go", "bin", name)
		if info, err := os.Stat(pdtmPath); err == nil && info.Mode()&0111 != 0 {
			return pdtmPath, nil
		}
	}

	// 3. Configured search path
	if searchPath != "" {
		candidate := filepath.Join(searchPath, name)
		if info, err := os.Stat(candidate); err == nil && info.Mode()&0111 != 0 {
			return candidate, nil
		}
	}

	// 4. LookPath fallback
	p, err := exec.LookPath(name)
	if err != nil {
		return "", fmt.Errorf("binary %q not found in any search path", name)
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", fmt.Errorf("cannot resolve absolute path for %s: %w", name, err)
	}
	return abs, nil
}

// GetVersion runs `<binary> -version` and parses the version string.
func GetVersion(binaryPath string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), versionTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, binaryPath, "-version")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	// Don't fail on non-zero exit — some tools print version on stderr and exit 0, others exit non-zero
	_ = cmd.Run()

	combined := stdout.String() + "\n" + stderr.String()
	match := versionRegex.FindString(combined)
	if match != "" {
		if !strings.HasPrefix(match, "v") {
			match = "v" + match
		}
		return match, nil
	}

	// Return raw output trimmed as fallback
	raw := strings.TrimSpace(combined)
	if raw != "" {
		return raw, nil
	}
	return "", fmt.Errorf("no version output from %s", binaryPath)
}

// GetTemplateVersion runs `nuclei -templates-version` and extracts the version.
func GetTemplateVersion(nucleiBinary string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), versionTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, nucleiBinary, "-templates-version")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	_ = cmd.Run()

	combined := stdout.String() + "\n" + stderr.String()
	match := versionRegex.FindString(combined)
	if match != "" {
		if !strings.HasPrefix(match, "v") {
			match = "v" + match
		}
		return match, nil
	}
	return "", fmt.Errorf("no template version from nuclei")
}

// GetTemplatePath returns the nuclei templates directory.
func GetTemplatePath() string {
	if p := os.Getenv("NUCLEI_TEMPLATES"); p != "" {
		return p
	}
	home := os.Getenv("HOME")
	if home != "" {
		return filepath.Join(home, "nuclei-templates")
	}
	return ""
}
