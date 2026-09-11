package agent

import (
	"github.com/ThanabordeeN/AgentRD2/runtime-go/internal/backend"
	"github.com/ThanabordeeN/AgentRD2/runtime-go/internal/config"
)

// ruleBackendSeed mirrors the Python “RuleBasedAgentBackend“ default seed.
const ruleBackendSeed = 7

// newDefaultBackend mirrors the Python constructor default
// “backend or RuleBasedAgentBackend()“: the deterministic, instantaneous
// backend used by tests, the dry-run demo, and the scenario runner.
func newDefaultBackend(settings config.Settings) backend.Backend {
	return backend.NewRuleBackend(backend.RuleOptions{
		SilenceProbability: settings.Speech.SilenceProbability,
		Seed:               ruleBackendSeed,
	})
}
