package builtin

import (
	_ "github.com/shengyanli1982/go-loadbalancer/plugin/algorithm/leastrequest"
	_ "github.com/shengyanli1982/go-loadbalancer/plugin/algorithm/p2c"
	_ "github.com/shengyanli1982/go-loadbalancer/plugin/objective/weighted"
	_ "github.com/shengyanli1982/go-loadbalancer/plugin/policy/healthgate"
	_ "github.com/shengyanli1982/go-loadbalancer/plugin/policy/tenantquota"
)
