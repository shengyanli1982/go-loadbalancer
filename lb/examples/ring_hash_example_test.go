package examples

import (
	"fmt"

	"github.com/shengyanli1982/go-loadbalancer/lb"
)

func ExampleNewRingHash() {
	selector := lb.NewRingHash(nil)
	backends := []lb.Backend{
		lb.NewBackend("192.168.1.1:8080"),
		lb.NewBackend("192.168.1.2:8080"),
		lb.NewBackend("192.168.1.3:8080"),
	}

	// 相同 key 恒定落在哈希环上同一后端；不同 key 沿环分散
	key := []byte("cart-1001")
	backend := selector.SelectByHash(backends, key)
	fmt.Println(backend.Address())

	key2 := []byte("cart-1002")
	backend2 := selector.SelectByHash(backends, key2)
	fmt.Println(backend2.Address())
	// Output:
	// 192.168.1.3:8080
	// 192.168.1.2:8080
}
