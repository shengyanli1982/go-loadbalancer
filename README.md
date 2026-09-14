<div align="center">

# A2X Load Balancer

**A Go library of production-grade load balancing algorithms — pure computation, zero side effects.**

</div>

[![Go Report Card](https://goreportcard.com/badge/github.com/shengyanli1982/go-loadbalancer)](https://goreportcard.com/report/github.com/shengyanli1982/go-loadbalancer)
[![Build Status](https://github.com/shengyanli1982/go-loadbalancer/actions/workflows/test.yaml/badge.svg)](https://github.com/shengyanli1982/go-loadbalancer)
[![Go Reference](https://pkg.go.dev/badge/github.com/shengyanli1982/go-loadbalancer.svg)](https://pkg.go.dev/github.com/shengyanli1982/go-loadbalancer)
[![Ask DeepWiki](https://deepwiki.com/badge.svg)](https://deepwiki.com/shengyanli1982/go-loadbalancer)

## Features

- **14 Algorithms** — Round Robin, Weighted, Hash, Consistent Hash, LeastConn, P2C, Least Time, ARB, Rendezvous
- **Zero Allocation** — every hot path is allocation-free. All 115 hot-path benchmark entries (345 samples) in [`.bench/baseline.txt`](.bench/baseline.txt) measured `0 B/op, 0 allocs/op`
- **Thread Safe** — all selectors are goroutine-safe, and CI runs the whole suite under `-race` on every push and pull request
- **Lock-free Consistent Hashing** — Maglev, Ring Hash and Rendezvous publish immutable snapshots behind an atomic pointer, so concurrent reads take no lock
- **Adaptive Scaling** — O(log n) indexed heap for large backend sets (≥32 backends; see `TreeThresholdLeastConn` / `TreeThresholdARB`)
- **Small API** — 8 exported interfaces, of which 4 are the vocabulary you actually have to learn (`Selector`, `HashSelector`, `Backend`, `WeightedBackend`); the other 4 are capability combinations or single-method declarations
- **One Module Dependency** — `xxhash/v2` is the only module dependency; the package additionally linknames `runtime.nanotime` (P2C's amortized decay clock), a symbol the Go runtime explicitly commits to keeping stable ([golang/go#67401](https://github.com/golang/go/issues/67401), see `time_nofake.go` in the runtime source)

## Requirements

Go 1.23+

## Supported Algorithms

| Algorithm               | Type              | Best For                                                          |
| ----------------------- | ----------------- | ----------------------------------------------------------------- |
| **Round Robin**         | Round Robin       | Equal-capacity backends, general purpose                          |
| **Random**              | Probabilistic     | Stateless random distribution                                     |
| **Weighted RR**         | Weighted          | Traffic proportional to backend capacity (O(log n) binary search) |
| **Smooth Weighted RR**  | Smooth Weighted   | Even traffic spread across weighted backends (avoids burst)       |
| **EDF**                 | Smooth Weighted   | Strict proportional scheduling via min-heap (Traefik style)       |
| **Least Connections**   | Least Connections | Varying request durations across backends                         |
| **P2C**                 | Dual Selection    | Large-scale microservices, lock-free concurrent selection         |
| **Least Time**          | Latency Aware     | Route by observed latency × connections (Traefik v3.6 style)      |
| **Active Request Bias** | Adaptive          | Tunable weight↔load blend via `bias ∈ (0, 1]` (Envoy WLR)         |
| **IP Hash**             | Hash              | Session persistence by client IP                                  |
| **URI Hash**            | Hash              | Cache-friendly routing by request URI                             |
| **Ring Hash**           | Consistent Hash   | Minimal key redistribution when backends change                   |
| **Maglev**              | Consistent Hash   | O(1) lookup with stable backend sets                              |
| **Rendezvous (HRW)**    | Consistent Hash   | Simple weighted consistent hashing, ~1/N key migration            |

## Choosing an Algorithm

| My scenario                                                 | Use                 |
| ----------------------------------------------------------- | ------------------- |
| All backends are the same                                   | Round Robin         |
| I just want random splitting                                | Random              |
| Traffic strictly proportional to weight                     | Weighted RR         |
| Bigger backends should get more traffic, without bursts     | Smooth Weighted RR  |
| Need strict proportional scheduling (O(log n) scaling)      | EDF                 |
| Same client must hit the same backend                       | IP Hash / URI Hash  |
| Backends handle requests at different speeds                | Least Connections   |
| Backends report their own latency and in-flight counts      | Least Time          |
| Thousands of backends, uneven load (microservices)          | P2C                 |
| Blend weight and live load, tunable via `bias` (Envoy WLR)  | Active Request Bias |
| Simple weighted consistent hashing with ~1/N redistribution | Rendezvous          |
| Adding/removing backends without reshuffling traffic        | Ring Hash           |
| Fast lookup, backend set rarely changes                     | Maglev              |

## Quick Start

```bash
go get github.com/shengyanli1982/go-loadbalancer
```

```go
package main

import (
	"fmt"

	"github.com/shengyanli1982/go-loadbalancer/lb"
)

func main() {
	backends := []lb.Backend{
		lb.NewBackend("192.168.1.1:8080"),
		lb.NewBackend("192.168.1.2:8080"),
		lb.NewBackend("192.168.1.3:8080"),
	}

	// Basic selection
	selector := lb.NewRoundRobin()
	for i := 0; i < 5; i++ {
		fmt.Println(selector.Select(backends).Address())
	}

	// Hash-based selection (session persistence)
	hashSelector := lb.NewIPHash()
	clientIP := []byte("10.0.0.1")
	fmt.Println(hashSelector.SelectByHash(backends, clientIP).Address())

	// Consistent hashing (resilient to node changes)
	ringSelector := lb.NewRingHash(nil)
	key := []byte("user-123-session")
	fmt.Println(ringSelector.SelectByHash(backends, key).Address())
}
```

Every `Select` and every `SelectByHash` implementation returns `nil` when `backends` is empty, so a nil check on the result is enough — there is no separate "select or nil" helper.

### Weighted Backends

Use `NewWeightedBackend` for weighted algorithms. A weight `<= 0` is treated as `1`.

Keep weights within the int32 range (`<= 2^31-1`). The library enforces no upper bound: the weighted paths of LeastConn and ARB compare int64 cross-products (`conn × weight` and `weight × (conn+1)`), and SmoothWeightedRR accumulates `totalWeight` as an `int` — huge weights overflow these computations and corrupt the selection order (an overflowed negative `totalWeight` even turns SmoothWeightedRR's post-selection subtraction into an addition). Within the int32 range the arithmetic stays overflow-free unless a single backend holds 2^32+ in-flight requests, which cannot happen in practice.

```go
package main

import (
	"fmt"

	"github.com/shengyanli1982/go-loadbalancer/lb"
)

func main() {
	backends := []lb.Backend{
		lb.NewWeightedBackend("192.168.1.1:8080", 5),
		lb.NewWeightedBackend("192.168.1.2:8080", 3),
	}

	// Smooth Weighted Round Robin (nginx-style smoothing, avoids bursts)
	swrr := lb.NewSmoothWeightedRR()

	// EDF — strict proportional scheduling via a min-heap
	edf := lb.NewEDF()

	for i := 0; i < 8; i++ {
		fmt.Println(swrr.Select(backends).Address(), edf.Select(backends).Address())
	}
}
```

### Connection-aware Algorithms

`NewLeastConn`, `NewP2C`, `NewP2CWithOptions`, `NewActiveRequestBias` and `NewActiveRequestBiasWithOptions` return a `TrackedSelector`: `Select` increments the chosen backend's in-flight count, and `Release` decrements it. `Release` is a method on the returned interface, so **no type assertion is needed**.

The two calls must be paired, and `Release` must receive the **same backend that `Select` returned** — releasing some other backend (for example a hard-coded `backends[0]`) makes the counts drift and systematically starves the backend whose count was inflated.

```go
package main

import (
	"fmt"

	"github.com/shengyanli1982/go-loadbalancer/lb"
)

func main() {
	backends := []lb.Backend{
		lb.NewBackend("192.168.1.1:8080"),
		lb.NewBackend("192.168.1.2:8080"),
		lb.NewBackend("192.168.1.3:8080"),
	}

	selector := lb.NewLeastConn() // TrackedSelector

	for i := 0; i < 6; i++ {
		backend := selector.Select(backends)
		fmt.Println(backend.Address())

		// ... handle the request ...

		selector.Release(backend) // same backend, one Release per Select
	}
}
```

### Latency-aware (Least Time)

`LeastTime` scores each backend as `AverageLatency() × (1 + ActiveConnections()) / Weight()` and picks the lowest score. All three inputs are read from the backend object itself, so **the caller owns the metrics** by implementing `LatencyBackend`:

```go
type LatencyBackend interface {
	Backend
	ActiveConnections() int  // caller-injected in-flight count
	AverageLatency() float64 // milliseconds or microseconds — only relative order matters
}
```

The `Weight()` term in the formula does **not** come from `LatencyBackend` — it is read from `WeightedBackend`. Weighted LeastTime therefore requires a backend to implement **both** interfaces; a backend that implements only `LatencyBackend` always gets weight 1, and dividing by 1 is a no-op: the `/ Weight()` term silently has no effect for it, even in a mixed-weight set where such a backend still goes through the weighted scoring path.

```go
package main

import (
	"fmt"

	"github.com/shengyanli1982/go-loadbalancer/lb"
)

// latencyBackend implements both LatencyBackend and WeightedBackend: the caller supplies all three score inputs (latency, connections, weight).
type latencyBackend struct {
	address string
	weight  int
	conns   int
	latency float64
}

func (b *latencyBackend) Address() string         { return b.address }
func (b *latencyBackend) Weight() int             { return b.weight }
func (b *latencyBackend) ActiveConnections() int  { return b.conns }
func (b *latencyBackend) AverageLatency() float64 { return b.latency }

func main() {
	backends := []lb.Backend{
		lb.NewBackend("no-metrics:8080"), // plain Backend: no latency data at all
		&latencyBackend{address: "observed:8080", weight: 1, conns: 9, latency: 500.0},
	}

	selector := lb.NewLeastTime()
	for i := 0; i < 3; i++ {
		fmt.Println(selector.Select(backends).Address())
	}
}
```

A backend that does not implement `LatencyBackend` has no observable data, so its score is the penalty sentinel `+Inf` rather than `0`:

- As long as **any** backend in the set is observable, unobservable ones always lose and are never selected — even when the observable ones look terrible (`observed:8080` above scores 5000 and still wins).
- Only when the set contains **no** observable backend at all are they selected. Every score is then `+Inf`, which counts as a tie, and selection degrades to round-robin rotation.

`+Inf` is used instead of a large finite value because the weighted path divides the score by the weight: a finite sentinel would be scaled differently per backend, breaking the tie and degenerating into "highest weight always wins". `+Inf` divided by any finite positive weight is still `+Inf`, so the tie holds on both the equal-weight and weighted paths.

`LeastTime` keeps **no** internal connection counter — `Select` never increments anything — so `NewLeastTime` returns a plain `Selector` and has no `Release`. Decrement your own `ActiveConnections()` metric when the request finishes.

### Active Request Bias (Envoy WLR)

ARB scores each backend as `Weight() / (in-flight + 1)^bias` and picks the **highest** score. `bias` accepts values in **(0, 1]**:

- `bias = 1` — standard LeastConn behaviour (the default)
- `0 < bias < 1` — smooth blend between weight and live load

`bias = 0` is **not** a valid value. Omitting `Bias`, passing `&ARBOptions{}` (Go's zero value) or passing any value `<= 0` all fall back to `1.0`. That fallback is deliberate: at `bias = 0` the formula reduces to `score = weight`, which would pin every request to the single highest-weight backend instead of spreading traffic proportionally. **If you want traffic proportional to weight, use `NewWeightedRR()`.**

```go
package main

import (
	"fmt"

	"github.com/shengyanli1982/go-loadbalancer/lb"
)

func main() {
	backends := []lb.Backend{
		lb.NewWeightedBackend("192.168.1.1:8080", 5),
		lb.NewWeightedBackend("192.168.1.2:8080", 1),
	}

	// bias = 1.0 (standard LeastConn behaviour)
	defaultSel := lb.NewActiveRequestBias()

	// bias = 0.5 softens the influence of the in-flight count
	softSel := lb.NewActiveRequestBiasWithOptions(&lb.ARBOptions{Bias: 0.5})

	// Bias: 0 is not valid — falls back to 1.0, same as a nil *ARBOptions
	fallbackSel := lb.NewActiveRequestBiasWithOptions(&lb.ARBOptions{})

	for _, sel := range []lb.TrackedSelector{defaultSel, softSel, fallbackSel} {
		backend := sel.Select(backends)
		fmt.Println(backend.Address())
		sel.Release(backend) // Release is a static method on TrackedSelector
	}
}
```

## Performance

Measured on **Apple M1 Max (10 logical CPUs), darwin/arm64, go1.25.13, GOMAXPROCS=10**, on **2026-09-10**. Every figure below is the median of `-count=3` samples; the raw output of all 3 samples for all 125 benchmark entries is in [`.bench/baseline.txt`](.bench/baseline.txt), together with the exact commands to reproduce it.

This host shows tight within-run variance but drifts up to ~20% between sessions, so single-run numbers are not usable for conclusions here — only the multi-sample medians are. Do not compare these figures against baselines collected on other machines.

Baseline backend counts: **100** for Round Robin / Random / Weighted RR / Smooth Weighted RR / EDF / LeastConn / P2C / ARB / Least Time; **50** for the hash family (marked `*`). Scaling sweeps over 10–1000 backends are available via `go test -run='^$' -bench=_Ext -benchmem ./lb/`.

### Single-goroutine

| Algorithm             | ns/op | B/op | allocs/op |
| --------------------- | ----- | ---- | --------- |
| Random                | 6.871 | 0    | 0         |
| Round Robin           | 7.216 | 0    | 0         |
| Maglev \*             | 11.35 | 0    | 0         |
| Weighted RR           | 14.18 | 0    | 0         |
| IP Hash \*            | 15.31 | 0    | 0         |
| URI Hash \*           | 16.34 | 0    | 0         |
| Ring Hash \*          | 19.38 | 0    | 0         |
| P2C                   | 20.97 | 0    | 0         |
| Least Connections     | 42.46 | 0    | 0         |
| EDF                   | 59.40 | 0    | 0         |
| Active Request Bias ‡ | 60.65 | 0    | 0         |
| Smooth Weighted RR    | 145.9 | 0    | 0         |
| Rendezvous \* †       | 179.1 | 0    | 0         |
| Least Time ‡          | 355.0 | 0    | 0         |

`\*` Measured via `SelectByHash` on 50 backends.
`†` Weighted variant (Rendezvous over 50 mixed-weight backends).
`‡` Measured via `ARB_Ext/100_backends` and `LeastTime_Ext/100_backends` (both 100 backends, mixed weights); dedicated `BenchmarkARB` and `BenchmarkLeastTime` baselines were added after this measurement session.

The `Select` wrapper of the consistent-hash family generates a random key internally before routing, so it costs more than calling `SelectByHash` with a key you already have. On 50 equal-weight backends, `Select` vs `SelectByHash`: Maglev **18.51** vs **11.35 ns**, Ring Hash **75.30** vs **19.38 ns**, Rendezvous **139.4** vs **78.06 ns**.

`Select` + `Release` pair cost, single-goroutine, 100 backends — each timed iteration performs one `Select` followed by one `Release`, so these are **pair** costs, not the cost of `Release` alone: LeastConn **61.64 ns**, P2C **30.07 ns**, Active Request Bias **64.33 ns** — all `0 B/op, 0 allocs/op`. `LeastTime` has no `Release`.

### Concurrent (b.RunParallel, GOMAXPROCS=10)

`ns/op` here is aggregate wall time divided by the total number of operations across all P goroutines, so for a lock-free algorithm it reads **lower** than the single-goroutine figure above. Lower is better.

| Algorithm                               | ns/op  | B/op | allocs/op |
| --------------------------------------- | ------ | ---- | --------- |
| Random                                  | 0.9369 | 0    | 0         |
| IP Hash \*                              | 1.851  | 0    | 0         |
| URI Hash \*                             | 2.220  | 0    | 0         |
| Maglev \*                               | 5.249  | 0    | 0         |
| Ring Hash \*                            | 5.300  | 0    | 0         |
| P2C                                     | 27.82  | 0    | 0         |
| Rendezvous                              | 29.41  | 0    | 0         |
| P2C (Select+Release pair)               | 42.58  | 0    | 0         |
| Round Robin                             | 64.67  | 0    | 0         |
| Weighted RR                             | 118.3  | 0    | 0         |
| Least Connections                       | 158.0  | 0    | 0         |
| EDF                                     | 184.7  | 0    | 0         |
| Active Request Bias                     | 189.2  | 0    | 0         |
| Least Connections (Select+Release pair) | 263.2  | 0    | 0         |
| Smooth Weighted RR                      | 332.4  | 0    | 0         |
| Least Time                              | 482.4  | 0    | 0         |

`\*` `SelectByHash` on 50 backends; every other row uses `Select` on 100 backends.

#### Lock-free reads for the consistent-hash family

Maglev, Ring Hash and Rendezvous now publish their lookup structures as immutable snapshots behind an atomic pointer, so the read path takes no lock at all. Concurrent `Select`/`SelectByHash` got **≈28× faster for Maglev** (145.8 → 5.249 ns/op), **≈29× for Ring Hash** (154.0 → 5.300 ns/op) and **≈5× for Rendezvous** (146.2 → 29.41 ns/op). The three "before" figures come from an earlier sampling session on this same host; they are not part of `baseline.txt`, which holds post-change data only.

The same conclusion is checkable inside `baseline.txt` alone, with no reliance on the "before" figures: the *concurrent* aggregate now beats the *single-goroutine* cost for all three — Maglev 11.35 → 5.249 ns/op and Ring Hash 19.38 → 5.300 ns/op (both `SelectByHash`, same 50 backends), Rendezvous 169.6 → 29.41 ns/op (100 backends; the concurrent row uses the `Select` wrapper, which does strictly more work per call than the single-goroutine `SelectByHash` it is compared against, so that ratio is conservative). Aggregate throughput exceeding a single core's throughput is only possible when readers never serialise.

The trade lands on the cold path. An immutable snapshot cannot reuse the previous structure's capacity, so rebuild allocations went back up — see the table below. That is a deliberate trade: rebuild runs only when the backend set changes (seconds to minutes apart in production), while `Select` runs on every request.

### Rebuild cost (backend-set change, 50 backends)

The fast paths above assume a stable backend slice. When the slice content changes, internal structures are rebuilt once (slow path). Each timed iteration alternates between two distinct 50-backend slices so the rebuild happens every time:

| Algorithm           | ns/op   | B/op   | allocs/op |
| ------------------- | ------- | ------ | --------- |
| Weighted RR         | 1088    | 0      | 0         |
| Smooth Weighted RR  | 1134    | 0      | 0         |
| EDF                 | 1170    | 0      | 0         |
| Rendezvous          | 1493    | 944    | 3         |
| P2C                 | 2484    | 3304   | 7         |
| Least Connections   | 6999    | 16     | 1         |
| Least Time          | 7015    | 0      | 0         |
| Active Request Bias | 7250    | 16     | 1         |
| Ring Hash           | 452367  | 188913 | 21        |
| Maglev              | 1111849 | 533458 | 3         |

Ring Hash rebuild is ≈452 µs and Maglev ≈1.11 ms per topology change.

> Zero allocations on every hot path (`Select` / `SelectByHash` / `Release`) — verified across all 115 hot-path entries in `baseline.txt`. Allocations above happen only on the cold rebuild path, when the backend set actually changes.
> Ring Hash rebuild hashes `n × VirtualNodes` (100 by default) vnodes and sorts them; Maglev fills a 65537-slot table. Prefer them for stable sets and expect a one-time spike per topology change.
> IP Hash and URI Hash are stateless hash lookups with no shared mutable state — under contention they stay at 1.851 and 2.220 ns/op respectively.

### Reuse your backend slice

All figures above assume the **same** `[]Backend` slice is passed on every call. The change-detection fast path is keyed on the slice's backing-array address and length, so building a fresh slice per request — even with identical content — misses it every time and degrades each call to the slow path: an O(n) fingerprint pass followed by a serialized metadata update. Identical content never triggers a structural rebuild, but the per-request slow path alone is enough to wreck concurrent throughput, and the cost lands differently per family:

- **RCU selectors** (Maglev, Ring Hash, Rendezvous, P2C): the fast path is a lock-free atomic-pointer read, but the slow path forces every request through the rebuild mutex and atomically publishes a new snapshot copy — the lock-free read advantage is forfeited entirely.
- **Mutex selectors** (LeastConn, ARB, LeastTime, EDF, Weighted RR, Smooth Weighted RR): `Select` already holds a mutex end to end, so the extra cost is the O(n) fingerprint computed inside the lock.

Reuse one slice and replace it only when the backend set actually changes.

## API

Eight exported interfaces. Four carry the vocabulary; the rest are combinations or single-capability declarations.

```go
// Selection capabilities
type Selector interface {
	Select(backends []Backend) Backend
}
type HashSelector interface {
	SelectByHash(backends []Backend, key []byte) Backend
}

// Selector + HashSelector. Returned by NewMaglev, NewRingHash, NewRendezvous.
type ConsistentHashSelector interface {
	Selector
	HashSelector
}

// Release one in-flight request previously assigned by Select.
// The three implementers count different things — LeastConn counts in-flight
// connections, P2C counts decayed load, Active Request Bias counts active
// requests — hence "Request", not "Conn".
type RequestReleaser interface {
	Release(backend Backend)
}

// Selector + RequestReleaser. Select and Release must be paired.
// Returned by NewLeastConn, NewP2C, NewP2CWithOptions,
// NewActiveRequestBias, NewActiveRequestBiasWithOptions.
type TrackedSelector interface {
	Selector
	RequestReleaser
}

// Backend hierarchy
type Backend interface {
	Address() string
}
type WeightedBackend interface {
	Backend
	Weight() int
}
type LatencyBackend interface {
	Backend
	ActiveConnections() int
	AverageLatency() float64
}
```

`RequestReleaser` and `TrackedSelector` exist so that `Release` is a **static** method on the value a constructor hands back. Earlier revisions required asserting a selector to another algorithm's interface name before calling `Release`; that is gone. Both `Select` and `SelectByHash` return `nil` for an empty backend slice, so no separate nil-safe helper is needed either.

### Constructors

```go
// Backends
func NewBackend(address string) Backend
func NewWeightedBackend(address string, weight int) WeightedBackend

// Return Selector
func NewRoundRobin() Selector
func NewRandom() Selector
func NewRandomWithSeed(seed int64) Selector
func NewWeightedRR() Selector
func NewSmoothWeightedRR() Selector
func NewEDF() Selector
func NewLeastTime() Selector

// Return TrackedSelector — Release needs no type assertion
func NewLeastConn() TrackedSelector
func NewP2C() TrackedSelector
func NewP2CWithOptions(opts *P2COptions) TrackedSelector
func NewActiveRequestBias() TrackedSelector
func NewActiveRequestBiasWithOptions(opts *ARBOptions) TrackedSelector

// Return HashSelector
func NewIPHash() HashSelector
func NewURIHash(opts *URIHashOptions) HashSelector

// Return ConsistentHashSelector
func NewMaglev(opts *MaglevOptions) ConsistentHashSelector
func NewRingHash(opts *RingHashOptions) ConsistentHashSelector
func NewRendezvous() ConsistentHashSelector
```

No constructor returns an `error`. Invalid configuration falls back to a default instead of failing.

Four constructor conventions coexist and are deliberately **not** unified:

| Convention             | Meaning                      | Constructors                                                                                                                                                                   |
| ---------------------- | ---------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| No arguments           | nothing to tune              | `NewRoundRobin`, `NewRandom`, `NewWeightedRR`, `NewSmoothWeightedRR`, `NewEDF`, `NewLeastConn`, `NewP2C`, `NewLeastTime`, `NewActiveRequestBias`, `NewIPHash`, `NewRendezvous` |
| `opts` passed directly | `nil` means defaults         | `NewMaglev(nil)`, `NewRingHash(nil)`, `NewURIHash(nil)`                                                                                                                        |
| `WithOptions` variant  | paired with a no-arg version | `NewP2CWithOptions`, `NewActiveRequestBiasWithOptions`                                                                                                                         |
| `WithSeed` variant     | reproducible randomness      | `NewRandomWithSeed`                                                                                                                                                            |

`NewP2C` and `NewActiveRequestBias` appear in two rows on purpose: both the no-argument form and the `WithOptions` form exist for them.

### Options and constants

```go
type P2COptions struct {
	Decay float64 // load decay factor; used only when 0 < Decay < 1, otherwise 0.9
}

type ARBOptions struct {
	Bias float64 // (0, 1]; out-of-range (<= 0 or > 1) falls back to 1.0
}

type RingHashOptions struct {
	RingSize     int // Deprecated: ignored. Use VirtualNodes.
	VirtualNodes int // vnodes per backend; used only when 0 < n <= 1<<20, otherwise DefaultVirtualNodes
}

type MaglevOptions struct {
	TableSize int // lookup table size; used only when 2 <= n <= 1<<23, rounded up to a prime; otherwise DefaultMaglevTableSize
}

type URIHashOptions struct {
	ExcludeQuery bool // zero value hashes the query string too; true hashes only the path before '?'
}
```

```go
const DefaultVirtualNodes    = 100   // vnodes per backend for Ring Hash
const TreeThresholdLeastConn = 32    // at or above this many backends: O(log n) indexed heap
const TreeThresholdARB       = 32    // same threshold for Active Request Bias
const DefaultMaglevTableSize = 65537 // prime
```

These four constants are the complete exported constant set. For the full symbol-by-symbol listing, see [pkg.go.dev](https://pkg.go.dev/github.com/shengyanli1982/go-loadbalancer/lb).

### Caller contract

- **Do not mutate the elements** of a `backends` slice in place after passing it to `Select`. Change detection has a fast path keyed on the slice's backing-array address and length; replacing an element changes neither, so the selector keeps using stale internal state. Build a **new slice** to change the backend set — then keep reusing that same slice on subsequent calls (see [Reuse your backend slice](#reuse-your-backend-slice) for the cost of building a fresh one per request).
- **Backend addresses must be unique** within `backends`. Duplicate addresses are not supported: `Select` increments per-position counts for each duplicate slot, while `Release` decrements through an address→position map that keeps only the **last** position for a duplicate address. Earlier duplicate slots therefore accumulate ghost counts that no `Release` can ever drain, and the drift never self-heals. Applies to LeastConn and ARB (via the shared connection tracker) and to P2C.
- **Pass non-empty hash keys.** All five hash algorithms (IP Hash, URI Hash, Ring Hash, Maglev, Rendezvous) deterministically return `backends[0]` when `len(key) == 0`. The behaviour is consistent by design, not a bug — but empty-key traffic concentrates on the first backend as a hotspot.
- **Normalize `net.IP` before hashing.** The same IPv4 address has two legal byte forms — 4-byte (`To4()`) and 16-byte (4-in-6, e.g. from `ParseIP`) — and hashing its `String()` output yields a third, textual form. The hash is byte-sensitive: mixing sources routes the same client IP to different backends and silently breaks session affinity. Canonicalize the key before calling `SelectByHash` (e.g. always hash the bytes of `ip.String()`, or always `To4()` with a `To16()` fallback); the library does not normalize for you.
- **Keep `LatencyBackend` callbacks non-blocking.** `LeastTime` calls `AverageLatency()` and `ActiveConnections()` inside its internal mutex on every `Select` — the only algorithm that runs user code while holding a hot-path lock. A blocking callback stalls all concurrent `Select` calls on that selector, and a callback that re-enters the same selector deadlocks immediately (`sync.Mutex` is not reentrant). Implement both as pure non-blocking reads (e.g. `atomic.Load`).
- **Keep weights in the int32 range** (`<= 2^31-1`); the library enforces no upper bound. See [Weighted Backends](#weighted-backends) for the overflow consequences.

## Examples

See [`lb/examples`](./lb/examples) for a complete runnable example of each algorithm. All 14 are testable examples with an `// Output:` comment, so `go test ./lb/examples/` actually executes them rather than only compiling them.

## Testing

```bash
go test ./...                        # all tests
go test -race ./...                  # race detection
go test -cover ./...                 # coverage
go test ./lb/examples/               # runnable examples (all 14 have // Output:)
go test -run='^$' -bench=. -benchmem ./lb/   # benchmarks
```

CI runs `gofmt` (as a hard assertion), `go vet`, `-race` and `-cover` across Go 1.23 / 1.24 / 1.25 on Ubuntu, macOS and Windows, on every push and pull request to `dev` or `main`. The `-race` step runs on Ubuntu; Windows race detection needs MinGW gcc and is not reliable there.

## Learn More

- [API Reference](https://pkg.go.dev/github.com/shengyanli1982/go-loadbalancer)
- [DeepWiki](https://deepwiki.com/shengyanli1982/go-loadbalancer)

## License

[MIT](./LICENSE) — Copyright (c) 2026 shengyanli1982
