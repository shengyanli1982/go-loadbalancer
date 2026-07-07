package examples

import (
	"fmt"

	"github.com/shengyanli1982/go-loadbalancer/lb"
)

type latencyBackend struct {
	address string
	latency float64
	conns   int
}

func (b *latencyBackend) Address() string           { return b.address }
func (b *latencyBackend) Weight() int               { return 1 }
func (b *latencyBackend) ActiveConnections() int    { return b.conns }
func (b *latencyBackend) AverageLatency() float64   { return b.latency }

func ExampleNewLeastTime() {
	selector := lb.NewLeastTime()
	backends := []lb.Backend{
		&latencyBackend{address: "192.168.1.1:8080", latency: 10.0, conns: 5},
		&latencyBackend{address: "192.168.1.2:8080", latency: 25.0, conns: 3},
		&latencyBackend{address: "192.168.1.3:8080", latency: 5.0, conns: 8},
	}

	backend := selector.Select(backends)
	fmt.Println(backend.Address())

	if releaser, ok := selector.(lb.LeastConnReleaser); ok {
		releaser.Release(backend)
	}
}
