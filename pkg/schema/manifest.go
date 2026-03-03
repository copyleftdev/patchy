package schema

import "time"

type Manifest struct {
	Tools         []ToolEntry   `json:"tools"`
	Templates     *TemplateInfo `json:"templates"`
	Pdtm          *PdtmEntry   `json:"pdtm"`
	GeneratedAt   time.Time     `json:"generated_at"`
	PatchyVersion string        `json:"patchy_version"`
}

type ToolEntry struct {
	Name       string    `json:"name"`
	BinaryPath string    `json:"binary_path"`
	Version    string    `json:"version"`
	Installed  bool      `json:"installed"`
	Healthy    bool      `json:"healthy"`
	Error      string    `json:"error,omitempty"`
	CheckedAt  time.Time `json:"checked_at"`
}

type PdtmEntry struct {
	BinaryPath string `json:"binary_path"`
	Version    string `json:"version"`
	Installed  bool   `json:"installed"`
	Healthy    bool   `json:"healthy"`
}

type TemplateInfo struct {
	Version    string    `json:"version"`
	Path       string    `json:"path"`
	LastUpdate time.Time `json:"last_update"`
}

type ManifestDiff struct {
	Pdtm      *VersionChange `json:"pdtm,omitempty"`
	Tools     []ToolChange   `json:"tools"`
	Templates *VersionChange `json:"templates,omitempty"`
}

type VersionChange struct {
	Before  string `json:"before"`
	After   string `json:"after"`
	Changed bool   `json:"changed"`
}

type ToolChange struct {
	Name    string `json:"name"`
	Before  string `json:"before"`
	After   string `json:"after"`
	Changed bool   `json:"changed"`
}

// Diff compares two manifests and returns what changed.
func Diff(before, after *Manifest) *ManifestDiff {
	d := &ManifestDiff{}

	if before.Pdtm != nil && after.Pdtm != nil {
		d.Pdtm = &VersionChange{
			Before:  before.Pdtm.Version,
			After:   after.Pdtm.Version,
			Changed: before.Pdtm.Version != after.Pdtm.Version,
		}
	}

	if before.Templates != nil && after.Templates != nil {
		d.Templates = &VersionChange{
			Before:  before.Templates.Version,
			After:   after.Templates.Version,
			Changed: before.Templates.Version != after.Templates.Version,
		}
	}

	afterMap := make(map[string]string)
	for _, t := range after.Tools {
		afterMap[t.Name] = t.Version
	}
	for _, bt := range before.Tools {
		av := afterMap[bt.Name]
		d.Tools = append(d.Tools, ToolChange{
			Name:    bt.Name,
			Before:  bt.Version,
			After:   av,
			Changed: bt.Version != av,
		})
	}

	return d
}
