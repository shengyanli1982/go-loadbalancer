package balancer

import (
	"context"
	"time"
)

// StateStore is the minimal boundary for writing derived runtime state without embedding a control plane.
type StateStore interface {
	SetCooldown(ctx context.Context, nodeID string, until time.Time) error
	ClearCooldown(ctx context.Context, nodeID string) error
}
