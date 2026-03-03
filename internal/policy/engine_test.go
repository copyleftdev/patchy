package policy

import (
	"log/slog"
	"os"
	"testing"
	"time"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func TestEvaluate_NoScope_PassesAll(t *testing.T) {
	eng := New(PolicyConfig{
		Scope: ScopeConfig{
			AllowDomains: []string{"*.com"},
		},
		RateLimits: RateLimitConfig{
			Defaults: RateLimitEntry{RequestsPerMin: 60, Burst: 10},
		},
		Concurrency: ConcurrencyConfig{
			Defaults: ConcurrencyEntry{MaxConcurrent: 5},
		},
		Timeouts: TimeoutConfig{
			Defaults: TimeoutEntry{Default: "5m", Max: "30m"},
		},
	}, testLogger())

	result := eng.Evaluate(EvalRequest{
		ToolName:   "subfinder",
		BinaryName: "subfinder",
		Targets:    []string{"example.com"},
		Args:       []string{"-d", "example.com", "-json"},
	})

	if !result.Allowed {
		t.Fatalf("expected allowed, got denials: %v", result.Denials)
	}
	if result.ClampedTimeout != 5*time.Minute {
		t.Fatalf("expected 5m timeout, got %v", result.ClampedTimeout)
	}

	eng.ReleaseConcurrency("subfinder")
}

func TestEvaluate_ScopeViolation(t *testing.T) {
	eng := New(PolicyConfig{
		Scope: ScopeConfig{
			AllowDomains: []string{"allowed.com"},
		},
		RateLimits: RateLimitConfig{
			Defaults: RateLimitEntry{RequestsPerMin: 60, Burst: 10},
		},
		Concurrency: ConcurrencyConfig{
			Defaults: ConcurrencyEntry{MaxConcurrent: 5},
		},
	}, testLogger())

	result := eng.Evaluate(EvalRequest{
		ToolName:   "subfinder",
		BinaryName: "subfinder",
		Targets:    []string{"evil.com"},
		Args:       []string{"-d", "evil.com"},
	})

	if result.Allowed {
		t.Fatal("expected denial for scope violation")
	}
	if len(result.Denials) == 0 {
		t.Fatal("expected at least one denial")
	}
	if result.Denials[0].Code != DenyScopeViolation {
		t.Fatalf("expected SCOPE_VIOLATION, got %s", result.Denials[0].Code)
	}
}

func TestEvaluate_UpdateLock(t *testing.T) {
	eng := New(PolicyConfig{
		Scope: ScopeConfig{
			AllowDomains: []string{"*.com"},
		},
		RateLimits: RateLimitConfig{
			Defaults: RateLimitEntry{RequestsPerMin: 60, Burst: 10},
		},
		Concurrency: ConcurrencyConfig{
			Defaults: ConcurrencyEntry{MaxConcurrent: 5},
		},
	}, testLogger())

	eng.SetUpdateLock(true)

	result := eng.Evaluate(EvalRequest{
		ToolName:   "subfinder",
		BinaryName: "subfinder",
		Targets:    []string{"example.com"},
	})

	if result.Allowed {
		t.Fatal("expected denial during update lock")
	}
	if result.Denials[0].Code != DenyUpdateInProgress {
		t.Fatalf("expected UPDATE_IN_PROGRESS, got %s", result.Denials[0].Code)
	}

	eng.SetUpdateLock(false)

	result2 := eng.Evaluate(EvalRequest{
		ToolName:   "subfinder",
		BinaryName: "subfinder",
		Targets:    []string{"example.com"},
	})
	if !result2.Allowed {
		t.Fatal("expected allowed after lock release")
	}
	eng.ReleaseConcurrency("subfinder")
}

func TestEvaluate_BlockedFlag(t *testing.T) {
	eng := New(PolicyConfig{
		Scope: ScopeConfig{
			AllowDomains: []string{"*.com"},
		},
		RateLimits: RateLimitConfig{
			Defaults: RateLimitEntry{RequestsPerMin: 60, Burst: 10},
		},
		Concurrency: ConcurrencyConfig{
			Defaults: ConcurrencyEntry{MaxConcurrent: 5},
		},
	}, testLogger())

	result := eng.Evaluate(EvalRequest{
		ToolName:   "subfinder",
		BinaryName: "subfinder",
		Targets:    []string{"example.com"},
		Args:       []string{"-d", "example.com", "-update"},
	})

	if result.Allowed {
		t.Fatal("expected denial for blocked flag -update")
	}
	if result.Denials[0].Code != DenyBlockedFlag {
		t.Fatalf("expected BLOCKED_FLAG, got %s", result.Denials[0].Code)
	}
}

func TestEvaluate_TimeoutClamp(t *testing.T) {
	eng := New(PolicyConfig{
		Scope: ScopeConfig{
			AllowDomains: []string{"*.com"},
		},
		RateLimits: RateLimitConfig{
			Defaults: RateLimitEntry{RequestsPerMin: 60, Burst: 10},
		},
		Concurrency: ConcurrencyConfig{
			Defaults: ConcurrencyEntry{MaxConcurrent: 5},
		},
		Timeouts: TimeoutConfig{
			Defaults: TimeoutEntry{Default: "2m", Max: "10m"},
		},
	}, testLogger())

	// Request exceeding max
	result := eng.Evaluate(EvalRequest{
		ToolName:   "subfinder",
		BinaryName: "subfinder",
		Targets:    []string{"example.com"},
		Timeout:    20 * time.Minute,
	})

	if !result.Allowed {
		t.Fatalf("expected allowed, got denials: %v", result.Denials)
	}
	if result.ClampedTimeout != 10*time.Minute {
		t.Fatalf("expected 10m clamped timeout, got %v", result.ClampedTimeout)
	}
	eng.ReleaseConcurrency("subfinder")

	// Request within bounds
	result2 := eng.Evaluate(EvalRequest{
		ToolName:   "subfinder",
		BinaryName: "subfinder",
		Targets:    []string{"example.com"},
		Timeout:    3 * time.Minute,
	})
	if result2.ClampedTimeout != 3*time.Minute {
		t.Fatalf("expected 3m, got %v", result2.ClampedTimeout)
	}
	eng.ReleaseConcurrency("subfinder")
}

func TestEvaluate_NaabuSynScanBlocked(t *testing.T) {
	eng := New(PolicyConfig{
		Scope: ScopeConfig{
			AllowDomains: []string{"*.com"},
		},
		RateLimits: RateLimitConfig{
			Defaults: RateLimitEntry{RequestsPerMin: 60, Burst: 10},
		},
		Concurrency: ConcurrencyConfig{
			Defaults: ConcurrencyEntry{MaxConcurrent: 5},
		},
		Naabu: NaabuPolicyConfig{AllowSynScan: false},
	}, testLogger())

	// -s syn should be blocked
	r1 := eng.Evaluate(EvalRequest{
		ToolName:   "naabu",
		BinaryName: "naabu",
		Targets:    []string{"example.com"},
		Args:       []string{"-host", "example.com", "-s", "syn"},
	})
	if r1.Allowed {
		t.Fatal("expected denial for SYN scan")
	}
	if r1.Denials[0].Code != DenyBlockedFlag {
		t.Fatalf("expected BLOCKED_FLAG, got %s", r1.Denials[0].Code)
	}

	// -s connect should be allowed
	r2 := eng.Evaluate(EvalRequest{
		ToolName:   "naabu",
		BinaryName: "naabu",
		Targets:    []string{"example.com"},
		Args:       []string{"-host", "example.com", "-s", "connect"},
	})
	if !r2.Allowed {
		t.Fatalf("expected allowed for connect scan, got denials: %v", r2.Denials)
	}
	eng.ReleaseConcurrency("naabu")

	// bare -s with no value should be blocked
	r3 := eng.Evaluate(EvalRequest{
		ToolName:   "naabu",
		BinaryName: "naabu",
		Targets:    []string{"example.com"},
		Args:       []string{"-host", "example.com", "-s"},
	})
	if r3.Allowed {
		t.Fatal("expected denial for bare -s flag")
	}
}

func TestEvaluate_ConcurrencyLimit(t *testing.T) {
	eng := New(PolicyConfig{
		Scope: ScopeConfig{
			AllowDomains: []string{"*.com"},
		},
		RateLimits: RateLimitConfig{
			Defaults: RateLimitEntry{RequestsPerMin: 600, Burst: 100},
		},
		Concurrency: ConcurrencyConfig{
			Defaults: ConcurrencyEntry{MaxConcurrent: 1},
		},
	}, testLogger())

	// First should pass
	r1 := eng.Evaluate(EvalRequest{
		ToolName:   "subfinder",
		BinaryName: "subfinder",
		Targets:    []string{"a.com"},
	})
	if !r1.Allowed {
		t.Fatal("first request should be allowed")
	}

	// Second should be denied (concurrency=1)
	r2 := eng.Evaluate(EvalRequest{
		ToolName:   "subfinder",
		BinaryName: "subfinder",
		Targets:    []string{"b.com"},
	})
	if r2.Allowed {
		t.Fatal("second request should be denied (concurrency limit)")
	}
	if r2.Denials[0].Code != DenyConcurrency {
		t.Fatalf("expected CONCURRENCY_LIMIT, got %s", r2.Denials[0].Code)
	}

	// Release and retry
	eng.ReleaseConcurrency("subfinder")
	r3 := eng.Evaluate(EvalRequest{
		ToolName:   "subfinder",
		BinaryName: "subfinder",
		Targets:    []string{"c.com"},
	})
	if !r3.Allowed {
		t.Fatal("third request should be allowed after release")
	}
	eng.ReleaseConcurrency("subfinder")
}
