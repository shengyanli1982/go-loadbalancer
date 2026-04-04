package examples

import (
	"fmt"

	"github.com/shengyanli1982/go-loadbalancer/lb"
)

func ExampleNewMaglev() {
	selector := lb.NewMaglev(nil)
	backends := []lb.Backend{
		&simpleBackend{addr: "192.168.1.1:8080"},
		&simpleBackend{addr: "192.168.1.2:8080"},
		&simpleBackend{addr: "192.168.1.3:8080"},
	}

	key := []byte("maglev-hash-key")
	backend := selector.SelectByHash(backends, key)
	fmt.Println(backend.Address())

	key2 := []byte("another-key")
	backend2 := selector.SelectByHash(backends, key2)
	fmt.Println(backend2.Address())
}
