package examples

import (
	"fmt"

	"github.com/shengyanli1982/go-loadbalancer/lb"
)

func ExampleNewMaglev() {
	selector := lb.NewMaglev(nil)
	backends := []lb.Backend{
		lb.NewBackend("192.168.1.1:8080"),
		lb.NewBackend("192.168.1.2:8080"),
		lb.NewBackend("192.168.1.3:8080"),
	}

	// 相同 key 恒定查表到相同后端；不同 key 按 Maglev 查找表分散
	key := []byte("session-1")
	backend := selector.SelectByHash(backends, key)
	fmt.Println(backend.Address())

	key2 := []byte("session-2")
	backend2 := selector.SelectByHash(backends, key2)
	fmt.Println(backend2.Address())
	// Output:
	// 192.168.1.2:8080
	// 192.168.1.3:8080
}
