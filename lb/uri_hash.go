package lb

import "bytes"

// uriHash 实现 URI 哈希负载均衡算法
// 相同 URI 请求始终路由到同一后端，提高后端缓存命中率
// 可选择包含或排除查询参数进行哈希计算
type uriHash struct {
	excludeQuery bool // true: 仅对 ? 前的路径部分哈希；false: 哈希完整 URI（含查询参数）
}

// URIHashOptions configures the URI-hash selector.
//
// URIHashOptions 配置 URI 哈希选择器的选项
// 零值即默认行为：NewURIHash(&URIHashOptions{}) 与 NewURIHash(nil) 完全等价。
type URIHashOptions struct {
	// ExcludeQuery reports whether the query string is left out of the hash input.
	//
	// ExcludeQuery 控制是否将查询参数（?key=value）排除在哈希计算之外：
	// false（零值，默认）为哈希完整 URI，仅 query 不同的请求路由到不同后端；
	// true 为仅对 ? 之前的路径部分哈希，query 不同的同路径请求路由到同一后端。
	ExcludeQuery bool
}

// NewURIHash creates a URI-hash selector; a nil or zero-valued opts hashes the full URI.
//
// NewURIHash 创建 URI 哈希选择器
// opts 为 nil 或零值时均包含查询参数
func NewURIHash(opts *URIHashOptions) HashSelector {
	h := &uriHash{}
	if opts != nil {
		h.excludeQuery = opts.ExcludeQuery
	}
	return h
}

// SelectByHash 根据 URI 的哈希值选择后端
// key 应为请求 URI 的字节表示（如 []byte("/api/users?page=1")）
// 当 excludeQuery 为 true 时，自动截取 ? 前的路径部分进行哈希
func (h *uriHash) SelectByHash(backends []Backend, key []byte) Backend {
	if len(backends) == 0 {
		return nil
	}
	if len(key) == 0 {
		return backends[0]
	}

	hashKey := key
	if h.excludeQuery {
		if idx := bytes.IndexByte(key, '?'); idx >= 0 {
			hashKey = key[:idx]
		}
	}

	h2 := hash64(hashKey)
	i := h2 % uint64(len(backends))
	return backends[i]
}
