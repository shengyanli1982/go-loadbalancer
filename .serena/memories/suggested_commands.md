# 常用命令 (Windows/PowerShell)
- 运行全部测试: `go test ./...`
- 运行竞态检查: `go test -race ./...`
- 运行基准测试(含 alloc): `go test -bench . -benchmem ./...`
- 仅运行某包基准: `go test -bench . -benchmem ./balancer ./registry`
- 格式化: `gofmt -w <files>`
- 快速文本检索: `rg <pattern>`