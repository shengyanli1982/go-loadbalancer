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

| Benchmark                                                |  ns/op |  B/op | allocs/op |
| -------------------------------------------------------- | -----: | ----: | --------: |
| `BenchmarkRoute/serial_nodes_32`                         |   1646 |  2816 |         8 |
| `BenchmarkRoute/serial_nodes_256`                        |   9077 |  2816 |         8 |
| `BenchmarkRoute/serial_nodes_1024`                       |  34308 |  2816 |         8 |
| `BenchmarkRoute/parallel_nodes_256`                      |   2180 |  2816 |         8 |
| `BenchmarkRoute/serial_default_config_nodes_256`         |   8845 |  3712 |        10 |
| `BenchmarkRoute/serial_objective_enabled_nodes_256`      |  11950 |  3672 |        15 |
| `BenchmarkRoute/serial_fallback_policy_ranked_nodes_256` |  20043 |  2841 |         9 |
| `BenchmarkSelectCandidates/nodes_1024_topk_8` (`rr`)     |  365.8 |  1664 |         9 |
| `BenchmarkSelectCandidates/nodes_1024_topk_8` (`wrr`)    |  19608 |  2688 |        10 |
| `BenchmarkSelectCandidates/nodes_1024_topk_8` (`ch`)     | 183983 | 29152 |        14 |
| `BenchmarkSelectCandidates/nodes_1024_topk_8` (`p2c`)    |  37424 |  2944 |        10 |
| `BenchmarkSelectCandidates/nodes_1024_topk_8` (`lr`)     |  38748 |  3072 |        10 |
| `BenchmarkChoose` (`plugin/objective/weighted`)          |  337.2 |    16 |         1 |
| `BenchmarkManagerGetAlgorithm/hit_serial`                |  16.75 |     0 |         0 |
| `BenchmarkManagerHasAlgorithm/hit_serial`                |  16.59 |     0 |         0 |

Algorithm deep-dive benchmark command:

```bash
go test -run ^$ -bench BenchmarkSelectCandidates -benchmem ./plugin/algorithm/rr ./plugin/algorithm/wrr ./plugin/algorithm/consistenthash ./plugin/algorithm/p2c ./plugin/algorithm/leastrequest
```

| Algorithm | Scenario             |  ns/op |  B/op | allocs/op |
| --------- | -------------------- | -----: | ----: | --------: |
| `rr`      | `nodes_32_topk_1`    |  66.86 |   208 |         2 |
| `rr`      | `nodes_32_topk_8`    |  376.5 |  1664 |         9 |
| `rr`      | `nodes_256_topk_8`   |  362.8 |  1664 |         9 |
| `rr`      | `nodes_1024_topk_8`  |  365.8 |  1664 |         9 |
| `rr`      | `nodes_1024_topk_32` |   1401 |  7168 |        33 |
| `rr`      | `nodes_4096_topk_32` |   1481 |  7168 |        33 |
| `wrr`     | `nodes_32_topk_1`    |  319.7 |   240 |         3 |
| `wrr`     | `nodes_32_topk_8`    |   1579 |  1696 |        10 |
| `wrr`     | `nodes_256_topk_8`   |   5682 |  1920 |        10 |
| `wrr`     | `nodes_1024_topk_8`  |  19608 |  2688 |        10 |
| `wrr`     | `nodes_1024_topk_32` |  69718 |  8192 |        34 |
| `wrr`     | `nodes_4096_topk_32` | 255986 | 11264 |        34 |
| `ch`      | `nodes_32_topk_1`    |   2545 |  1216 |         7 |
| `ch`      | `nodes_32_topk_8`    |   9380 |  2784 |        14 |
| `ch`      | `nodes_256_topk_8`   |  36093 |  8416 |        14 |
| `ch`      | `nodes_1024_topk_8`  | 183983 | 29152 |        14 |
| `ch`      | `nodes_1024_topk_32` | 276899 | 35040 |        38 |
| `p2c`     | `nodes_32_topk_1`    |  117.0 |   240 |         2 |
| `p2c`     | `nodes_32_topk_8`    |   1592 |  2944 |        10 |
| `p2c`     | `nodes_256_topk_8`   |   9306 |  2944 |        10 |
| `p2c`     | `nodes_1024_topk_8`  |  37424 |  2944 |        10 |
| `p2c`     | `nodes_1024_topk_32` |  25373 | 22624 |        70 |
| `p2c`     | `nodes_4096_topk_32` |  52214 | 22624 |        70 |
| `lr`      | `nodes_32_topk_1`    |  356.1 |   384 |         3 |
| `lr`      | `nodes_32_topk_8`    |   1595 |  3072 |        10 |
| `lr`      | `nodes_256_topk_8`   |  10142 |  3072 |        10 |
| `lr`      | `nodes_1024_topk_8`  |  38748 |  3072 |        10 |
| `lr`      | `nodes_1024_topk_32` |  23212 | 22768 |        71 |
| `lr`      | `nodes_4096_topk_32` |  52564 | 22768 |        71 |

Numbers are from a single local run and should be used as a baseline reference. Re-run on your target hardware for production capacity planning.

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

	nodes := []types.NodeSnapshot{
		{
			NodeID:            "node-a",
			Healthy:           true,
			Inflight:          10,
			QueueDepth:        5,
			P95LatencyMs:      30,
			ErrorRate:         0.02,
			ModelAvailability: map[string]bool{"model-a": true},
		},
		{
			NodeID:            "node-b",
			Healthy:           true,
			Inflight:          3,
			QueueDepth:        1,
			P95LatencyMs:      18,
			ErrorRate:         0.01,
			ModelAvailability: map[string]bool{"model-a": true},
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
- `WithWeight(routeClass, metric string, w int)`
- `WithTelemetrySink(s telemetry.Sink)`

Validation includes:

- route class legality and algorithm binding completeness
- fallback chain legality
- BPS weight bounds and per-route-class weight sum
- required LLM metrics (`ttft`, `tpot`, `kv_hit`)
- aggregated multi-error return via `errors.Join`
