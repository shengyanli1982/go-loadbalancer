package examples

import (
	"fmt"

	"github.com/shengyanli1982/go-loadbalancer/lb"
)

func ExampleNewRandom() {
	// 生产代码用 NewRandom()（全局熵播种源，结果不可复现）；此处用固定种子构造器使随机结果可复现。
	//
	// 可移植性约束：math/rand/v2 的带种子序列隐含「64 位平台」假设——Rand.IntN 底层在
	// 32 位平台走不同采样分支（$GOROOT/src/math/rand/v2/rand.go 的 is32bit 判定），
	// 同一种子在 32/64 位平台产生不同序列，下方 // Output: 断言仅在 64 位平台成立。
	// 当前 CI 矩阵（ubuntu-latest/amd64、macos-15/arm64、windows-latest/amd64）全为 64 位；
	// 将来若向 CI 加入 32 位 GOARCH（如 386），本示例会因序列差异失败，那不是库缺陷。
	selector := lb.NewRandomWithSeed(42)
	backends := []lb.Backend{
		lb.NewBackend("192.168.1.1:8080"),
		lb.NewBackend("192.168.1.2:8080"),
		lb.NewBackend("192.168.1.3:8080"),
	}

	for i := 0; i < 5; i++ {
		backend := selector.Select(backends)
		fmt.Println(backend.Address())
	}
	// Output:
	// 192.168.1.2:8080
	// 192.168.1.3:8080
	// 192.168.1.3:8080
	// 192.168.1.2:8080
	// 192.168.1.3:8080
}
