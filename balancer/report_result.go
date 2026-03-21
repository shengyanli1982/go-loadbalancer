package balancer

import (
	"context"
	"time"

	"github.com/shengyanli1982/go-loadbalancer/types"
)

// ReportOutcome describes the post-route outcome class for feedback loops.
type ReportOutcome string

const (
	ReportOutcomeSuccess ReportOutcome = "success"
	ReportOutcomeFailure ReportOutcome = "failure"
	ReportOutcomeTimeout ReportOutcome = "timeout"
)

// RouteReport carries the minimum post-route feedback payload.
type RouteReport struct {
	RouteClass    types.RouteClass
	SessionID     string
	NodeID        string
	Outcome       ReportOutcome
	FailureReason types.FailureReason
	Duration      time.Duration
	ObservedAt    time.Time
}

// ResultReporter is a future extension point for feeding route results back into shared state.
type ResultReporter interface {
	ReportResult(ctx context.Context, report RouteReport) error
}
