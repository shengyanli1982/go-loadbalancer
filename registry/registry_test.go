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

type algorithmStub struct{ name string }

func (s algorithmStub) Name() string { return s.name }
func (s algorithmStub) SelectCandidates(_ types.RequestContext, nodes []types.NodeSnapshot, topK int) ([]types.Candidate, error) {
	if len(nodes) == 0 || topK <= 0 {
		return nil, nil
	}
	return []types.Candidate{{Node: nodes[0]}}, nil
}

type policyStub struct{ name string }

func (s policyStub) Name() string { return s.name }
func (s policyStub) ReRank(_ types.RequestContext, candidates []types.Candidate) ([]types.Candidate, error) {
	return candidates, nil
}

type objectiveStub struct{ name string }

func (s objectiveStub) Name() string { return s.name }
func (s objectiveStub) Choose(_ types.RequestContext, candidates []types.Candidate) (types.Candidate, error) {
	if len(candidates) == 0 {
		return types.Candidate{}, nil
	}
	return candidates[0], nil
}

func TestRegisterDuplicatePlugin(t *testing.T) {
	m := registry.NewManager()
	require.NoError(t, m.RegisterAlgorithm(algorithmStub{name: "algoA"}))
	err := m.RegisterAlgorithm(algorithmStub{name: "algoA"})
	require.Error(t, err)
	assert.ErrorIs(t, err, lberrors.ErrDuplicatePlugin)
}

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

var _ algorithm.Plugin = algorithmStub{}
var _ policy.Plugin = policyStub{}
var _ objective.Plugin = objectiveStub{}
