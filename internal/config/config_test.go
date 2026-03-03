package config

import (
	"os"
	"testing"
)

func TestPatchyScopeEnvDomains(t *testing.T) {
	t.Setenv("PATCHY_SCOPE", "example.com,target.org")
	cfg := Defaults()
	applyEnvOverrides(&cfg)

	if len(cfg.Policy.Scope.AllowDomains) != 2 {
		t.Fatalf("expected 2 domains, got %d", len(cfg.Policy.Scope.AllowDomains))
	}
	if cfg.Policy.Scope.AllowDomains[0] != "example.com" {
		t.Errorf("expected example.com, got %s", cfg.Policy.Scope.AllowDomains[0])
	}
	if cfg.Policy.Scope.AllowDomains[1] != "target.org" {
		t.Errorf("expected target.org, got %s", cfg.Policy.Scope.AllowDomains[1])
	}
}

func TestPatchyScopeEnvCIDRs(t *testing.T) {
	t.Setenv("PATCHY_SCOPE", "10.0.0.0/8,192.168.1.0/24")
	cfg := Defaults()
	applyEnvOverrides(&cfg)

	if len(cfg.Policy.Scope.AllowCIDRs) != 2 {
		t.Fatalf("expected 2 CIDRs, got %d", len(cfg.Policy.Scope.AllowCIDRs))
	}
	if cfg.Policy.Scope.AllowCIDRs[0] != "10.0.0.0/8" {
		t.Errorf("expected 10.0.0.0/8, got %s", cfg.Policy.Scope.AllowCIDRs[0])
	}
	if cfg.Policy.Scope.AllowCIDRs[1] != "192.168.1.0/24" {
		t.Errorf("expected 192.168.1.0/24, got %s", cfg.Policy.Scope.AllowCIDRs[1])
	}
}

func TestPatchyScopeEnvMixed(t *testing.T) {
	t.Setenv("PATCHY_SCOPE", "example.com, 10.0.0.0/8, target.org")
	cfg := Defaults()
	applyEnvOverrides(&cfg)

	if len(cfg.Policy.Scope.AllowDomains) != 2 {
		t.Fatalf("expected 2 domains, got %d: %v", len(cfg.Policy.Scope.AllowDomains), cfg.Policy.Scope.AllowDomains)
	}
	if len(cfg.Policy.Scope.AllowCIDRs) != 1 {
		t.Fatalf("expected 1 CIDR, got %d: %v", len(cfg.Policy.Scope.AllowCIDRs), cfg.Policy.Scope.AllowCIDRs)
	}
}

func TestPatchyScopeEnvEmpty(t *testing.T) {
	t.Setenv("PATCHY_SCOPE", "")
	cfg := Defaults()
	applyEnvOverrides(&cfg)

	if len(cfg.Policy.Scope.AllowDomains) != 0 {
		t.Errorf("expected 0 domains, got %d", len(cfg.Policy.Scope.AllowDomains))
	}
	if len(cfg.Policy.Scope.AllowCIDRs) != 0 {
		t.Errorf("expected 0 CIDRs, got %d", len(cfg.Policy.Scope.AllowCIDRs))
	}
}

func TestPatchyScopeEnvAppendsToExisting(t *testing.T) {
	t.Setenv("PATCHY_SCOPE", "new.example.com")
	cfg := Defaults()
	cfg.Policy.Scope.AllowDomains = []string{"existing.example.com"}
	applyEnvOverrides(&cfg)

	if len(cfg.Policy.Scope.AllowDomains) != 2 {
		t.Fatalf("expected 2 domains, got %d: %v", len(cfg.Policy.Scope.AllowDomains), cfg.Policy.Scope.AllowDomains)
	}
	if cfg.Policy.Scope.AllowDomains[0] != "existing.example.com" {
		t.Errorf("expected existing.example.com first, got %s", cfg.Policy.Scope.AllowDomains[0])
	}
	if cfg.Policy.Scope.AllowDomains[1] != "new.example.com" {
		t.Errorf("expected new.example.com second, got %s", cfg.Policy.Scope.AllowDomains[1])
	}
}

func TestPatchyScopeUnset(t *testing.T) {
	os.Unsetenv("PATCHY_SCOPE")
	cfg := Defaults()
	applyEnvOverrides(&cfg)

	if len(cfg.Policy.Scope.AllowDomains) != 0 {
		t.Errorf("expected 0 domains, got %d", len(cfg.Policy.Scope.AllowDomains))
	}
}
