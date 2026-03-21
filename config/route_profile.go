package config

import (
	"fmt"

	lberrors "github.com/shengyanli1982/go-loadbalancer/errors"
	"github.com/shengyanli1982/go-loadbalancer/registry"
	"github.com/shengyanli1982/go-loadbalancer/types"
)

const (
	FieldRouteProfiles = "route_profiles"
)

// RouteProfile defines per-route-class routing profile.
type RouteProfile struct {
	Algorithm    string
	Policies     []string
	DegradeChain []string
}

// RouteProfileConfig holds route profiles keyed by RouteClass.
type RouteProfileConfig struct {
	ByRouteClass map[types.RouteClass]RouteProfile
}

func (c *Config) resolveRouteProfile(routeClass types.RouteClass) RouteProfile {
	profile := RouteProfile{Algorithm: c.Plugins.Algorithms[routeClass]}
	if len(c.Plugins.Policies) > 0 {
		profile.Policies = append([]string(nil), c.Plugins.Policies...)
	}
	if len(c.FallbackChain) > 0 {
		profile.DegradeChain = append([]string(nil), c.FallbackChain...)
	}

	if c.RouteProfiles.ByRouteClass == nil {
		return profile
	}
	override, ok := c.RouteProfiles.ByRouteClass[routeClass]
	if !ok {
		return profile
	}
	if override.Algorithm != "" {
		profile.Algorithm = override.Algorithm
	}
	if len(override.Policies) > 0 {
		profile.Policies = append([]string(nil), override.Policies...)
	}
	if len(override.DegradeChain) > 0 {
		profile.DegradeChain = append([]string(nil), override.DegradeChain...)
	}
	return profile
}

// RouteProfileFor returns merged profile for route class.
func (c *Config) RouteProfileFor(routeClass types.RouteClass) RouteProfile {
	return c.resolveRouteProfile(routeClass)
}

func validateRouteProfiles(c *Config) []error {
	if len(c.RouteProfiles.ByRouteClass) == 0 {
		return nil
	}

	out := make([]error, 0)
	for routeClass := range c.RouteProfiles.ByRouteClass {
		if isValidRouteClass(routeClass) {
			continue
		}
		out = append(out, lberrors.NewConfigError(
			lberrors.CodeInvalidRouteClass,
			fmt.Sprintf("%s.%s", FieldRouteProfiles, routeClass),
			routeClass,
			"route profile key must be one of generic,llm-prefill,llm-decode",
		))
	}

	for _, routeClass := range c.RouteClasses {
		if !isValidRouteClass(routeClass) {
			continue
		}

		profile := c.resolveRouteProfile(routeClass)
		fieldPrefix := fmt.Sprintf("%s.%s", FieldRouteProfiles, routeClass)
		if profile.Algorithm == "" {
			out = append(out, lberrors.NewConfigError(
				lberrors.CodeMissingAlgorithmBinding,
				fmt.Sprintf("%s.algorithm", fieldPrefix),
				profile.Algorithm,
				"route profile must bind one algorithm per route class",
			))
			continue
		}
		if !registry.HasAlgorithm(profile.Algorithm) {
			out = append(out, lberrors.NewConfigError(
				lberrors.CodeMissingAlgorithmBinding,
				fmt.Sprintf("%s.algorithm", fieldPrefix),
				profile.Algorithm,
				"algorithm is not registered",
			))
		}

		seen := make(map[string]struct{}, len(profile.Policies))
		for idx, policyName := range profile.Policies {
			policyField := fmt.Sprintf("%s.policies[%d]", fieldPrefix, idx)
			if policyName == "" {
				out = append(out, lberrors.NewConfigError(
					lberrors.CodeUnknownPolicy,
					policyField,
					policyName,
					"policy is not registered",
				))
				continue
			}
			if _, exists := seen[policyName]; exists {
				out = append(out, lberrors.NewConfigError(
					lberrors.CodeDuplicatePolicy,
					policyField,
					policyName,
					"policy must be unique",
				))
				continue
			}
			seen[policyName] = struct{}{}
			if registry.HasPolicy(policyName) {
				continue
			}
			out = append(out, lberrors.NewConfigError(
				lberrors.CodeUnknownPolicy,
				policyField,
				policyName,
				"policy is not registered",
			))
		}

		out = append(out, validateProfileDegradeChain(fieldPrefix, profile.DegradeChain)...)
	}
	return out
}

func validateProfileDegradeChain(fieldPrefix string, chain []string) []error {
	if len(chain) == 0 {
		return []error{lberrors.NewConfigError(
			lberrors.CodeInvalidFallbackChain,
			fmt.Sprintf("%s.degrade_chain", fieldPrefix),
			nil,
			"degrade chain must not be empty",
		)}
	}

	seen := make(map[string]struct{}, len(chain))
	out := make([]error, 0)
	for idx, step := range chain {
		field := fmt.Sprintf("%s.degrade_chain[%d]", fieldPrefix, idx)
		if step == "" {
			out = append(out, lberrors.NewConfigError(
				lberrors.CodeInvalidFallbackChain,
				field,
				step,
				"must not be empty",
			))
			continue
		}
		if _, ok := seen[step]; ok {
			out = append(out, lberrors.NewConfigError(
				lberrors.CodeInvalidFallbackChain,
				field,
				step,
				"must not contain duplicates",
			))
			continue
		}
		seen[step] = struct{}{}
		if step == FallbackPolicyRanked || registry.HasAlgorithm(step) {
			continue
		}
		out = append(out, lberrors.NewConfigError(
			lberrors.CodeInvalidFallbackChain,
			field,
			step,
			"must be policy_ranked or a registered algorithm",
		))
	}
	return out
}

// WithRouteProfile sets route profile override.
func WithRouteProfile(routeClass types.RouteClass, profile RouteProfile) Option {
	return func(c *Config) {
		if c.RouteProfiles.ByRouteClass == nil {
			c.RouteProfiles.ByRouteClass = make(map[types.RouteClass]RouteProfile)
		}
		clone := RouteProfile{Algorithm: profile.Algorithm}
		if len(profile.Policies) > 0 {
			clone.Policies = append([]string(nil), profile.Policies...)
		}
		if len(profile.DegradeChain) > 0 {
			clone.DegradeChain = append([]string(nil), profile.DegradeChain...)
		}
		c.RouteProfiles.ByRouteClass[routeClass] = clone
	}
}

// WithRouteDegradeChain sets per-route degrade chain.
func WithRouteDegradeChain(routeClass types.RouteClass, chain ...string) Option {
	return func(c *Config) {
		if c.RouteProfiles.ByRouteClass == nil {
			c.RouteProfiles.ByRouteClass = make(map[types.RouteClass]RouteProfile)
		}
		profile := c.RouteProfiles.ByRouteClass[routeClass]
		profile.DegradeChain = append([]string(nil), chain...)
		c.RouteProfiles.ByRouteClass[routeClass] = profile
	}
}
