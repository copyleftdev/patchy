package schema

const SubfinderSchemaVersion = "1.0.0"

type SubfinderRecord struct {
	Host   string `json:"host"`
	Input  string `json:"input"`
	Source string `json:"source,omitempty"`
}
