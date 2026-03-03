package schema

const KatanaSchemaVersion = "1.0.0"

type KatanaRecord struct {
	Timestamp string         `json:"timestamp,omitempty"`
	Request   KatanaRequest  `json:"request"`
	Response  KatanaResponse `json:"response,omitempty"`
}

type KatanaRequest struct {
	Method   string `json:"method,omitempty"`
	Endpoint string `json:"endpoint"`
	Raw      string `json:"raw,omitempty"`
}

type KatanaResponse struct {
	StatusCode    int               `json:"status_code,omitempty"`
	Headers       map[string]string `json:"headers,omitempty"`
	Body          string            `json:"body,omitempty"`
	ContentLength int               `json:"content_length,omitempty"`
	Raw           string            `json:"raw,omitempty"`
}
