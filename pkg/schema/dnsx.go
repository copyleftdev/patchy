package schema

const DnsxSchemaVersion = "1.0.0"

type DnsxRecord struct {
	Host       string   `json:"host"`
	Resolver   []string `json:"resolver,omitempty"`
	A          []string `json:"a,omitempty"`
	AAAA       []string `json:"aaaa,omitempty"`
	CNAME      []string `json:"cname,omitempty"`
	MX         []string `json:"mx,omitempty"`
	NS         []string `json:"ns,omitempty"`
	TXT        []string `json:"txt,omitempty"`
	SOA        []string `json:"soa,omitempty"`
	PTR        []string `json:"ptr,omitempty"`
	SRV        []string `json:"srv,omitempty"`
	CAA        []string `json:"caa,omitempty"`
	CDN        string   `json:"cdn,omitempty"`
	ASN        string   `json:"asn,omitempty"`
	StatusCode string   `json:"status_code,omitempty"`
}
