package pipeline

import (
	"context"
	"encoding/json"
)

// AssetDiscovery: subfinder → dnsx → httpx
func AssetDiscovery() Pipeline {
	return Pipeline{
		Name:        "asset_discovery",
		Description: "Discover subdomains, resolve DNS, and probe HTTP services.",
		Steps: []Step{
			{
				Name:     "subdomain_enum",
				ToolName: "subfinder",
				BuildArgs: func(ctx context.Context, input StepInput) ([]string, []string, error) {
					return input.Targets, []string{"-d", input.Targets[0]}, nil
				},
				Transform: extractHosts,
			},
			{
				Name:     "dns_resolve",
				ToolName: "dnsx",
				BuildArgs: func(ctx context.Context, input StepInput) ([]string, []string, error) {
					return input.Targets, []string{"-l", "-", "-a", "-resp"}, nil
				},
				Transform: extractHosts,
			},
			{
				Name:     "http_probe",
				ToolName: "httpx",
				BuildArgs: func(ctx context.Context, input StepInput) ([]string, []string, error) {
					return input.Targets, []string{"-l", "-", "-status-code", "-title", "-tech-detect"}, nil
				},
			},
		},
	}
}

// WebAttackSurface: httpx → katana
func WebAttackSurface() Pipeline {
	return Pipeline{
		Name:        "web_attack_surface",
		Description: "Probe HTTP services then crawl discovered web applications.",
		Steps: []Step{
			{
				Name:     "http_probe",
				ToolName: "httpx",
				BuildArgs: func(ctx context.Context, input StepInput) ([]string, []string, error) {
					return input.Targets, []string{"-l", "-", "-status-code", "-title"}, nil
				},
				Transform: extractLiveURLs,
			},
			{
				Name:     "web_crawl",
				ToolName: "katana",
				BuildArgs: func(ctx context.Context, input StepInput) ([]string, []string, error) {
					return input.Targets, []string{"-list", "-", "-d", "3", "-jc"}, nil
				},
			},
		},
	}
}

// VulnSweep: httpx → nuclei
func VulnSweep() Pipeline {
	return Pipeline{
		Name:        "vuln_sweep",
		Description: "Probe HTTP services then run nuclei vulnerability scanner.",
		Steps: []Step{
			{
				Name:     "http_probe",
				ToolName: "httpx",
				BuildArgs: func(ctx context.Context, input StepInput) ([]string, []string, error) {
					return input.Targets, []string{"-l", "-", "-status-code"}, nil
				},
				Transform: extractLiveURLs,
			},
			{
				Name:     "vuln_scan",
				ToolName: "nuclei",
				BuildArgs: func(ctx context.Context, input StepInput) ([]string, []string, error) {
					return input.Targets, []string{"-l", "-", "-as"}, nil
				},
			},
		},
	}
}

// FullRecon: subfinder → dnsx → httpx → katana → nuclei
func FullRecon() Pipeline {
	return Pipeline{
		Name:        "full_recon",
		Description: "Complete reconnaissance: subdomain enumeration, DNS, HTTP probing, crawling, and vulnerability scanning.",
		Steps: []Step{
			{
				Name:     "subdomain_enum",
				ToolName: "subfinder",
				BuildArgs: func(ctx context.Context, input StepInput) ([]string, []string, error) {
					return input.Targets, []string{"-d", input.Targets[0]}, nil
				},
				Transform: extractHosts,
			},
			{
				Name:     "dns_resolve",
				ToolName: "dnsx",
				BuildArgs: func(ctx context.Context, input StepInput) ([]string, []string, error) {
					return input.Targets, []string{"-l", "-", "-a", "-resp"}, nil
				},
				Transform: extractHosts,
			},
			{
				Name:     "http_probe",
				ToolName: "httpx",
				BuildArgs: func(ctx context.Context, input StepInput) ([]string, []string, error) {
					return input.Targets, []string{"-l", "-", "-status-code", "-title", "-tech-detect"}, nil
				},
				Transform: extractLiveURLs,
			},
			{
				Name:     "web_crawl",
				ToolName: "katana",
				BuildArgs: func(ctx context.Context, input StepInput) ([]string, []string, error) {
					return input.Targets, []string{"-list", "-", "-d", "2", "-jc"}, nil
				},
				Transform: extractCrawledURLs,
				Optional:  true,
			},
			{
				Name:     "vuln_scan",
				ToolName: "nuclei",
				BuildArgs: func(ctx context.Context, input StepInput) ([]string, []string, error) {
					return input.Targets, []string{"-l", "-", "-as"}, nil
				},
			},
		},
	}
}

// --- Record transform functions ---

// extractHosts extracts the "host" field from each JSON record.
func extractHosts(records []json.RawMessage) ([]string, error) {
	var hosts []string
	for _, rec := range records {
		var m map[string]interface{}
		if err := json.Unmarshal(rec, &m); err != nil {
			continue
		}
		if host, ok := m["host"].(string); ok && host != "" {
			hosts = append(hosts, host)
		}
	}
	return hosts, nil
}

func extractLiveURLs(records []json.RawMessage) ([]string, error) {
	var urls []string
	for _, rec := range records {
		var m map[string]interface{}
		if err := json.Unmarshal(rec, &m); err != nil {
			continue
		}
		if u, ok := m["url"].(string); ok && u != "" {
			urls = append(urls, u)
		}
	}
	return urls, nil
}

func extractCrawledURLs(records []json.RawMessage) ([]string, error) {
	seen := make(map[string]bool)
	var urls []string
	for _, rec := range records {
		var m map[string]interface{}
		if err := json.Unmarshal(rec, &m); err != nil {
			continue
		}
		if ep, ok := m["endpoint"].(string); ok && ep != "" && !seen[ep] {
			seen[ep] = true
			urls = append(urls, ep)
		}
	}
	return urls, nil
}
