package lb

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 本文件钉住包公开 API 的接口契约（编译期断言为主，运行时断言为回归护栏）。
//
// 导出接口全集（8 个），与实测能力矩阵一一对应：
//
//	选择器侧（selector.go）：
//	  - Selector: Select 能力（除 IPHash、URIHash 外的 12 个算法满足；该二者为纯 HashSelector，仅 SelectByHash）
//	  - HashSelector: SelectByHash 能力（IPHash、URIHash、Maglev、RingHash、Rendezvous）
//	  - ConsistentHashSelector: Selector + HashSelector（Maglev、RingHash、Rendezvous 构造返回类型）
//	  - RequestReleaser: Release 能力（LeastConn、P2C、ARB）
//	  - TrackedSelector: Selector + RequestReleaser（LeastConn、P2C、ARB 构造返回类型）
//
//	后端侧（backend.go）：
//	  - Backend: Address（所有算法的最小后端接口）
//	  - WeightedBackend: Backend + Weight（WeightedRR、SmoothWeightedRR、LeastConn 等）
//	  - LatencyBackend: Backend + ActiveConnections + AverageLatency（LeastTime）

// 编译期契约断言：连接跟踪类算法构造函数返回值静态满足 TrackedSelector，
// 调用方无需类型断言即可直接调用 Release（本次重构的核心收益）。
// 若构造函数返回类型退化（如改回 Selector），此处直接编译失败。
var (
	_ TrackedSelector = NewLeastConn()
	_ TrackedSelector = NewP2C()
	_ TrackedSelector = NewP2CWithOptions(nil)
	_ TrackedSelector = NewActiveRequestBias()
	_ TrackedSelector = NewActiveRequestBiasWithOptions(nil)
)

// 编译期契约断言：一致性哈希类算法构造函数返回值静态满足 ConsistentHashSelector，
// 调用方无需类型断言即可直接调用 Select 与 SelectByHash。
var (
	_ ConsistentHashSelector = NewMaglev(nil)
	_ ConsistentHashSelector = NewRingHash(nil)
	_ ConsistentHashSelector = NewRendezvous()
)

// 编译期契约断言：LeastTime 是纯 Selector（Release 已移除，不携带连接跟踪语义）
var _ Selector = NewLeastTime()

// TestAPISurface_ExportedInterfaceSet 断言导出接口全集恰为 8 个且与能力矩阵一一对应。
// 用 go/parser 解析包源码 AST（排除 _test.go），防止接口集合被意外增删。
func TestAPISurface_ExportedInterfaceSet(t *testing.T) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi fs.FileInfo) bool {
		return !strings.HasSuffix(fi.Name(), "_test.go")
	}, 0)
	require.NoError(t, err, "解析包目录不应失败")
	p := pkgs["lb"]
	require.NotNil(t, p, "应能解析出 lb 包的 AST")

	var got []string
	for _, f := range p.Files {
		for _, decl := range f.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.TYPE {
				continue
			}
			for _, spec := range gd.Specs {
				ts, ok := spec.(*ast.TypeSpec)
				if !ok {
					continue
				}
				if _, isIface := ts.Type.(*ast.InterfaceType); isIface && token.IsExported(ts.Name.Name) {
					got = append(got, ts.Name.Name)
				}
			}
		}
	}

	want := []string{
		"Selector", "HashSelector", "ConsistentHashSelector", "RequestReleaser", "TrackedSelector",
		"Backend", "WeightedBackend", "LatencyBackend",
	}
	assert.ElementsMatch(t, want, got,
		"导出接口集合应恰为 8 个且与能力矩阵一一对应（多退少补都须显式更新本契约）")
}

// TestAPISurface_TrackedSelectorStaticRelease 验证连接跟踪类算法可在静态类型上直接
// Select→Release 成对调用，全程零类型断言。
func TestAPISurface_TrackedSelectorStaticRelease(t *testing.T) {
	selectors := map[string]TrackedSelector{
		"least_conn": NewLeastConn(),
		"p2c":        NewP2C(),
		"arb":        NewActiveRequestBias(),
	}
	backends := []Backend{NewBackend("a:80"), NewBackend("b:80")}

	for name, s := range selectors {
		t.Run(name, func(t *testing.T) {
			b := s.Select(backends)
			require.NotNil(t, b, "Select 不应返回 nil")
			// 静态调用 Release（无需类型断言）
			s.Release(b)
			// nil 释放应为空操作，不得 panic
			assert.NotPanics(t, func() { s.Release(nil) }, "Release(nil) 应为空操作")
		})
	}
}

// TestAPISurface_ConsistentHashStaticSelectByHash 验证一致性哈希类算法可在静态类型上
// 直接调用 Select 与 SelectByHash，全程零类型断言，且同 key 路由亲和性保持不变。
func TestAPISurface_ConsistentHashStaticSelectByHash(t *testing.T) {
	selectors := map[string]ConsistentHashSelector{
		"maglev":     NewMaglev(nil),
		"ring_hash":  NewRingHash(nil),
		"rendezvous": NewRendezvous(),
	}
	backends := []Backend{NewBackend("a:80"), NewBackend("b:80"), NewBackend("c:80")}

	for name, s := range selectors {
		t.Run(name, func(t *testing.T) {
			b := s.Select(backends)
			require.NotNil(t, b, "Select 不应返回 nil")
			// 静态调用 SelectByHash（无需类型断言）：同 key 两次必须路由到同一后端
			k1 := s.SelectByHash(backends, []byte("affinity-key"))
			k2 := s.SelectByHash(backends, []byte("affinity-key"))
			require.NotNil(t, k1, "SelectByHash 不应返回 nil")
			assert.Equal(t, k1.Address(), k2.Address(), "同 key 应路由到同一后端")
		})
	}
}

// TestAPISurface_LeastTimeNotRequestReleaser 回归护栏：LeastTime 的 Release 已移除
// （范畴错误：其评分只读外部注入指标 ActiveConnections/AverageLatency，
// 不维护内部连接计数，「释放 Select 时递增的计数」这一前提对它不成立），
// 不得再满足 RequestReleaser。
func TestAPISurface_LeastTimeNotRequestReleaser(t *testing.T) {
	_, ok := any(NewLeastTime()).(RequestReleaser)
	assert.False(t, ok, "NewLeastTime 返回值不应再实现 RequestReleaser（Release 已移除）")
}
