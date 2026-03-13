package builtin

import (
	// 通过匿名导入触发各内置插件的 init 注册逻辑。
	_ "github.com/shengyanli1982/go-loadbalancer/plugin/algorithm/leastrequest"
	_ "github.com/shengyanli1982/go-loadbalancer/plugin/algorithm/p2c"
	_ "github.com/shengyanli1982/go-loadbalancer/plugin/objective/weighted"
	_ "github.com/shengyanli1982/go-loadbalancer/plugin/policy/healthgate"
	_ "github.com/shengyanli1982/go-loadbalancer/plugin/policy/tenantquota"
)
