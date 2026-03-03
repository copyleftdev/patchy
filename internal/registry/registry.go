package registry

import (
	"context"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/patchy-mcp/patchy/pkg/schema"
)

var knownTools = []string{
	"subfinder",
	"dnsx",
	"httpx",
	"naabu",
	"katana",
	"nuclei",
}

// BinaryConfig holds binary search configuration.
type BinaryConfig struct {
	SearchPath string
	PdtmPath   string
	Overrides  map[string]string
}

// Registry discovers, validates, and tracks all PD tool binaries.
type Registry struct {
	tools     map[string]*schema.ToolEntry
	pdtm      *schema.PdtmEntry
	templates *schema.TemplateInfo
	config    BinaryConfig
	logger    *slog.Logger
	mu        sync.RWMutex
}

func New(cfg BinaryConfig, logger *slog.Logger) *Registry {
	return &Registry{
		tools:  make(map[string]*schema.ToolEntry),
		config: cfg,
		logger: logger,
	}
}

// Refresh re-scans all binaries and rebuilds the manifest.
func (r *Registry) Refresh(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	searchPath := r.config.SearchPath
	overrides := r.config.Overrides
	if overrides == nil {
		overrides = make(map[string]string)
	}

	var lastErr error

	// Discover each known tool
	for _, name := range knownTools {
		entry := &schema.ToolEntry{
			Name:      name,
			CheckedAt: time.Now(),
		}

		binaryPath, err := ResolveBinary(name, overrides, searchPath)
		if err != nil {
			entry.Installed = false
			entry.Healthy = false
			entry.Error = err.Error()
			r.tools[name] = entry
			r.logger.Warn("binary_not_found", "component", "registry", "tool", name, "error", err)
			lastErr = err
			continue
		}

		entry.BinaryPath = binaryPath
		entry.Installed = true

		version, err := GetVersion(binaryPath)
		if err != nil {
			entry.Healthy = false
			entry.Error = err.Error()
			r.logger.Warn("version_failed", "component", "registry", "tool", name, "error", err)
			lastErr = err
		} else {
			entry.Version = version
			entry.Healthy = true
		}

		r.tools[name] = entry
	}

	// pdtm
	r.pdtm = &schema.PdtmEntry{}
	pdtmPath, err := ResolveBinary("pdtm", overrides, searchPath)
	if err != nil {
		r.pdtm.Installed = false
		r.logger.Warn("pdtm_not_found", "component", "registry", "error", err)
	} else {
		r.pdtm.BinaryPath = pdtmPath
		r.pdtm.Installed = true
		if v, err := GetVersion(pdtmPath); err == nil {
			r.pdtm.Version = v
			r.pdtm.Healthy = true
		}
	}

	// nuclei templates
	r.templates = &schema.TemplateInfo{
		Path: GetTemplatePath(),
	}
	if nucleiEntry, ok := r.tools["nuclei"]; ok && nucleiEntry.Healthy {
		if tv, err := GetTemplateVersion(nucleiEntry.BinaryPath); err == nil {
			r.templates.Version = tv
		}
		// Check if template directory exists for last_update
		if info, err := os.Stat(r.templates.Path); err == nil {
			r.templates.LastUpdate = info.ModTime()
		}
	}

	return lastErr
}

// GetManifest returns the current manifest snapshot.
func (r *Registry) GetManifest() *schema.Manifest {
	r.mu.RLock()
	defer r.mu.RUnlock()

	m := &schema.Manifest{
		GeneratedAt: time.Now(),
		Pdtm:        r.pdtm,
		Templates:   r.templates,
	}

	for _, name := range knownTools {
		if entry, ok := r.tools[name]; ok {
			m.Tools = append(m.Tools, *entry)
		}
	}

	return m
}

// GetBinaryPath returns the resolved absolute path for a tool.
func (r *Registry) GetBinaryPath(toolName string) (string, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	entry, ok := r.tools[toolName]
	if !ok {
		return "", &RegistryError{Tool: toolName, Message: "tool not registered"}
	}
	if !entry.Installed {
		return "", &RegistryError{Tool: toolName, Message: "binary not installed"}
	}
	if !entry.Healthy {
		return "", &RegistryError{Tool: toolName, Message: "binary not healthy: " + entry.Error}
	}
	return entry.BinaryPath, nil
}

// GetAllowedBinaries returns name→path map for the Runner.
func (r *Registry) GetAllowedBinaries() map[string]string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	binaries := make(map[string]string)
	for name, entry := range r.tools {
		if entry.Installed && entry.BinaryPath != "" {
			binaries[name] = entry.BinaryPath
		}
	}
	// Include pdtm
	if r.pdtm != nil && r.pdtm.Installed && r.pdtm.BinaryPath != "" {
		binaries["pdtm"] = r.pdtm.BinaryPath
	}
	return binaries
}

// IsHealthy returns true if all required tools are installed and runnable.
func (r *Registry) IsHealthy() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	for _, entry := range r.tools {
		if !entry.Healthy {
			return false
		}
	}
	return true
}

// GetToolVersion returns the version string for a tool.
func (r *Registry) GetToolVersion(toolName string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if entry, ok := r.tools[toolName]; ok {
		return entry.Version
	}
	return ""
}

// GetTemplatesVersion returns the nuclei templates version.
func (r *Registry) GetTemplatesVersion() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.templates != nil {
		return r.templates.Version
	}
	return ""
}

// RegistryError is a structured error from the Registry.
type RegistryError struct {
	Tool    string
	Message string
}

func (e *RegistryError) Error() string {
	return "registry: " + e.Tool + ": " + e.Message
}
