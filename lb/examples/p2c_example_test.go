package examples

import (
	"fmt"

	"github.com/shengyanli1982/go-loadbalancer/lb"
)

func ExampleNewP2C() {
	selector := lb.NewP2C()
	backends := []lb.Backend{
		lb.NewBackend("192.168.1.1:8080"),
		lb.NewBackend("192.168.1.2:8080"),
		lb.NewBackend("192.168.1.3:8080"),
	}

	// P2C 每次随机抽两个候选、选负载较低者，随机源（rand/v2 ChaCha8）不提供种子参数，
	// 选中的具体后端不可复现，故此处只演示 Select/Release 成对用法并输出确定性事实。
	selected := 0
	for i := 0; i < 5; i++ {
		backend := selector.Select(backends)
		if backend != nil {
			selected++
		}

		// ... 处理请求 ...

		// NewP2C 返回 TrackedSelector，Release 静态可用，无需类型断言
		selector.Release(backend)
	}

	fmt.Printf("selected %d/%d from %d backends\n", selected, 5, len(backends))
	// Output: selected 5/5 from 3 backends
}
