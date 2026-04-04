package examples

import (
	"fmt"

	"github.com/shengyanli1982/go-loadbalancer/lb"
)

func ExampleNewLeastConn() {
	selector := lb.NewLeastConn()
	backends := []lb.Backend{
		&simpleBackend{addr: "192.168.1.1:8080"},
		&simpleBackend{addr: "192.168.1.2:8080"},
		&simpleBackend{addr: "192.168.1.3:8080"},
	}

	for i := 0; i < 5; i++ {
		backend := selector.Select(backends)
		fmt.Println(backend.Address())
	}

	if releaser, ok := selector.(lb.LeastConnReleaser); ok {
		releaser.Release(backends[0])
	}
}
