package schema

const NucleiSchemaVersion = "1.0.0"

type NucleiRecord struct {
	Template         string       `json:"template"`
	TemplateURL      string       `json:"template-url,omitempty"`
	TemplateID       string       `json:"template-id"`
	TemplatePath     string       `json:"template-path,omitempty"`
	Info             NucleiInfo   `json:"info"`
	MatcherName      string       `json:"matcher-name,omitempty"`
	Type             string       `json:"type"`
	Host             string       `json:"host"`
	Port             string       `json:"port,omitempty"`
	Scheme           string       `json:"scheme,omitempty"`
	URL              string       `json:"url,omitempty"`
	MatchedAt        string       `json:"matched-at"`
	IP               string       `json:"ip,omitempty"`
	Timestamp        string       `json:"timestamp"`
	CurlCommand      string       `json:"curl-command,omitempty"`
	ExtractedResults []string     `json:"extracted-results,omitempty"`
	MatcherStatus    bool         `json:"matcher-status,omitempty"`
}

type NucleiInfo struct {
	Name           string                `json:"name"`
	Author         []string              `json:"author"`
	Severity       string                `json:"severity"`
	Description    string                `json:"description,omitempty"`
	Tags           []string              `json:"tags,omitempty"`
	Reference      []string              `json:"reference,omitempty"`
	Classification *NucleiClassification `json:"classification,omitempty"`
}

type NucleiClassification struct {
	CVEID []string `json:"cve-id,omitempty"`
	CWEID []string `json:"cwe-id,omitempty"`
	CVSS  float64  `json:"cvss-score,omitempty"`
}
