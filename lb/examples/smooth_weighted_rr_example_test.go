package examples

import (
	"fmt"

	"github.com/shengyanli1982/go-loadbalancer/lb"
)

func ExampleNewSmoothWeightedRR() {
	selector := lb.NewSmoothWeightedRR()
	backends := []lb.Backend{
		lb.NewWeightedBackend("192.168.1.1:8080", 6),
		lb.NewWeightedBackend("192.168.1.2:8080", 3),
		lb.NewWeightedBackend("192.168.1.3:8080", 1),
	}

	for i := 0; i < 10; i++ {
		backend := selector.Select(backends)
		fmt.Println(backend.Address())
	}
}
