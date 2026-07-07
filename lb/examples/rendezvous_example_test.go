package examples

import (
	"fmt"

	"github.com/shengyanli1982/go-loadbalancer/lb"
)

func ExampleNewRendezvous() {
	selector := lb.NewRendezvous()
	backends := []lb.Backend{
		lb.NewBackend("192.168.1.1:8080"),
		lb.NewBackend("192.168.1.2:8080"),
		lb.NewBackend("192.168.1.3:8080"),
	}

	key := []byte("user-session-123")
	backend := selector.SelectByHash(backends, key)
	fmt.Println(backend.Address())
}
