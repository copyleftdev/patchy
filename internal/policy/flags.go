package policy

import "strings"

// Per-tool blocked flags (security-critical flags that must never be passed).
var blockedFlags = map[string][]string{
	"nuclei": {
		"-headless",
		"-system-resolvers",
		"-passive",
		"-code",
	},
	"naabu": {
		"-interface",
		"-source-ip",
	},
}

// globalBlockedFlags are blocked for all tools.
var globalBlockedFlags = []string{
	"-update",
	"-update-templates",
	"-disable-update-check", // only PATCHY injects this
}

// CheckFlags validates args against the flag blocklist.
// Returns a Denial if any blocked flag is found.
func CheckFlags(toolName string, args []string) *Denial {
	toolBlocked := blockedFlags[toolName]

	for _, arg := range args {
		for _, blocked := range globalBlockedFlags {
			if matchFlag(arg, blocked) {
				return &Denial{
					Code:    DenyBlockedFlag,
					Message: "flag " + arg + " is blocked",
					Detail:  "global blocklist",
				}
			}
		}

		for _, blocked := range toolBlocked {
			if matchFlag(arg, blocked) {
				return &Denial{
					Code:    DenyBlockedFlag,
					Message: "flag " + arg + " is blocked for " + toolName,
					Detail:  "tool-specific blocklist",
				}
			}
		}
	}

	return nil
}

func matchFlag(arg, blocked string) bool {
	// Normalize: strip leading dashes for comparison
	argNorm := strings.TrimLeft(arg, "-")
	blockedNorm := strings.TrimLeft(blocked, "-")
	return strings.EqualFold(argNorm, blockedNorm)
}

// ToolRateCapConfig holds per-tool rate cap settings for tool-internal -rl flag.
type ToolRateCapConfig struct {
	Defaults  ToolRateCapEntry            `yaml:"defaults"`
	Overrides map[string]ToolRateCapEntry `yaml:"overrides"`
}

type ToolRateCapEntry struct {
	RLMax int `yaml:"rl_max"`
}

// ClampRateLimit returns the clamped rate limit value for a tool.
// If requested > max, returns max. If requested <= 0, returns max (default).
func ClampRateLimit(toolName string, requested int, cfg ToolRateCapConfig) (clamped int, wasClamped bool) {
	max := cfg.Defaults.RLMax
	if max <= 0 {
		max = 300
	}
	if override, ok := cfg.Overrides[toolName]; ok && override.RLMax > 0 {
		max = override.RLMax
	}

	if requested <= 0 {
		return max, false
	}
	if requested > max {
		return max, true
	}
	return requested, false
}
