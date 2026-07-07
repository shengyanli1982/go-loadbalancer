package lb

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLeastConn_Select(t *testing.T) {
	selector := NewLeastConn()
	backends := newTestBackends("a", "b", "c")

	result := selector.Select(backends)
	require.NotNil(t, result)
}

func TestLeastConn_NilBackends(t *testing.T) {
	selector := NewLeastConn()
	result := selector.Select(nil)
	assert.Nil(t, result)
}

func TestLeastConn_EmptyBackends(t *testing.T) {
	selector := NewLeastConn()
	result := selector.Select([]Backend{})
	assert.Nil(t, result)
}

func TestLeastConn_SingleBackend(t *testing.T) {
	selector := NewLeastConn()
	backends := newTestBackends("a")

	for i := 0; i < 5; i++ {
		result := selector.Select(backends)
		require.NotNil(t, result)
		assert.Equal(t, "a", result.Address())
	}
}

func TestP2C_Select(t *testing.T) {
	selector := NewP2C()
	backends := newTestBackends("a", "b", "c")

	result := selector.Select(backends)
	require.NotNil(t, result)
}

func TestP2C_NilBackends(t *testing.T) {
	selector := NewP2C()
	result := selector.Select(nil)
	assert.Nil(t, result)
}

func TestP2C_SingleBackend(t *testing.T) {
	selector := NewP2C()
	backends := newTestBackends("a")

	for i := 0; i < 5; i++ {
		result := selector.Select(backends)
		require.NotNil(t, result)
		assert.Equal(t, "a", result.Address())
	}
}
