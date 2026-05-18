# 编码风格与约定
- 使用 Go idiomatic 风格，错误优先返回，不忽略 error。
- 命名遵循导出 PascalCase、非导出 camelCase。
- 导出符号应有注释；实现关注并发安全与内存效率。
- 测试使用 `_test.go`，性能路径应提供 Benchmark 并配合 `b.ReportAllocs()` 观察分配。