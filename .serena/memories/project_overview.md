# go-loadbalancer 概览
- 目的: 提供 A2X 负载均衡核心，覆盖 generic 与 LLM prefill/decode 路由，支持 algorithm/policy/objective 插件化与 fallback。
- 技术栈: Go 1.20，Go Modules，测试框架以标准 testing + testify。
- 关键目录: balancer(主流程), config(配置与校验), registry(插件注册中心), plugin(算法/策略/目标函数实现), telemetry(观测边界), types(核心数据结构)。
- 设计特点: default config + option + validate；路由流程为 filter -> algorithm -> policy -> objective(可选) -> fallback。