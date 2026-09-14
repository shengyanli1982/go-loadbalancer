package examples

import (
	"fmt"

	"github.com/shengyanli1982/go-loadbalancer/lb"
)

func ExampleNewIPHash() {
	selector := lb.NewIPHash()
	backends := []lb.Backend{
		lb.NewBackend("192.168.1.1:8080"),
		lb.NewBackend("192.168.1.2:8080"),
		lb.NewBackend("192.168.1.3:8080"),
	}

	// 相同 IP 恒定路由到相同后端；不同 IP 按哈希分散（下面三个 IP 恰好落在三个不同后端）
	clientIP1 := []byte("10.0.0.4")
	clientIP2 := []byte("10.0.0.5")
	clientIP3 := []byte("10.0.0.6")

	backend1 := selector.SelectByHash(backends, clientIP1)
	backend2 := selector.SelectByHash(backends, clientIP2)
	backend3 := selector.SelectByHash(backends, clientIP3)

	fmt.Println(backend1.Address())
	fmt.Println(backend2.Address())
	fmt.Println(backend3.Address())
	// Output:
	// 192.168.1.2:8080
	// 192.168.1.3:8080
	// 192.168.1.1:8080
}
