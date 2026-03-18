package registry_test

import (
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	lberrors "github.com/shengyanli1982/go-loadbalancer/errors"
	"github.com/shengyanli1982/go-loadbalancer/plugin/algorithm"
	"github.com/shengyanli1982/go-loadbalancer/plugin/objective"
	"github.com/shengyanli1982/go-loadbalancer/plugin/policy"
	"github.com/shengyanli1982/go-loadbalancer/registry"
	"github.com/shengyanli1982/go-loadbalancer/types"
)

// algorithmStub 是算法插件测试桩。
type algorithmStub struct{ name string }

// Name 返回插件名。
func (s algorithmStub) Name() string { return s.name }

// SelectCandidates 返回固定候选用于测试。
func (s algorithmStub) SelectCandidates(_ types.RequestContext, nodes []types.NodeSnapshot, topK int) ([]types.Candidate, error) {
	if len(nodes) == 0 || topK <= 0 {
		return nil, nil
	}
	return []types.Candidate{{Node: nodes[0]}}, nil
}

// policyStub 是策略插件测试桩。
type policyStub struct{ name string }

// Name 返回插件名。
func (s policyStub) Name() string { return s.name }

// ReRank 原样返回候选用于测试。
func (s policyStub) ReRank(_ types.RequestContext, candidates []types.Candidate) ([]types.Candidate, error) {
	return candidates, nil
}

// objectiveStub 是目标函数插件测试桩。
type objectiveStub struct{ name string }

// Name 返回插件名。
func (s objectiveStub) Name() string { return s.name }

// Choose 返回首个候选用于测试。
func (s objectiveStub) Choose(_ types.RequestContext, candidates []types.Candidate) (types.Candidate, error) {
	if len(candidates) == 0 {
		return types.Candidate{}, nil
	}
	return candidates[0], nil
}

// TestRegisterDuplicatePlugin 验证重复注册会返回重复错误。
func TestRegisterDuplicatePlugin(t *testing.T) {
	m := registry.NewManager()
	require.NoError(t, m.RegisterAlgorithm(algorithmStub{name: "algoA"}))
	err := m.RegisterAlgorithm(algorithmStub{name: "algoA"})
	require.Error(t, err)
	assert.ErrorIs(t, err, lberrors.ErrDuplicatePlugin)
}

// TestRegisterFactoryAndLookup 验证工厂注册后可查询且 Has 接口可见。
func TestRegisterFactoryAndLookup(t *testing.T) {
	m := registry.NewManager()
	require.NoError(t, m.RegisterAlgorithmFactory("algo_factory", func() algorithm.Plugin { return algorithmStub{name: "algo_factory"} }))
	require.NoError(t, m.RegisterPolicyFactory("policy_factory", func() policy.Plugin { return policyStub{name: "policy_factory"} }))
	require.NoError(t, m.RegisterObjectiveFactory("objective_factory", func() objective.Plugin { return objectiveStub{name: "objective_factory"} }))

	algoCtor, ok := m.GetAlgorithmFactory("algo_factory")
	require.True(t, ok)
	require.NotNil(t, algoCtor)
	assert.Equal(t, "algo_factory", algoCtor().Name())
	assert.True(t, m.HasAlgorithm("algo_factory"))

	policyCtor, ok := m.GetPolicyFactory("policy_factory")
	require.True(t, ok)
	require.NotNil(t, policyCtor)
	assert.Equal(t, "policy_factory", policyCtor().Name())
	assert.True(t, m.HasPolicy("policy_factory"))

	objectiveCtor, ok := m.GetObjectiveFactory("objective_factory")
	require.True(t, ok)
	require.NotNil(t, objectiveCtor)
	assert.Equal(t, "objective_factory", objectiveCtor().Name())
	assert.True(t, m.HasObjective("objective_factory"))
}

// TestRegisterFactoryPrototypeConflict 验证同名工厂与原型互斥注册。
func TestRegisterFactoryPrototypeConflict(t *testing.T) {
	t.Run("prototype_then_factory", func(t *testing.T) {
		m := registry.NewManager()
		require.NoError(t, m.RegisterAlgorithm(algorithmStub{name: "dup_algo"}))
		err := m.RegisterAlgorithmFactory("dup_algo", func() algorithm.Plugin { return algorithmStub{name: "dup_algo"} })
		require.Error(t, err)
		assert.ErrorIs(t, err, lberrors.ErrDuplicatePlugin)
	})

	t.Run("factory_then_prototype", func(t *testing.T) {
		m := registry.NewManager()
		require.NoError(t, m.RegisterPolicyFactory("dup_policy", func() policy.Plugin { return policyStub{name: "dup_policy"} }))
		err := m.RegisterPolicy(policyStub{name: "dup_policy"})
		require.Error(t, err)
		assert.ErrorIs(t, err, lberrors.ErrDuplicatePlugin)
	})

	t.Run("objective_conflict", func(t *testing.T) {
		m := registry.NewManager()
		require.NoError(t, m.RegisterObjective(objectiveStub{name: "dup_objective"}))
		err := m.RegisterObjectiveFactory("dup_objective", func() objective.Plugin { return objectiveStub{name: "dup_objective"} })
		require.Error(t, err)
		assert.ErrorIs(t, err, lberrors.ErrDuplicatePlugin)
	})
}

// TestConcurrentRegisterAndRead 验证并发注册与读取不会破坏可见性。
func TestConcurrentRegisterAndRead(t *testing.T) {
	m := registry.NewManager()

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = m.RegisterAlgorithm(algorithmStub{name: fmt.Sprintf("algo_%d", i)})
			_, _ = m.GetAlgorithm(fmt.Sprintf("algo_%d", i))
			_ = m.RegisterPolicy(policyStub{name: fmt.Sprintf("policy_%d", i)})
			_, _ = m.GetPolicy(fmt.Sprintf("policy_%d", i))
			_ = m.RegisterObjective(objectiveStub{name: fmt.Sprintf("objective_%d", i)})
			_, _ = m.GetObjective(fmt.Sprintf("objective_%d", i))
		}()
	}
	wg.Wait()

	assert.True(t, m.HasAlgorithm("algo_1"))
	assert.True(t, m.HasPolicy("policy_1"))
	assert.True(t, m.HasObjective("objective_1"))
}

// 编译期接口断言，确保测试桩满足插件接口约束。
var _ algorithm.Plugin = algorithmStub{}
var _ policy.Plugin = policyStub{}
var _ objective.Plugin = objectiveStub{}
