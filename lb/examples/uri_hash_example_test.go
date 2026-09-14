package examples

import (
	"fmt"

	"github.com/shengyanli1982/go-loadbalancer/lb"
)

func ExampleNewURIHash() {
	backends := []lb.Backend{
		lb.NewBackend("192.168.1.1:8080"),
		lb.NewBackend("192.168.1.2:8080"),
		lb.NewBackend("192.168.1.3:8080"),
	}

	page1 := []byte("/api/users?page=1")
	page2 := []byte("/api/users?page=2")

	// 默认（ExcludeQuery 零值 false）：哈希完整 URI（含查询参数），
	// 同路径不同 query 的两个请求可以落到不同后端
	defaultSel := lb.NewURIHash(nil)
	fmt.Println(defaultSel.SelectByHash(backends, page1).Address())
	fmt.Println(defaultSel.SelectByHash(backends, page2).Address())

	// ExcludeQuery=true：仅对 '?' 之前的路径部分哈希，
	// 两个请求的哈希 key 相同，必然落到同一后端
	pathSel := lb.NewURIHash(&lb.URIHashOptions{ExcludeQuery: true})
	fmt.Println(pathSel.SelectByHash(backends, page1).Address())
	fmt.Println(pathSel.SelectByHash(backends, page2).Address())
	// Output:
	// 192.168.1.1:8080
	// 192.168.1.2:8080
	// 192.168.1.3:8080
	// 192.168.1.3:8080
}
