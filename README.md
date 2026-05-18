<div align="center">

# A2X Load Balancer

![Logo](assets/logo.png)

**Production-ready load balancing algorithm library for Go services, providing efficient traffic distribution.**

</div>

[![Go Report Card](https://goreportcard.com/badge/github.com/shengyanli1982/go-loadbalancer)](https://goreportcard.com/report/github.com/shengyanli1982/go-loadbalancer)
[![Build Status](https://github.com/shengyanli1982/go-loadbalancer/actions/workflows/test.yaml/badge.svg)](https://github.com/shengyanli1982/go-loadbalancer)
[![Go Reference](https://pkg.go.dev/badge/github.com/shengyanli1982/go-loadbalancer.svg)](https://pkg.go.dev/github.com/shengyanli1982/go-loadbalancer)
[![Ask DeepWiki](https://deepwiki.com/badge.svg)](https://deepwiki.com/shengyanli1982/go-loadbalancer)

## Why Go Load Balancer

When your service needs to distribute traffic across multiple backend instances, Go Load Balancer provides a rich set of load balancing algorithms while maintaining a clean API design.

- **Rich Algorithms**: Supports 10 load balancing algorithms, from simple round-robin to advanced consistent hashing.
- **High Performance**: Uses `xxhash/v2` for fast hashing and fingerprint caching to minimize memory allocations — most algorithms achieve zero allocations per call.
- **Thread Safe**: All algorithms are designed for concurrent safety, usable directly in Goroutines.
- **Clean Interface**: Unified `Selector` interface, easy to understand and integrate.
- **Extensible**: Supports custom Backend interface and Hash Selector.

## Supported Algorithms

| Algorithm                | Type              | Zero-alloc  | Use Case                                          |
| ------------------------ | ----------------- | ----------- | ------------------------------------------------- |
| **Round Robin**          | Round Robin       | ✓           | Backends have consistent performance              |
| **Random**               | Probabilistic     | ✓           | Simple distribution, no state needed              |
| **Weighted Round Robin** | Weighted          | ✗ (1 alloc) | Proportional distribution by weight               |
| **Smooth Weighted RR**   | Smooth Weighted   | ✓           | Production environments needing even distribution |
| **Least Connections**    | Least Connections | ✓           | Large variation in backend processing times       |
| **P2C**                  | Dual Selection    | ✓           | Large-scale distributed systems                   |
| **IP Hash**              | Hash              | ✓           | Session persistence by client IP                  |
| **URI Hash**             | Hash              | ✓           | Cache-friendly routing by request URI             |
| **Ring Hash**            | Consistent Hash   | ✓           | Minimal impact when nodes change                  |
| **Maglev**               | Consistent Hash   | ✓           | O(1) lookup, high throughput                      |

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

type backend struct {
	addr string
}

func (b *backend) Address() string { return b.addr }

func main() {
	backends := []lb.Backend{
		&backend{addr: "192.168.1.1:8080"},
		&backend{addr: "192.168.1.2:8080"},
		&backend{addr: "192.168.1.3:8080"},
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

Implement the `WeightedBackend` interface for weighted algorithms:

```go
type weightedBackend struct {
	addr   string
	weight int
}

func (b *weightedBackend) Address() string { return b.addr }
func (b *weightedBackend) Weight() int     { return b.weight }

selector := lb.NewSmoothWeightedRR()
backends := []lb.Backend{
	&weightedBackend{addr: "192.168.1.1:8080", weight: 3},
	&weightedBackend{addr: "192.168.1.2:8080", weight: 1},
}
```

### Connection-aware Algorithms

LeastConn and P2C support connection release:

```go
selector := lb.NewLeastConn()

// Select and release after request completes
be := selector.Select(backends)
if releaser, ok := selector.(lb.LeastConnReleaser); ok {
	releaser.Release(be)
}
```

## Performance

Benchmark results on Apple M1 Max (50-100 backends):

| Algorithm                | ns/op | B/op | allocs/op |
| ------------------------ | ----- | ---- | --------- |
| Round Robin              | 7     | 0    | 0         |
| Random                   | 14    | 0    | 0         |
| IP Hash                  | 15    | 0    | 0         |
| URI Hash                 | 16    | 0    | 0         |
| P2C                      | 107   | 0    | 0         |
| Smooth Weighted RR       | 145   | 0    | 0         |
| Maglev (SelectByHash)    | 162   | 0    | 0         |
| Weighted RR              | 416   | 896  | 1         |
| Ring Hash (SelectByHash) | 325   | 0    | 0         |
| Least Connections        | 3700  | 0    | 0         |

## Interface Design

```go
type Backend interface {
	Address() string
}

type WeightedBackend interface {
	Backend
	Weight() int
}

type ConnBackend interface {
	Backend
	ActiveConnections() int
}

type Selector interface {
	Select(backends []Backend) Backend
}

type HashSelector interface {
	SelectByHash(backends []Backend, key []byte) Backend
}

func SelectOrNil(s Selector, backends []Backend) Backend
```

## Examples

Complete runnable examples in [`lb/examples`](./lb/examples):

- [`random_example_test.go`](./lb/examples/random_example_test.go)
- [`round_robin_example_test.go`](./lb/examples/round_robin_example_test.go)
- [`weighted_rr_example_test.go`](./lb/examples/weighted_rr_example_test.go)
- [`smooth_weighted_rr_example_test.go`](./lb/examples/smooth_weighted_rr_example_test.go)
- [`least_conn_example_test.go`](./lb/examples/least_conn_example_test.go)
- [`p2c_example_test.go`](./lb/examples/p2c_example_test.go)
- [`ip_hash_example_test.go`](./lb/examples/ip_hash_example_test.go)
- [`uri_hash_example_test.go`](./lb/examples/uri_hash_example_test.go)
- [`ring_hash_example_test.go`](./lb/examples/ring_hash_example_test.go)
- [`maglev_example_test.go`](./lb/examples/maglev_example_test.go)

## Testing

```bash
go test ./... -v
go test ./... -race
```

Benchmark:

```bash
go test -bench=. -benchmem ./lb/...
```

## Learn More

- DeepWiki: <https://deepwiki.com/shengyanli1982/go-loadbalancer>
- Go API reference: <https://pkg.go.dev/github.com/shengyanli1982/go-loadbalancer>

## License

[MIT](./LICENSE)
