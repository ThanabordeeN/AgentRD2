// Package backend defines how the runtime asks a model (or a deterministic
// test double) what an NPC should do next.
package backend

import (
	"context"

	"github.com/ThanabordeeN/AgentRD2/runtime-go/internal/domain"
)

// Backend turns an agent context into a decision.
//
// Implementations must be safe for concurrent use: the runtime may consult
// several NPCs at once.
type Backend interface {
	// Name identifies the backend in logs and diagnostics.
	Name() string

	// Decide returns the NPC's next decision. Returning a decision with no
	// speech and no actions means "stay silent and do nothing".
	Decide(ctx context.Context, agentCtx *domain.AgentContext) (*domain.AgentDecision, error)

	// SupportsWaitGestures reports whether Decide has real generation latency,
	// in which case the runtime plays a short thinking/listening gesture while
	// it waits.
	SupportsWaitGestures() bool
}
