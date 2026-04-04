package examples

import (
	"fmt"

	"github.com/shengyanli1982/go-loadbalancer/lb"
)

func ExampleNewURIHash() {
	selector := lb.NewURIHash(nil)
	backends := []lb.Backend{
		&simpleBackend{addr: "192.168.1.1:8080"},
		&simpleBackend{addr: "192.168.1.2:8080"},
		&simpleBackend{addr: "192.168.1.3:8080"},
	}

	uri1 := []byte("/api/users")
	uri2 := []byte("/api/products")
	uri3 := []byte("/api/orders")

	backend1 := selector.SelectByHash(backends, uri1)
	backend2 := selector.SelectByHash(backends, uri2)
	backend3 := selector.SelectByHash(backends, uri3)

	fmt.Println(backend1.Address())
	fmt.Println(backend2.Address())
	fmt.Println(backend3.Address())
}
