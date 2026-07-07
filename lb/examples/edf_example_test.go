package examples

import (
	"fmt"

	"github.com/shengyanli1982/go-loadbalancer/lb"
)

func ExampleNewEDF() {
	selector := lb.NewEDF()
	backends := []lb.Backend{
		lb.NewWeightedBackend("192.168.1.1:8080", 5),
		lb.NewWeightedBackend("192.168.1.2:8080", 3),
		lb.NewWeightedBackend("192.168.1.3:8080", 2),
	}

	for i := 0; i < 5; i++ {
		backend := selector.Select(backends)
		fmt.Println(backend.Address())
	}
}
