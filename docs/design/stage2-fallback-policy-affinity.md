# Stage 2 Fallback, Policy, and Affinity Design

## Goals

- Give fallback a structured failure reason instead of relying only on free-form error strings.
- Clarify the internal role split inside the current policy chain without forcing an external API explosion.
- Replace the in-memory session affinity map with a small storage interface.

## Failure Reason Model

### Stage 2 Failure Reasons

| Reason | Meaning |
| --- | --- |
| `no_candidate` | The primary selection path returned no usable candidate. |
| `algorithm_error` | The configured algorithm failed unexpectedly. |
| `policy_reject` | A policy stage rejected or emptied the ranked candidate set. |
| `objective_timeout` | Objective selection exceeded its timeout budget. |
| `objective_error` | Objective selection failed for a non-timeout reason. |
| `affinity_miss` | Reserved for future affinity-specific degrade paths. |

### Design Notes

- The fallback chain format stays unchanged in Stage 2.
- The routing path now passes a typed failure reason plus the original error into fallback.
- Fallback result annotations should record the reason in a stable `cause=<reason>` format.

## Policy Role Mapping

| Current plugin | Internal role |
| --- | --- |
| `health_gate` | guard |
| `tenant_quota` | guard |
| `llm_budget_gate` | guard |
| `llm_kv_affinity` | affinity |
| `llm_session_affinity` | affinity |
| `llm_stage_aware` | rerank |
| `llm_token_aware_queue` | rerank |

### Current Decision

- Keep the public `policy.Plugin` contract for now.
- Use the role map as an execution and documentation aid before deciding whether to split public interfaces.

## Affinity Store Plan

- Introduce an `AffinityStore` with `Get`, `Set`, and `Delete`.
- Preserve the current process-local behavior via a default in-memory implementation.
- Keep TTL semantics explicit at the interface boundary rather than hidden in ad-hoc map usage.

### Stage 2 Landing

- `balancer` now depends on `AffinityStore` instead of a raw session map.
- The default memory store supports lazy TTL expiry and explicit delete semantics.
