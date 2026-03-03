package schema

const NaabuSchemaVersion = "1.0.0"

type NaabuRecord struct {
	Host      string `json:"host"`
	IP        string `json:"ip"`
	Port      int    `json:"port"`
	Protocol  string `json:"protocol,omitempty"`
	Timestamp string `json:"timestamp,omitempty"`
}
