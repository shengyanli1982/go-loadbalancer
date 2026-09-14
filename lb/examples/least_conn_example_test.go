package examples

import (
	"fmt"

	"github.com/shengyanli1982/go-loadbalancer/lb"
)

func ExampleNewLeastConn() {
	selector := lb.NewLeastConn()
	backends := []lb.Backend{
		lb.NewBackend("192.168.1.1:8080"),
		lb.NewBackend("192.168.1.2:8080"),
		lb.NewBackend("192.168.1.3:8080"),
	}

	for i := 0; i < 5; i++ {
		backend := selector.Select(backends)
		fmt.Println(backend.Address())

		// ... 处理请求 ...

		// 释放的必须是 Select 返回的同一个后端，Select/Release 成对，
		// 否则内部连接计数单调增长，被选后端会被永久降权
		// NewLeastConn 返回 TrackedSelector，Release 静态可用，无需类型断言
		selector.Release(backend)
	}
	// Output:
	// 192.168.1.1:8080
	// 192.168.1.2:8080
	// 192.168.1.3:8080
	// 192.168.1.1:8080
	// 192.168.1.2:8080
}
