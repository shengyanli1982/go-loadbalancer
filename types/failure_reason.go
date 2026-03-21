package types

import "fmt"

// FailureReason identifies why the main routing path degraded into fallback.
type FailureReason string

const (
	FailureReasonNoCandidate     FailureReason = "no_candidate"
	FailureReasonAlgorithmError  FailureReason = "algorithm_error"
	FailureReasonPolicyReject    FailureReason = "policy_reject"
	FailureReasonObjectiveTimeout FailureReason = "objective_timeout"
	FailureReasonObjectiveError  FailureReason = "objective_error"
	FailureReasonAffinityMiss    FailureReason = "affinity_miss"
)

// FailureCause preserves a structured failure reason while still unwrapping to the underlying error.
type FailureCause struct {
	Reason FailureReason
	Err    error
}

// NewFailureCause constructs a structured failure cause for fallback handling.
func NewFailureCause(reason FailureReason, err error) FailureCause {
	return FailureCause{
		Reason: reason,
		Err:    err,
	}
}

func (c FailureCause) Error() string {
	if c.Err == nil {
		return fmt.Sprintf("reason=%s", c.Reason)
	}
	return fmt.Sprintf("reason=%s err=%v", c.Reason, c.Err)
}

func (c FailureCause) Unwrap() error {
	return c.Err
}
