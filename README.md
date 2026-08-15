<div align="center">

# A2X Load Balancer

![Logo](assets/logo.png)

**A Go library of production-grade load balancing algorithms — pure computation, zero side effects.**

</div>

[![Go Report Card](https://goreportcard.com/badge/github.com/shengyanli1982/go-loadbalancer)](https://goreportcard.com/report/github.com/shengyanli1982/go-loadbalancer)
[![Build Status](https://github.com/shengyanli1982/go-loadbalancer/actions/workflows/test.yaml/badge.svg)](https://github.com/shengyanli1982/go-loadbalancer)
[![Go Reference](https://pkg.go.dev/badge/github.com/shengyanli1982/go-loadbalancer.svg)](https://pkg.go.dev/github.com/shengyanli1982/go-loadbalancer)
[![Ask DeepWiki](https://deepwiki.com/badge.svg)](https://deepwiki.com/shengyanli1982/go-loadbalancer)

## Features

- **14 Algorithms** — Round Robin, Weighted, Hash, Consistent Hash, LeastConn, P2C, Least Time, ARB, Rendezvous
- **Zero Allocation** — all selectors allocate nothing on the fast path
- **Thread Safe** — all selectors are goroutine-safe
- **Adaptive Scaling** — O(log n) indexed binary heap / min-heap for large backend sets (≥32 backends)
- **Minimal API** — `Selector` / `HashSelector` / `WeightedBackend` / `LatencyBackend`, four interfaces to learn
- **One Dependency** — `xxhash/v2` only

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
| **Active Request Bias** | Adaptive          | Tunable WRR↔LeastConn blend via bias parameter (Envoy WLR)        |
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
| Bigger backends should get more traffic                     | Smooth Weighted RR  |
| Need strict proportional scheduling (O(log n) scaling)      | EDF                 |
| Same client must hit the same backend                       | IP Hash / URI Hash  |
| Backends handle requests at different speeds                | Least Connections   |
| Route by observed latency, not just connection count        | Least Time          |
| Thousands of backends, uneven load (microservices)          | P2C                 |
| Gradual transition from WRR to LeastConn (Envoy WLR)        | Active Request Bias |
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

### Weighted Backends

Use `NewWeightedBackend` for weighted algorithms:

```go
backends := []lb.Backend{
	lb.NewWeightedBackend("192.168.1.1:8080", 5),
	lb.NewWeightedBackend("192.168.1.2:8080", 3),
}

// Smooth Weighted Round Robin (nginx-style smoothing, avoids burst)
swrr := lb.NewSmoothWeightedRR()
fmt.Println(swrr.Select(backends).Address())

// EDF — strict proportional scheduling via min-heap
edf := lb.NewEDF()
fmt.Println(edf.Select(backends).Address())
```

### Connection-aware Algorithms

LeastConn and ARB track an internal connection counter. Call `Release` when a request completes to decrement the counter:

```go
selector := lb.NewLeastConn()
backend := selector.Select(backends)

// ... handle request ...

if releaser, ok := selector.(lb.LeastConnReleaser); ok {
	releaser.Release(backend)
}
```

### Latency-aware (Least Time)

`LeastTime` requires backends implementing `LatencyBackend` to inject observed latency and connection counts:

```go
// Backend must implement LatencyBackend:
type LatencyBackend interface {
	Backend
	ActiveConnections() int  // caller-injected active connection count
	AverageLatency() float64 // milliseconds or microseconds (relative ordering matters)
}

selector := lb.NewLeastTime()
backend := selector.Select(latencyBackends)
```

`LeastTime` does not keep an internal connection counter — connection counts come from each backend's `ActiveConnections()` (caller-injected). It implements `LeastConnReleaser` for interface compatibility, but `Release` does not affect its selection.

### Active Request Bias (Envoy WLR)

ARB smoothly interpolates between Weighted RR (bias=0) and standard LeastConn (bias=1):

```go
// Default: bias=1.0 (standard LeastConn behavior)
selector := lb.NewActiveRequestBias()

// Custom: bias=0.5 (softer influence of connection count)
selector = lb.NewActiveRequestBiasWithOptions(&lb.ARBOptions{Bias: 0.5})
```

## Performance

Benchmark results on Intel Core i5-12400F (windows/amd64), Go 1.25, GOMAXPROCS=12.
Baseline backend counts: **100** for Round Robin / Random / Weighted RR / Smooth Weighted RR / LeastConn / P2C / EDF; **50** for the hash family (marked `*`). Scaling tests (10–1000 backends) are available via `go test -bench=_Ext ./lb/`. Full baseline: [`.bench/baseline.txt`](.bench/baseline.txt).

### Single-goroutine

| Algorithm           | ns/op | B/op | allocs/op |
| ------------------- | ----- | ---- | --------- |
| Round Robin         | 4.7   | 0    | 0         |
| IP Hash             | 5.7   | 0    | 0         |
| URI Hash            | 6.7   | 0    | 0         |
| Random              | 7.0   | 0    | 0         |
| Maglev \*           | 15.1  | 0    | 0         |
| Weighted RR         | 15.9  | 0    | 0         |
| Ring Hash \*        | 22.6  | 0    | 0         |
| P2C                 | 23.5  | 0    | 0         |
| Least Connections   | 53.0  | 0    | 0         |
| EDF                 | 69.4  | 0    | 0         |
| Active Request Bias | 75.8  | 0    | 0         |
| Smooth Weighted RR  | 144.5 | 0    | 0         |
| Rendezvous \* †     | 282.8 | 0    | 0         |
| Least Time ‡        | 300.2 | 0    | 0         |

`*` Uses `SelectByHash`, 50 backends.
`†` Weighted variant (Rendezvous with mixed weights).
`‡` Measured via `LeastTime_Ext/100_backends` (no fixed-key base benchmark); Active Request Bias via `ARB_Ext/100_backends`.

`Release` cost (per call, 100 backends, single-goroutine): LeastConn **61.1 ns**, P2C **31.3 ns** — both 0 allocs/op.

### Concurrent (b.RunParallel, GOMAXPROCS=12)

| Algorithm                        | ns/op | B/op | allocs/op |
| -------------------------------- | ----- | ---- | --------- |
| IP Hash                          | 1.1   | 0    | 0         |
| Random                           | 1.2   | 0    | 0         |
| URI Hash                         | 1.2   | 0    | 0         |
| P2C                              | 11.6  | 0    | 0         |
| P2C (Select+Release pair)        | 16.6  | 0    | 0         |
| Round Robin                      | 18.3  | 0    | 0         |
| Weighted RR                      | 45.0  | 0    | 0         |
| Maglev \*                        | 46.8  | 0    | 0         |
| Ring Hash \*                     | 53.2  | 0    | 0         |
| Rendezvous                       | 59.9  | 0    | 0         |
| Least Connections                | 74.5  | 0    | 0         |
| EDF                              | 94.1  | 0    | 0         |
| Active Request Bias              | 98.0  | 0    | 0         |
| Least Connections (Select+Release pair) | 99.9 | 0    | 0         |
| Smooth Weighted RR               | 173.6 | 0    | 0         |
| Least Time                       | 349.0 | 0    | 0         |

### Rebuild cost (backend-set change, 50 backends)

Fast paths above assume a stable backend slice. When the slice content changes, internal structures are rebuilt once (slow path):

| Algorithm          | ns/op (rebuild) | B/op    | allocs/op |
| ------------------ | --------------- | ------- | --------- |
| Weighted RR        | 1.2µs           | 0       | 0         |
| Smooth Weighted RR | 1.2µs           | 0       | 0         |
| Rendezvous         | 1.3µs           | 0       | 0         |
| Least Connections  | 7.7µs           | 2.3KB   | 6         |
| Ring Hash          | 450µs           | 116B    | 1         |
| Maglev             | 3.1ms           | 534KB   | 4         |

> Zero allocations on every hot path (Select / SelectByHash / Release); rebuild allocations happen only on the cold path when the backend set actually changes.
> Ring Hash rebuild builds n×virtualNodes hashed vnodes and sorts them; Maglev fills a 65537-slot table — prefer them for stable sets, and expect a one-time rebuild spike per topology change.
> IP Hash and URI Hash are stateless hash lookups (no shared mutex) — consistently ~1 ns/op under contention.

## API

```go
// Core interfaces
type Selector interface     { Select(backends []Backend) Backend }
type HashSelector interface { SelectByHash(backends []Backend, key []byte) Backend }

// Backend hierarchy
type Backend interface          { Address() string }
type WeightedBackend interface  { Backend; Weight() int }
type LatencyBackend interface   { Backend; ActiveConnections() int; AverageLatency() float64 }

// Connection release (LeastConn, ARB; no-op for LeastTime)
type LeastConnReleaser interface { Release(backend Backend) }
type P2CReleaser interface       { Release(backend Backend) }

// Factory functions
func NewBackend(address string) Backend
func NewWeightedBackend(address string, weight int) WeightedBackend
func SelectOrNil(s Selector, backends []Backend) Backend

// Algorithm constructors
func NewRoundRobin() Selector
func NewRandom() Selector
func NewWeightedRR() Selector
func NewSmoothWeightedRR() Selector
func NewEDF() Selector
func NewLeastConn() Selector
func NewP2C() Selector
func NewP2CWithOptions(opts *P2COptions) Selector
func NewLeastTime() Selector
func NewActiveRequestBias() Selector
func NewActiveRequestBiasWithOptions(opts *ARBOptions) Selector
func NewIPHash() HashSelector
func NewURIHash(opts *URIHashOptions) HashSelector
func NewRingHash(opts *RingHashOptions) RingHashSelector
func NewMaglev(opts *MaglevOptions) MaglevSelector
func NewRendezvous() RendezvousSelector

// Options
type P2COptions struct    { Decay float64 }
type ARBOptions struct    { Bias float64 }
type RingHashOptions struct { RingSize, VirtualNodes int }
type MaglevOptions struct { TableSize int }
type URIHashOptions struct { IncludeQuery bool }
```

## Examples

See [`lb/examples`](./lb/examples) for complete runnable examples of each algorithm.

## Testing

```bash
go test ./...                        # all tests
go test -race ./...                  # race detection
go test -bench=. -benchmem ./lb/...  # benchmarks
```

## Learn More

- [API Reference](https://pkg.go.dev/github.com/shengyanli1982/go-loadbalancer)
- [DeepWiki](https://deepwiki.com/shengyanli1982/go-loadbalancer)

## License

[MIT](./LICENSE)
