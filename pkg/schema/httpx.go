package schema

const HttpxSchemaVersion = "1.0.0"

type HttpxRecord struct {
	URL           string   `json:"url"`
	Input         string   `json:"input"`
	StatusCode    int      `json:"status_code,omitempty"`
	ContentLength int64    `json:"content_length,omitempty"`
	ContentType   string   `json:"content_type,omitempty"`
	Title         string   `json:"title,omitempty"`
	WebServer     string   `json:"webserver,omitempty"`
	Technologies  []string `json:"technologies,omitempty"`
	Host          string   `json:"host,omitempty"`
	Port          string   `json:"port,omitempty"`
	Scheme        string   `json:"scheme,omitempty"`
	Method        string   `json:"method,omitempty"`
	ResponseTime  string   `json:"response_time,omitempty"`
	A             []string `json:"a,omitempty"`
	CNAME         string   `json:"cname,omitempty"`
	CDNName       string   `json:"cdn_name,omitempty"`
	CDNType       string   `json:"cdn_type,omitempty"`
	JARM          string   `json:"jarm,omitempty"`
}
