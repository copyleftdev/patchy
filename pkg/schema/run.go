package schema

import "encoding/json"

const RunResultSchemaVersion = "1.0.0"

// RunResult is the universal return envelope for every tool execution.
type RunResult struct {
	SchemaVersion string          `json:"schema_version"`
	RunID         string          `json:"run_id"`
	Tool          string          `json:"tool"`
	ToolVersion   string          `json:"tool_version"`
	BinaryPath    string          `json:"binary_path"`
	Invocation    InvocationInfo  `json:"invocation"`
	Timing        TimingInfo      `json:"timing"`
	Result        ResultPayload   `json:"result"`
	Status        string          `json:"status"` // success | error | timeout | cancelled | policy_denied
	Error         *ErrorInfo      `json:"error,omitempty"`
	Environment   EnvironmentInfo `json:"environment"`
}

type InvocationInfo struct {
	Args       []string `json:"args"`
	Cwd        string   `json:"cwd"`
	EnvProfile string   `json:"env_profile"`
}

type TimingInfo struct {
	Start      string `json:"start"`       // RFC 3339
	End        string `json:"end"`         // RFC 3339
	DurationMs int64  `json:"duration_ms"`
}

type ResultPayload struct {
	Records    []json.RawMessage `json:"records"`
	RecordType string            `json:"record_type"`
	Count      int               `json:"count"`
	Stdout     string            `json:"stdout"`
	Stderr     string            `json:"stderr"`
	Truncated  bool              `json:"truncated"`
}

type ErrorInfo struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Hint    string `json:"hint,omitempty"`
}

type EnvironmentInfo struct {
	PatchyVersion    string `json:"patchy_version"`
	ToolVersion      string `json:"tool_version"`
	TemplatesVersion string `json:"templates_version,omitempty"`
	OS               string `json:"os"`
	Arch             string `json:"arch"`
}
