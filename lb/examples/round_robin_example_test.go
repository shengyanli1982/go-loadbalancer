package examples

import (
	"fmt"

	"github.com/shengyanli1982/go-loadbalancer/lb"
)

func ExampleNewRoundRobin() {
	selector := lb.NewRoundRobin()
	backends := []lb.Backend{
		&simpleBackend{addr: "192.168.1.1:8080"},
		&simpleBackend{addr: "192.168.1.2:8080"},
		&simpleBackend{addr: "192.168.1.3:8080"},
	}

	for i := 0; i < 6; i++ {
		backend := selector.Select(backends)
		fmt.Println(backend.Address())
	}
}
