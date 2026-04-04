# 任务完成检查
- 变更最小化且仅触达目标模块。
- 执行 `go test ./...` 保证功能回归。
- 对性能相关变更执行 `go test -bench . -benchmem` 验证 CPU/内存指标可观测。
- 如涉及并发路径，补充 `go test -race ./...`。