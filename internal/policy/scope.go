package policy

import (
	"net"
	"net/url"
	"strings"
)

// ScopeConfig defines allowed and denied targets.
type ScopeConfig struct {
	AllowDomains []string `yaml:"allow_domains"`
	AllowCIDRs   []string `yaml:"allow_cidrs"`
	AllowURLs    []string `yaml:"allow_urls"`
	DenyDomains  []string `yaml:"deny_domains"`
	DenyCIDRs    []string `yaml:"deny_cidrs"`
}

// scopeChecker evaluates targets against domain/CIDR allowlists.
type scopeChecker struct {
	allowDomains []string
	denyCIDRs    []*net.IPNet
	allowCIDRs   []*net.IPNet
	denyDomains  []string
	allowURLs    []string
}

func newScopeChecker(cfg ScopeConfig) *scopeChecker {
	sc := &scopeChecker{
		allowDomains: cfg.AllowDomains,
		denyDomains:  cfg.DenyDomains,
		allowURLs:    cfg.AllowURLs,
	}
	for _, cidr := range cfg.AllowCIDRs {
		if _, ipnet, err := net.ParseCIDR(cidr); err == nil {
			sc.allowCIDRs = append(sc.allowCIDRs, ipnet)
		}
	}
	for _, cidr := range cfg.DenyCIDRs {
		if _, ipnet, err := net.ParseCIDR(cidr); err == nil {
			sc.denyCIDRs = append(sc.denyCIDRs, ipnet)
		}
	}
	return sc
}

// IsEmpty returns true if no scope restrictions are configured.
func (sc *scopeChecker) IsEmpty() bool {
	return len(sc.allowDomains) == 0 &&
		len(sc.allowCIDRs) == 0 &&
		len(sc.allowURLs) == 0
}

// CheckTarget validates a single target string against the scope.
// Returns a Denial if the target is not allowed, nil if allowed.
func (sc *scopeChecker) CheckTarget(target string) *Denial {
	if sc.IsEmpty() {
		return &Denial{
			Code:    DenyNoScope,
			Message: "no scope configured; all targets denied by default",
			Detail:  target,
		}
	}

	// Normalize target: extract host from URL if needed
	host := extractHost(target)

	// Check deny lists first (deny overrides allow)
	if sc.isDenied(host, target) {
		return &Denial{
			Code:    DenyScopeViolation,
			Message: "target explicitly denied",
			Detail:  target,
		}
	}

	// Check allow
	if sc.isAllowed(host, target) {
		return nil
	}

	return &Denial{
		Code:    DenyScopeViolation,
		Message: "target not in scope allowlist",
		Detail:  target,
	}
}

func (sc *scopeChecker) isDenied(host, rawTarget string) bool {
	for _, d := range sc.denyDomains {
		if matchDomain(host, d) {
			return true
		}
	}

	ip := net.ParseIP(host)
	if ip != nil {
		for _, cidr := range sc.denyCIDRs {
			if cidr.Contains(ip) {
				return true
			}
		}
	}

	return false
}

func (sc *scopeChecker) isAllowed(host, rawTarget string) bool {
	// Check URL allowlist
	for _, allowed := range sc.allowURLs {
		if strings.HasPrefix(rawTarget, allowed) {
			return true
		}
	}

	// Check domain allowlist
	for _, d := range sc.allowDomains {
		if matchDomain(host, d) {
			return true
		}
	}

	// Check CIDR allowlist
	ip := net.ParseIP(host)
	if ip != nil {
		for _, cidr := range sc.allowCIDRs {
			if cidr.Contains(ip) {
				return true
			}
		}
	}

	return false
}

// matchDomain checks if host matches a domain pattern.
// Patterns: "example.com" matches exactly, ".example.com" or "*.example.com" matches subdomains.
func matchDomain(host, pattern string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	pattern = strings.ToLower(strings.TrimSuffix(pattern, "."))

	// Wildcard: *.example.com
	if strings.HasPrefix(pattern, "*.") {
		suffix := pattern[1:] // ".example.com"
		return strings.HasSuffix(host, suffix) || host == pattern[2:]
	}

	// Suffix match: .example.com
	if strings.HasPrefix(pattern, ".") {
		return strings.HasSuffix(host, pattern) || host == pattern[1:]
	}

	// Exact match
	return host == pattern
}

// extractHost pulls the hostname from a target (URL, host:port, or bare host/IP).
func extractHost(target string) string {
	// Try URL parse first
	if strings.Contains(target, "://") {
		if u, err := url.Parse(target); err == nil && u.Hostname() != "" {
			return u.Hostname()
		}
	}

	// Strip port if present
	if h, _, err := net.SplitHostPort(target); err == nil {
		return h
	}

	return target
}
