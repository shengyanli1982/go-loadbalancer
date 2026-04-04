# A2X Load Balancer

<div align="center">

![Logo](assets/logo.png)

**Production-ready load balancing algorithm library for Go services, providing efficient traffic distribution.**

</div>

[![Go Report Card](https://goreportcard.com/badge/github.com/shengyanli1982/go-loadbalancer)](https://goreportcard.com/report/github.com/shengyanli1982/go-loadbalancer)
[![Build Status](https://github.com/shengyanli1982/go-loadbalancer/actions/workflows/test.yaml/badge.svg)](https://github.com/shengyanli1982/go-loadbalancer)
[![Go Reference](https://pkg.go.dev/badge/github.com/shengyanli1982/go-loadbalancer.svg)](https://pkg.go.dev/github.com/shengyanli1982/go-loadbalancer)
[![Ask DeepWiki](https://deepwiki.com/badge.svg)](https://deepwiki.com/shengyanli1982/go-loadbalancer)

## Why Go Load Balancer

When your service needs to distribute traffic across multiple backend instances, Go Load Balancer provides a rich set of load balancing algorithms while maintaining a clean API design.

- **Rich Algorithms**: Supports 10+ load balancing algorithms, from simple round-robin to advanced consistent hashing.
- **High Performance**: Uses `maphash` for hashing and `sync.Pool` for object reuse, minimizing memory allocations.
- **Thread Safe**: All algorithms are designed for concurrent safety, usable directly in Goroutines.
- **Clean Interface**: Unified `Selector` interface, easy to understand and integrate.
- **Extensible**: Supports custom Backend interface and Hash Selector.

## Supported Algorithms

| Algorithm                      | Type              | Description                                                  |
| ------------------------------ | ----------------- | ------------------------------------------------------------ |
| **Random**                     | Probabilistic     | Randomly selects a backend, simple and efficient             |
| **Round Robin**                | Round Robin       | Sequentially iterates through all backends in a cycle        |
| **Weighted Round Robin**       | Weighted          | Distributes traffic proportional to weights                  |
| **Smooth Weighted RR**         | Smooth Weighted   | Nginx-style smooth weighted algorithm, smoother distribution |
| **Least Connections**          | Least Connections | Selects backend with fewest current connections              |
| **P2C (Power of Two Choices)** | Dual Selection    | Randomly picks two, selects the less loaded one              |
| **IP Hash**                    | Hash              | Hash-based distribution using client IP                      |
| **URI Hash**                   | Hash              | Hash-based distribution using request URI                    |
| **Ring Hash**                  | Consistent Hash   | Virtual node ring, resilient to node changes                 |
| **Maglev**                     | Consistent Hash   | Google paper implementation, fast lookup                     |

## Quick Start

```bash
go get github.com/shengyanli1982/go-loadbalancer
```

### Basic Usage

```go
package main

import (
	"fmt"

	"github.com/shengyanli1982/go-loadbalancer/lb"
)

type backend struct {
	addr string
}

func (b *backend) Address() string {
	return b.addr
}

func main() {
	selector := lb.NewRoundRobin()
	backends := []lb.Backend{
		&backend{addr: "192.168.1.1:8080"},
		&backend{addr: "192.168.1.2:8080"},
		&backend{addr: "192.168.1.3:8080"},
	}

	for i := 0; i < 5; i++ {
		backend := selector.Select(backends)
		fmt.Println(backend.Address())
	}
}
```

### Using Weighted Backends

```go
selector := lb.NewWeightedRR()
backends := []lb.Backend{
	&weightedBackend{addr: "192.168.1.1:8080", weight: 3},
	&weightedBackend{addr: "192.168.1.2:8080", weight: 1},
}

backend := selector.Select(backends)
fmt.Println(backend.Address())
```

### Using Hash Algorithms

```go
selector := lb.NewRingHash(nil)
backends := []lb.Backend{
	&backend{addr: "192.168.1.1:8080"},
	&backend{addr: "192.168.1.2:8080"},
}

key := []byte("user-123-session")
backend := selector.SelectByHash(backends, key)
fmt.Println(backend.Address())
```

## Algorithm Details

### Random

```go
selector := lb.NewRandom()
```

- **Pros**: Simple implementation, stateless
- **Cons**: Cannot guarantee even request distribution
- **Use case**: Backends have consistent performance, random distribution is acceptable

### Round Robin

```go
selector := lb.NewRoundRobin()
```

- **Pros**: Simple implementation, even request distribution
- **Cons**: Cannot account for backend performance differences
- **Use case**: Backends have consistent performance

### Weighted Round Robin

```go
selector := lb.NewWeightedRR()
```

- **Pros**: Distributes traffic proportional to weights
- **Cons**: May select high-weight backends consecutively
- **Use case**: Backends have different performance, need proportional distribution

### Smooth Weighted RR

```go
selector := lb.NewSmoothWeightedRR()
```

- **Pros**: Smoother distribution, avoids consecutive selection of same backend
- **Cons**: Slightly higher implementation complexity
- **Use case**: Production environments requiring even distribution

### Least Connections

```go
selector := lb.NewLeastConn()

// Release connection after request completes
if releaser, ok := selector.(lb.LeastConnReleaser); ok {
	releaser.Release(backend)
}
```

- **Pros**: Dynamically adapts to backend load
- **Cons**: Requires maintaining connection state
- **Use case**: Large variation in backend processing times

### P2C (Power of Two Choices)

```go
selector := lb.NewP2C()
```

- **Pros**: Balances randomness and load balancing
- **Cons**: Requires maintaining load state
- **Use case**: Large-scale distributed systems

### IP Hash

```go
selector := lb.NewIPHash()

clientIP := []byte("10.0.0.1")
backend := selector.SelectByHash(backends, clientIP)
```

- **Pros**: Same IP requests route to same backend
- **Cons**: May rebalance when backends change
- **Use case**: Scenarios requiring session persistence

### URI Hash

```go
selector := lb.NewURIHash(nil)

// Optional: include query parameters
selector := lb.NewURIHash(&lb.URIHashOptions{IncludeQuery: true})

uri := []byte("/api/users")
backend := selector.SelectByHash(backends, uri)
```

- **Pros**: Same URI requests route to same backend
- **Cons**: May rebalance when backends change
- **Use case**: Cache-friendly routing

### Ring Hash (Consistent Hash)

```go
selector := lb.NewRingHash(nil)

// Optional: custom configuration
selector := lb.NewRingHash(&lb.RingHashOptions{
	VirtualNodes: 150,
})

backend := selector.SelectByHash(backends, key)
```

- **Pros**: Minimal impact when nodes change
- **Cons**: Higher implementation complexity
- **Use case**: Nodes change frequently

### Maglev

```go
selector := lb.NewMaglev(nil)

// Optional: custom table size
selector := lb.NewMaglev(&lb.MaglevOptions{
	TableSize: 65537,
})

backend := selector.SelectByHash(backends, key)
```

- **Pros**: O(1) lookup speed, space efficient
- **Cons**: Table building takes time
- **Use case**: High performance requirements

## Interface Design

### Backend Interface

```go
type Backend interface {
	Address() string
}

type WeightedBackend interface {
	Backend
	Weight() int
}
```

### Selector Interface

```go
type Selector interface {
	Select(backends []Backend) Backend
}

type HashSelector interface {
	SelectByHash(backends []Backend, key []byte) Backend
}
```

### Helper Functions

```go
func SelectOrNil(s Selector, backends []Backend) Backend {
	if len(backends) == 0 {
		return nil
	}
	return s.Select(backends)
}
```

## Constants

```go
const (
	MaxBackends     = 65536
	MinRingSize     = 128
	MaxRingSize     = 1 << 20
	DefaultRingSize = 1 << 16
	MaglevTableSize = 65537
	MinWeight       = 1
	MaxWeight       = 65535
)

const (
	P2CTrials = 2
	P2CBias   = 0.0
)
```

## Error Handling

```go
var ErrNilBackends = &Error{
	Code:    ErrCodeNilBackends,
	Message: "backend list is nil or empty",
}

func ErrInvalidWeight(cause error) *Error
func ErrInvalidRingSize(cause error) *Error
```

## Examples

Complete examples are available in the [`lb/examples`](./lb/examples) directory:

- [`random_example_test.go`](./lb/examples/random_example_test.go) - Random algorithm example
- [`round_robin_example_test.go`](./lb/examples/round_robin_example_test.go) - Round Robin algorithm example
- [`weighted_rr_example_test.go`](./lb/examples/weighted_rr_example_test.go) - Weighted RR example
- [`smooth_weighted_rr_example_test.go`](./lb/examples/smooth_weighted_rr_example_test.go) - Smooth weighted RR example
- [`least_conn_example_test.go`](./lb/examples/least_conn_example_test.go) - Least connections example
- [`p2c_example_test.go`](./lb/examples/p2c_example_test.go) - P2C algorithm example
- [`ip_hash_example_test.go`](./lb/examples/ip_hash_example_test.go) - IP Hash example
- [`uri_hash_example_test.go`](./lb/examples/uri_hash_example_test.go) - URI Hash example
- [`ring_hash_example_test.go`](./lb/examples/ring_hash_example_test.go) - Ring Hash example
- [`maglev_example_test.go`](./lb/examples/maglev_example_test.go) - Maglev example

## Run Tests

```bash
go test ./... -v
go test ./... -race
```

## Benchmark

```bash
go test -bench=. -benchmem ./lb/...
```

## Learn More

- DeepWiki (full guides and architecture): <https://deepwiki.com/shengyanli1982/go-loadbalancer>
- Go API reference: <https://pkg.go.dev/github.com/shengyanli1982/go-loadbalancer>

## License

[MIT](./LICENSE)
