# Stage 3 Feedback Loop and Registry Design

## Goals

- Define the smallest possible feedback-loop boundary without turning the SDK into a control plane.
- Clarify what a future shared `StateStore` should own.
- Decide whether registry evolution should enter implementation now or stay at design level.

## Feedback Loop Boundary

### Minimal Interfaces

- `balancer.ResultReporter`
  - accepts a `RouteReport`
  - designed for optional post-route write-back
- `balancer.StateStore`
  - owns derived runtime state such as cooldown markers
  - intentionally does not expose full snapshot orchestration

### Why This Stays Deferred

- Stage 2 only just established structured failure reasons and explicit affinity storage boundaries.
- There is still no agreed shared-state backend or lifecycle model.
- Wiring feedback directly into `balancer.Route()` now would increase coupling before the storage contract is mature.

## Registry Assessment

### Current State

- `registry.NewManager()` already supports instance-level registries.
- `registry.Default()` is still the path used by `balancer.New()`.
- Factory support exists, but constructor signatures are still untyped `func() Plugin`.

### Decision

- Do not modify runtime wiring in Stage 3.
- Keep the global default registry for backward compatibility.
- Defer instance-level registry injection to a follow-up change that introduces an explicit balancer construction option.
- Defer typed constructors until configuration injection requirements become concrete.

## Follow-up Path

1. Add a balancer construction hook for supplying a registry instance.
2. Keep default behavior unchanged when no registry is provided.
3. Revisit typed constructors only after plugin-specific config objects are defined.
