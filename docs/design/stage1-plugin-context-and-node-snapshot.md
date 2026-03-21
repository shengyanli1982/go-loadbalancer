# Stage 1 Plugin Context and Node Snapshot Design

## Goals

- Unify `context.Context` propagation across algorithm, policy, and objective stages.
- Keep existing algorithm and policy plugins compatible during the transition window.
- Define a clearer `NodeSnapshot` contract without turning it into an oversized state bag.

## Plugin Context Plan

### Current State

- `objective.ContextPlugin` already supports `ctx`.
- `algorithm.Plugin` and `policy.Plugin` are still request-only interfaces.
- `balancer.Route()` already accepts `ctx`, but the algorithm and policy layers do not receive it.

### Design Decision

- Add optional `ContextPlugin` extensions to `plugin/algorithm` and `plugin/policy`.
- Keep the existing `Plugin` interfaces unchanged.
- In `balancer.Route()` and fallback enforcement paths:
  - Prefer the `ContextPlugin` method when the plugin implements it.
  - Fall back to the legacy method otherwise.

### Compatibility Window

- Legacy plugins continue to compile and run unchanged.
- New plugins can opt into cancellation and deadline semantics immediately.
- No registry or config format changes are required for Stage 1.

### Method Shapes

| Layer | Existing method | Optional context-aware method |
| --- | --- | --- |
| Algorithm | `SelectCandidates(req, nodes, topK)` | `SelectCandidatesWithContext(ctx, req, nodes, topK)` |
| Policy | `ReRank(req, candidates)` | `ReRankWithContext(ctx, req, candidates)` |
| Objective | `Choose(req, candidates)` | `ChooseWithContext(ctx, req, candidates)` |

## NodeSnapshot Contract Plan

### Field Layering

| Category | Fields |
| --- | --- |
| Existing core fields | `NodeID`, `Region`, `Pool`, `Healthy`, `Outlier`, `FreshnessTTLms`, `StaticWeight`, `Inflight`, `QueueDepth`, `CPUUtil`, `MemUtil`, `AvgLatencyMs`, `P95LatencyMs`, `ErrorRate`, `KVCacheHitRate`, `TTFTms`, `TPOTms` |
| New Stage 1 core fields | `ObservedAt`, `Version`, `Source`, `CooldownUntil`, `OutlierReason` |
| Deferred for later discussion | `Window`, richer provider-specific metadata, shared-state counters |

### Contract Notes

- `ObservedAt` is the explicit observation timestamp for external state producers.
- `Version` is a monotonic or externally assigned snapshot version string.
- `Source` identifies the producer of the snapshot, such as a probe or controller.
- `CooldownUntil` makes cooldown semantics explicit instead of relying on hidden conventions.
- `OutlierReason` is a lightweight explanation field for degraded nodes.

### Validation Direction

- `ObservedAt` and `CooldownUntil` should be optional zero values.
- `Version` and `Source` should remain optional but must be trimmed when set.
- `OutlierReason` should remain a bounded string for Stage 1 rather than a hard enum.
- `CooldownUntil` must not be earlier than `ObservedAt` when both timestamps are provided.

## Deferred Items

- Stage 1 landed items:
  - `algorithm.ContextPlugin` and `policy.ContextPlugin` are now available as optional interfaces.
  - `balancer.Route()` and fallback hard-policy checks now prefer ctx-aware plugin methods when present.
  - `NodeSnapshot` now exposes `ObservedAt`, `Version`, `Source`, `CooldownUntil`, and `OutlierReason`.
  - `NodeSnapshot.Validate()` now enforces trimmed optional strings and timestamp ordering for cooldown metadata.

- Failure reason modeling stays in Stage 2.
- Policy role splitting stays in Stage 2.
- Shared affinity storage and feedback loops stay out of Stage 1.
