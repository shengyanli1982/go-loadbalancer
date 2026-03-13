package builtin

import (
	_ "go-loadbalancer/plugin/algorithm/leastrequest"
	_ "go-loadbalancer/plugin/algorithm/p2c"
	_ "go-loadbalancer/plugin/objective/weighted"
	_ "go-loadbalancer/plugin/policy/healthgate"
	_ "go-loadbalancer/plugin/policy/tenantquota"
)
