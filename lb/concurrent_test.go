package lb

import (
	"sync"
	"testing"
)

func TestConcurrent_RoundRobin(t *testing.T) {
	selector := NewRoundRobin()
	backends := newTestBackends("a", "b", "c", "d", "e")
	const goroutines = 20
	const callsPerGoroutine = 50

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < callsPerGoroutine; j++ {
				b := selector.Select(backends)
				if b == nil {
					t.Error("expected non-nil backend")
				}
			}
		}()
	}
	wg.Wait()
}

func TestConcurrent_WeightedRR(t *testing.T) {
	selector := NewWeightedRR()
	backends := []Backend{
		NewWeightedBackend("a", 1),
		NewWeightedBackend("b", 3),
		NewWeightedBackend("c", 2),
	}
	const goroutines = 20
	const callsPerGoroutine = 50

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < callsPerGoroutine; j++ {
				b := selector.Select(backends)
				if b == nil {
					t.Error("expected non-nil backend")
				}
			}
		}()
	}
	wg.Wait()
}

func TestConcurrent_SmoothWeightedRR(t *testing.T) {
	selector := NewSmoothWeightedRR()
	backends := []Backend{
		NewWeightedBackend("a", 1),
		NewWeightedBackend("b", 3),
		NewWeightedBackend("c", 2),
	}
	const goroutines = 20
	const callsPerGoroutine = 50

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < callsPerGoroutine; j++ {
				b := selector.Select(backends)
				if b == nil {
					t.Error("expected non-nil backend")
				}
			}
		}()
	}
	wg.Wait()
}

func TestConcurrent_Random(t *testing.T) {
	selector := NewRandom()
	backends := newTestBackends("a", "b", "c", "d", "e")
	const goroutines = 20
	const callsPerGoroutine = 50

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < callsPerGoroutine; j++ {
				b := selector.Select(backends)
				if b == nil {
					t.Error("expected non-nil backend")
				}
			}
		}()
	}
	wg.Wait()
}

func TestConcurrent_LestConn(t *testing.T) {
	selector := NewLeastConn()
	backends := newTestBackends("a", "b", "c")
	const goroutines = 20
	const callsPerGoroutine = 50

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < callsPerGoroutine; j++ {
				b := selector.Select(backends)
				if b == nil {
					t.Error("expected non-nil backend")
				}
			}
		}()
	}
	wg.Wait()
}

func TestConcurrent_LestConn_Release(t *testing.T) {
	selector := NewLeastConn()
	releaser, ok := selector.(LeastConnReleaser)
	if !ok {
		t.Fatal("selector does not implement LeastConnReleaser")
	}
	backends := newTestBackends("a", "b", "c")
	const goroutines = 20
	const callsPerGoroutine = 50

	var wg sync.WaitGroup
	wg.Add(goroutines * 2)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < callsPerGoroutine; j++ {
				b := selector.Select(backends)
				if b == nil {
					t.Error("expected non-nil backend")
				}
				releaser.Release(b)
			}
		}()
		go func() {
			defer wg.Done()
			for j := 0; j < callsPerGoroutine; j++ {
				releaser.Release(backends[j%len(backends)])
			}
		}()
	}
	wg.Wait()
}

func TestConcurrent_P2C(t *testing.T) {
	selector := NewP2C()
	backends := newTestBackends("a", "b", "c")
	const goroutines = 20
	const callsPerGoroutine = 50

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < callsPerGoroutine; j++ {
				b := selector.Select(backends)
				if b == nil {
					t.Error("expected non-nil backend")
				}
			}
		}()
	}
	wg.Wait()
}

func TestConcurrent_P2C_Release(t *testing.T) {
	selector := NewP2C()
	releaser, ok := selector.(P2CReleaser)
	if !ok {
		t.Fatal("selector does not implement P2CReleaser")
	}
	backends := newTestBackends("a", "b", "c")
	const goroutines = 20
	const callsPerGoroutine = 50

	var wg sync.WaitGroup
	wg.Add(goroutines * 2)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < callsPerGoroutine; j++ {
				b := selector.Select(backends)
				if b == nil {
					t.Error("expected non-nil backend")
				}
				releaser.Release(b)
			}
		}()
		go func() {
			defer wg.Done()
			for j := 0; j < callsPerGoroutine; j++ {
				releaser.Release(backends[j%len(backends)])
			}
		}()
	}
	wg.Wait()
}

func TestConcurrent_RingHash_SelectByHash(t *testing.T) {
	selector := NewRingHash(&RingHashOptions{RingSize: 1024, VirtualNodes: 50})
	backends := newTestBackends("a", "b", "c")
	const goroutines = 20
	const callsPerGoroutine = 50

	keys := [][]byte{
		[]byte("key1"), []byte("key2"), []byte("key3"), []byte("key4"), []byte("key5"),
		[]byte("key6"), []byte("key7"), []byte("key8"), []byte("key9"), []byte("key10"),
	}

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < callsPerGoroutine; j++ {
				key := keys[j%len(keys)]
				b := selector.SelectByHash(backends, key)
				if b == nil {
					t.Error("expected non-nil backend")
				}
			}
		}()
	}
	wg.Wait()
}

func TestConcurrent_Maglev_SelectByHash(t *testing.T) {
	selector := NewMaglev(&MaglevOptions{TableSize: 1024})
	backends := newTestBackends("a", "b", "c")
	const goroutines = 20
	const callsPerGoroutine = 50

	keys := [][]byte{
		[]byte("key1"), []byte("key2"), []byte("key3"), []byte("key4"), []byte("key5"),
		[]byte("key6"), []byte("key7"), []byte("key8"), []byte("key9"), []byte("key10"),
	}

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < callsPerGoroutine; j++ {
				key := keys[j%len(keys)]
				b := selector.SelectByHash(backends, key)
				if b == nil {
					t.Error("expected non-nil backend")
				}
			}
		}()
	}
	wg.Wait()
}
