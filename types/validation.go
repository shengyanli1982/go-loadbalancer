package types

import (
	"errors"
	"fmt"
	"strings"

	"github.com/go-playground/validator/v10"
)

var inputValidator = validator.New(validator.WithRequiredStructEnabled())

// ValidationError represents a field-level input validation error.
type ValidationError struct {
	Field      string
	Value      any
	Constraint string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("field=%s value=%v constraint=%s", e.Field, e.Value, e.Constraint)
}

type requestValidationView struct {
	RouteClass           RouteClass `validate:"required,oneof=generic llm-prefill llm-decode"`
	PromptTokens         int        `validate:"gte=0"`
	ExpectedTokens       int        `validate:"gte=0"`
	BudgetMaxTotalTokens int        `validate:"gte=0"`
	BudgetMaxInflight    int        `validate:"gte=0"`
	BudgetMaxQueueDepth  int        `validate:"gte=0"`
}

type nodeValidationView struct {
	NodeID         string  `validate:"required"`
	StaticWeight   int     `validate:"gte=0"`
	Inflight       int     `validate:"gte=0"`
	QueueDepth     int     `validate:"gte=0"`
	CPUUtil        float64 `validate:"gte=0,lte=100"`
	MemUtil        float64 `validate:"gte=0,lte=100"`
	AvgLatencyMs   float64 `validate:"gte=0"`
	P95LatencyMs   float64 `validate:"gte=0"`
	ErrorRate      float64 `validate:"gte=0,lte=1"`
	KVCacheHitRate float64 `validate:"gte=0,lte=1"`
	TTFTMs         float64 `validate:"gte=0"`
	TPOTMs         float64 `validate:"gte=0"`
}

func (r RequestContext) Validate() error {
	view := requestValidationView{
		RouteClass:           r.RouteClass,
		PromptTokens:         r.PromptTokens,
		ExpectedTokens:       r.ExpectedTokens,
		BudgetMaxTotalTokens: r.BudgetMaxTotalTokens,
		BudgetMaxInflight:    r.BudgetMaxInflight,
		BudgetMaxQueueDepth:  r.BudgetMaxQueueDepth,
	}
	return validateInput(view, mapRequestValidationError)
}

func (n NodeSnapshot) Validate() error {
	view := nodeValidationView{
		NodeID:         n.NodeID,
		StaticWeight:   n.StaticWeight,
		Inflight:       n.Inflight,
		QueueDepth:     n.QueueDepth,
		CPUUtil:        n.CPUUtil,
		MemUtil:        n.MemUtil,
		AvgLatencyMs:   n.AvgLatencyMs,
		P95LatencyMs:   n.P95LatencyMs,
		ErrorRate:      n.ErrorRate,
		KVCacheHitRate: n.KVCacheHitRate,
		TTFTMs:         n.TTFTms,
		TPOTMs:         n.TPOTms,
	}
	errs := make([]error, 0, 4)
	if err := validateInput(view, mapNodeValidationError); err != nil {
		errs = append(errs, err)
	}
	if n.Version != strings.TrimSpace(n.Version) {
		errs = append(errs, &ValidationError{Field: "version", Value: n.Version, Constraint: "must not have leading or trailing whitespace"})
	}
	if n.Source != strings.TrimSpace(n.Source) {
		errs = append(errs, &ValidationError{Field: "source", Value: n.Source, Constraint: "must not have leading or trailing whitespace"})
	}
	if n.OutlierReason != strings.TrimSpace(n.OutlierReason) {
		errs = append(errs, &ValidationError{Field: "outlier_reason", Value: n.OutlierReason, Constraint: "must not have leading or trailing whitespace"})
	}
	if !n.CooldownUntil.IsZero() && !n.ObservedAt.IsZero() && n.CooldownUntil.Before(n.ObservedAt) {
		errs = append(errs, &ValidationError{Field: "cooldown_until", Value: n.CooldownUntil, Constraint: "must be >= observed_at when both are set"})
	}
	if len(errs) == 0 {
		return nil
	}
	return errors.Join(errs...)
}

func validateInput(view any, mapper func(validator.FieldError) error) error {
	err := inputValidator.Struct(view)
	if err == nil {
		return nil
	}

	var validationErrs validator.ValidationErrors
	if !errors.As(err, &validationErrs) {
		return err
	}

	out := make([]error, 0, len(validationErrs))
	for _, item := range validationErrs {
		out = append(out, mapper(item))
	}
	return errors.Join(out...)
}

func mapRequestValidationError(fieldErr validator.FieldError) error {
	switch fieldErr.StructField() {
	case "RouteClass":
		if fieldErr.Tag() == "required" {
			return &ValidationError{Field: "route_class", Value: fieldErr.Value(), Constraint: "must not be empty"}
		}
		return &ValidationError{Field: "route_class", Value: fieldErr.Value(), Constraint: "must be one of generic,llm-prefill,llm-decode"}
	case "PromptTokens":
		return &ValidationError{Field: "prompt_tokens", Value: fieldErr.Value(), Constraint: "must be >= 0"}
	case "ExpectedTokens":
		return &ValidationError{Field: "expected_tokens", Value: fieldErr.Value(), Constraint: "must be >= 0"}
	case "BudgetMaxTotalTokens":
		return &ValidationError{Field: "budget_max_total_tokens", Value: fieldErr.Value(), Constraint: "must be >= 0"}
	case "BudgetMaxInflight":
		return &ValidationError{Field: "budget_max_inflight", Value: fieldErr.Value(), Constraint: "must be >= 0"}
	case "BudgetMaxQueueDepth":
		return &ValidationError{Field: "budget_max_queue_depth", Value: fieldErr.Value(), Constraint: "must be >= 0"}
	default:
		return &ValidationError{Field: fieldErr.StructField(), Value: fieldErr.Value(), Constraint: fieldErr.Error()}
	}
}

func mapNodeValidationError(fieldErr validator.FieldError) error {
	switch fieldErr.StructField() {
	case "NodeID":
		return &ValidationError{Field: "node_id", Value: fieldErr.Value(), Constraint: "must not be empty"}
	case "StaticWeight":
		return &ValidationError{Field: "static_weight", Value: fieldErr.Value(), Constraint: "must be >= 0"}
	case "Inflight":
		return &ValidationError{Field: "inflight", Value: fieldErr.Value(), Constraint: "must be >= 0"}
	case "QueueDepth":
		return &ValidationError{Field: "queue_depth", Value: fieldErr.Value(), Constraint: "must be >= 0"}
	case "CPUUtil":
		return &ValidationError{Field: "cpu_util", Value: fieldErr.Value(), Constraint: "must be between 0 and 100"}
	case "MemUtil":
		return &ValidationError{Field: "mem_util", Value: fieldErr.Value(), Constraint: "must be between 0 and 100"}
	case "AvgLatencyMs":
		return &ValidationError{Field: "avg_latency_ms", Value: fieldErr.Value(), Constraint: "must be >= 0"}
	case "P95LatencyMs":
		return &ValidationError{Field: "p95_latency_ms", Value: fieldErr.Value(), Constraint: "must be >= 0"}
	case "ErrorRate":
		return &ValidationError{Field: "error_rate", Value: fieldErr.Value(), Constraint: "must be between 0 and 1"}
	case "KVCacheHitRate":
		return &ValidationError{Field: "kv_cache_hit_rate", Value: fieldErr.Value(), Constraint: "must be between 0 and 1"}
	case "TTFTMs":
		return &ValidationError{Field: "ttft_ms", Value: fieldErr.Value(), Constraint: "must be >= 0"}
	case "TPOTMs":
		return &ValidationError{Field: "tpot_ms", Value: fieldErr.Value(), Constraint: "must be >= 0"}
	default:
		return &ValidationError{Field: fieldErr.StructField(), Value: fieldErr.Value(), Constraint: fieldErr.Error()}
	}
}
