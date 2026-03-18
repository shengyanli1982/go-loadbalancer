<div align="center">
  <h1>A2X Go Load Balancer</h1>
  <p><strong>One balancing core for both generic traffic and LLM inference traffic.</strong></p>
  <p>Default-Ready · Plugin-First · Fallback-Safe</p>
</div>

![Architecture](https://img.shields.io/badge/Architecture-A2X-2E86DE)
![Tests](https://img.shields.io/badge/Tests-16%20passing-success)
![Race Support](https://img.shields.io/badge/Race%20Test-Windows%20Friendly-brightgreen)
[![Ask DeepWiki](https://deepwiki.com/badge.svg)](https://deepwiki.com/shengyanli1982/go-loadbalancer)

`go-loadbalancer` is a production-oriented routing library for teams that need stable load balancing today and safe strategy evolution tomorrow.

Start with a default config and built-in plugins. Then evolve toward advanced LLM-aware policies without changing your core mental model.

## Responsibility Boundary

This is an SDK routing decision library, and upstream probing should be implemented by external systems, which then update each node's state.

## Why Teams Choose A2X

- **One core, multiple traffic classes**: `generic`, `llm-prefill`, `llm-decode`.
- **Safe by default**: no Objective plugin required for stable routing.
- **Composable plugin architecture**: algorithm, policy, and objective layers are decoupled.
- **Failure containment built in**: fallback chain protects availability when advanced plugins fail.
- **Strict config contracts**: typed config, validation, structured errors, and aggregated failures.

## Product Fit

A2X is built for:

- Platform teams building shared routing foundations.
- Inference teams that need both cost control and latency control.
- Backend teams that want gradual strategy rollout instead of risky rewrites.

## Capability Portfolio

| Layer             | Built-in                                                                                     | What It Gives You                          |
| ----------------- | -------------------------------------------------------------------------------------------- | ------------------------------------------ |
| Core Balancer     | Routing pipeline + fallback                                                                  | Stable hot path and predictable behavior   |
| Algorithm Plugins | `rr`, `wrr`, `ch`, `p2c`, `lr`                                                               | Fast balancing strategies for real traffic |
| Policy Plugins    | `health_gate`, `tenant_quota`, `llm_kv_affinity`, `llm_stage_aware`, `llm_token_aware_queue` | Hard constraints before final pick         |
| Objective Plugin  | `weighted_objective` (optional)                                                              | Top-K second-pass optimization             |
| Telemetry         | `Sink`, `NoopSink`                                                                           | Clean observability integration boundary   |

## Algorithm IDs

We use short algorithm IDs in config for product readability:

| ID    | Meaning              |
| ----- | -------------------- |
| `rr`  | Round Robin          |
| `wrr` | Weighted Round Robin |
| `p2c` | Power of Two Choices |
| `lr`  | Least Request        |
| `ch`  | Consistent Hash      |

## Performance

```bash
go test -run ^$ -bench . -benchmem ./balancer ./plugin/algorithm/rr ./plugin/algorithm/wrr ./plugin/algorithm/consistenthash ./plugin/algorithm/p2c ./plugin/algorithm/leastrequest ./plugin/objective/weighted ./registry
```

Benchmark environment (measured on 2026-03-14):

- Go `1.24.13`
- OS/Arch: `windows/amd64`
- CPU: `12th Gen Intel(R) Core(TM) i5-12400F`

Core route benchmarks:

| Benchmark                                                | ns/op | B/op | allocs/op |
| -------------------------------------------------------- | ----: | ---: | --------: |
| `BenchmarkRoute/serial_nodes_32`                         | 663.7 | 2032 |         4 |
| `BenchmarkRoute/serial_nodes_256`                        |  1751 | 2032 |         4 |
| `BenchmarkRoute/serial_nodes_1024`                       |  5034 | 2032 |         4 |
| `BenchmarkRoute/parallel_nodes_256`                      | 602.8 | 2032 |         4 |
| `BenchmarkRoute/serial_default_config_nodes_256`         |  2175 | 2016 |         9 |
| `BenchmarkRoute/serial_objective_enabled_nodes_256`      |  3858 | 2888 |        11 |
| `BenchmarkRoute/serial_fallback_policy_ranked_nodes_256` |  1922 | 2057 |         5 |

Failure-path benchmarks:

| Benchmark                                              | ns/op | B/op | allocs/op |
| ------------------------------------------------------ | ----: | ---: | --------: |
| `BenchmarkRouteFailurePaths/serial_no_healthy_nodes`   | 22.23 |    0 |         0 |
| `BenchmarkRouteFailurePaths/serial_no_model_available` | 21.80 |    0 |         0 |
| `BenchmarkRouteFailurePaths/serial_empty_candidates`   | 903.3 |   56 |         2 |
| `BenchmarkRouteFailurePaths/serial_algorithm_error`    |  1046 |  168 |         6 |

Selected component benchmarks:

| Benchmark                                             | ns/op | B/op | allocs/op |
| ----------------------------------------------------- | ----: | ---: | --------: |
| `BenchmarkSelectCandidates/nodes_1024_topk_8` (`rr`)  | 367.7 | 1792 |         2 |
| `BenchmarkSelectCandidates/nodes_1024_topk_8` (`wrr`) |  4060 | 1792 |         2 |
| `BenchmarkSelectCandidates/nodes_1024_topk_8` (`ch`)  |  3091 | 1920 |         3 |
| `BenchmarkSelectCandidates/nodes_1024_topk_8` (`p2c`) |  4045 | 1856 |         3 |
| `BenchmarkSelectCandidates/nodes_1024_topk_8` (`lr`)  |  2622 | 2112 |         3 |
| `BenchmarkChoose` (`plugin/objective/weighted`)       | 375.0 |   16 |         1 |
| `BenchmarkManagerGetAlgorithm/hit_serial`             | 21.93 |    0 |         0 |
| `BenchmarkManagerHasAlgorithm/hit_serial`             | 16.75 |    0 |         0 |
| `BenchmarkManagerRegisterAlgorithmParallel`           | 484.7 |  163 |         1 |

Algorithm deep-dive benchmark command:

```bash
go test -run ^$ -bench BenchmarkSelectCandidates -benchmem ./plugin/algorithm/rr ./plugin/algorithm/wrr ./plugin/algorithm/consistenthash ./plugin/algorithm/p2c ./plugin/algorithm/leastrequest
```

Numbers are from a single local run and should be used as a baseline reference. Re-run on your target hardware for production capacity planning.

Objective guard benchmark command:

```bash
go test -run ^$ -bench "BenchmarkRoute/(serial_nodes_256|serial_default_config_nodes_256|serial_objective_enabled_nodes_256|parallel_objective_guard_max_concurrent_1_nodes_256|parallel_objective_guard_max_concurrent_64_nodes_256)$|BenchmarkRouteObjectiveGuardLatency/(max_concurrent_1_nodes_256|max_concurrent_64_nodes_256)$" -benchmem -benchtime=2s -count=1 ./balancer
```

Objective guard benchmark environment (measured on 2026-03-18):

- Go `1.24.13`
- OS/Arch: `windows/amd64`
- CPU: `Intel(R) Core(TM) 5 210H`

Objective guard benchmark results:

| Benchmark                                                             |  ns/op | req/s | p95 (ms) | p99 (ms) | B/op | allocs/op |
| --------------------------------------------------------------------- | -----: | ----: | -------: | -------: | ---: | --------: |
| `BenchmarkRoute/serial_nodes_256`                                     |   2180 |   N/A |      N/A |      N/A | 1392 |         3 |
| `BenchmarkRoute/serial_default_config_nodes_256`                      |   3979 |   N/A |      N/A |      N/A | 5248 |        16 |
| `BenchmarkRoute/serial_objective_enabled_nodes_256`                   |   5123 |   N/A |      N/A |      N/A | 2680 |        16 |
| `BenchmarkRoute/parallel_objective_guard_max_concurrent_1_nodes_256`  | 616182 |   N/A |      N/A |      N/A | 2649 |        16 |
| `BenchmarkRoute/parallel_objective_guard_max_concurrent_64_nodes_256` |  52409 |   N/A |      N/A |      N/A | 2648 |        16 |
| `BenchmarkRouteObjectiveGuardLatency/max_concurrent_1_nodes_256`      | 630585 |  1585 |    9.471 |    11.56 | 2649 |        16 |
| `BenchmarkRouteObjectiveGuardLatency/max_concurrent_64_nodes_256`     |  52822 | 18926 |    1.001 |    1.259 | 2648 |        16 |

Observed deltas from this run:

- objective enabled (serial): `5123 ns/op` vs `3979 ns/op` (default config), +28.7% hot-path cost in this synthetic setup.
- parallel throughput (guard 64 vs guard 1): `18926 req/s` vs `1585 req/s`, about `11.9x` improvement.
- tail latency (guard 64 vs guard 1): p95 `1.001 ms` vs `9.471 ms`, about `89.4%` lower; p99 `1.259 ms` vs `11.56 ms`, about `89.1%` lower.

## Quick Start

### 1) Verify your local setup

```bash
go test ./...
```

### 2) Minimal integration example

```go
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/shengyanli1982/go-loadbalancer/balancer"
	"github.com/shengyanli1982/go-loadbalancer/config"
	"github.com/shengyanli1982/go-loadbalancer/types"
)

func main() {
	lb, err := balancer.New(
		config.DefaultConfig(),
		config.WithTopK(5),
		config.WithAlgorithm(types.RouteGeneric, config.AlgorithmLeastRequest),
		config.WithPolicies(config.PolicyHealthGate),
	)
	if err != nil {
		log.Fatalf("create balancer: %v", err)
	}
	defer func() { _ = lb.Close(context.Background()) }()

	req := types.RequestContext{
		RequestID:  "req-1",
		TenantID:   "team-a",
		SessionID:  "session-a",
		RouteClass: types.RouteGeneric,
		Model:      "model-a",
	}
	modelASet := types.NewModelCapabilitySet(map[string]bool{"model-a": true})

	nodes := []types.NodeSnapshot{
		{
			NodeID:          "node-a",
			Healthy:         true,
			Inflight:        10,
			QueueDepth:      5,
			P95LatencyMs:    30,
			ErrorRate:       0.02,
			ModelCapability: modelASet,
		},
		{
			NodeID:          "node-b",
			Healthy:         true,
			Inflight:        3,
			QueueDepth:      1,
			P95LatencyMs:    18,
			ErrorRate:       0.01,
			ModelCapability: modelASet,
		},
	}

	chosen, err := lb.Route(context.Background(), req, nodes)
	if err != nil {
		log.Fatalf("route failed: %v", err)
	}

	fmt.Printf("chosen=%s score=%.2f reason=%v\n", chosen.Node.NodeID, chosen.Score, chosen.Reason)
}
```

### 3) Run ready-to-use examples

```bash
go run ./examples/basic-routing
go run ./examples/algorithm-matrix
go run ./examples/objective-routing
go run ./examples/fallback-routing
go run ./examples/telemetry-sink
```

Examples overview:

- `examples/basic-routing`: minimal and stable generic routing.
- `examples/algorithm-matrix`: quick runnable matrix for `rr`, `wrr`, `p2c`, `lr`, `ch`.
- `examples/objective-routing`: objective-enabled LLM prefill routing.
- `examples/fallback-routing`: policy failure path with fallback chain.
- `examples/telemetry-sink`: custom telemetry sink integration.

## Reliability by Design

- **Filter-first guarantees**: unhealthy nodes or unavailable model routes fail fast with typed errors.
- **Policy-safe routing**: policy failures or empty policy outputs trigger fallback.
- **Objective-safe routing**: objective timeout/failure degrades to policy-ranked candidate.
- **Telemetry-safe execution**: telemetry callback panic never breaks routing.

Common typed errors:

- `ErrInvalidConfig`
- `ErrNoHealthyNodes`
- `ErrNoModelAvailable`
- `ErrNoCandidate`
- `ErrPluginTimeout`

## Config Experience

Core config pattern: `DefaultConfig + Option + Validate`

Frequently used options:

- `WithTopK(v int)`
- `WithAlgorithm(routeClass, pluginName string)`
- `WithPolicies(names ...string)`
- `WithObjective(name string, timeoutMs int, enabled bool)`
- `WithObjectiveMaxConcurrent(v int)`
- `WithWeight(routeClass, metric string, w int)`
- `WithSnapshotTTLGuard(enabled bool)`
- `WithTelemetrySink(s telemetry.Sink)`

Default policy chain (LLM-aware by default):

- `health_gate -> tenant_quota -> llm_token_aware_queue -> llm_stage_aware -> llm_kv_affinity`

Objective guard tuning notes:

- `timeoutMs` valid range: `1..200`.
- `maxConcurrent` valid range when set: `1..2048`; default is `128`.
- Prefill/decode requests apply stage-aware timeout scaling based on token size before objective execution.

Request-level KV affinity hint:

- metadata key: `llm_kv_affinity_preferred_nodes`
- value format: node id list split by comma / semicolon / whitespace, e.g. `node-a,node-b`
- effect: hinted nodes are prioritized before global `KVCacheHitRate` sorting for `llm-prefill` and `llm-decode`.

Snapshot freshness contract:

- `types.NodeSnapshot.FreshnessTTLms > 0`: snapshot is fresh and eligible for routing.
- `types.NodeSnapshot.FreshnessTTLms <= 0`: snapshot is stale and can be filtered when `WithSnapshotTTLGuard(true)` is enabled.

Validation includes:

- static schema checks powered by `validator/v10` (pinned to `v10.27.0`)
- validator checks are used on config boundaries, not on the `Route` hot path
- route class legality and algorithm binding completeness
- fallback chain legality
- BPS weight bounds and per-route-class weight sum
- required LLM metrics (`ttft`, `tpot`, `kv_hit`)
- aggregated multi-error return via `errors.Join`

Optional input boundary checks:

- `types.RequestContext.Validate()`
- `types.NodeSnapshot.Validate()`

## API Reference

- GoDoc: <https://pkg.go.dev/github.com/shengyanli1982/go-loadbalancer>

## DeepWiki

- <https://deepwiki.com/shengyanli1982/go-loadbalancer>
