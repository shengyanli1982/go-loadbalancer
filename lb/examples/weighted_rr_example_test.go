package examples

import (
	"fmt"

	"github.com/shengyanli1982/go-loadbalancer/lb"
)

func ExampleNewWeightedRR() {
	selector := lb.NewWeightedRR()
	backends := []lb.Backend{
		&simpleWeightedBackend{addr: "192.168.1.1:8080", weight: 3},
		&simpleWeightedBackend{addr: "192.168.1.2:8080", weight: 1},
		&simpleWeightedBackend{addr: "192.168.1.3:8080", weight: 1},
	}

	for i := 0; i < 10; i++ {
		backend := selector.Select(backends)
		fmt.Println(backend.Address())
	}
}
