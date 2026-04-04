package examples

import (
	"fmt"

	"github.com/shengyanli1982/go-loadbalancer/lb"
)

func ExampleNewIPHash() {
	selector := lb.NewIPHash()
	backends := []lb.Backend{
		&simpleBackend{addr: "192.168.1.1:8080"},
		&simpleBackend{addr: "192.168.1.2:8080"},
		&simpleBackend{addr: "192.168.1.3:8080"},
	}

	clientIP1 := []byte("10.0.0.1")
	clientIP2 := []byte("10.0.0.2")
	clientIP3 := []byte("10.0.0.3")

	backend1 := selector.SelectByHash(backends, clientIP1)
	backend2 := selector.SelectByHash(backends, clientIP2)
	backend3 := selector.SelectByHash(backends, clientIP3)

	fmt.Println(backend1.Address())
	fmt.Println(backend2.Address())
	fmt.Println(backend3.Address())
}
