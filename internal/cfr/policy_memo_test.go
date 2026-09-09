package cfr

import (
	"sync"
	"testing"
)

func TestPolicyMemoConcurrentCollisions(t *testing.T) {
	pm := newPolicyMemo(1)
	hand := five("2c", "3d", "4h", "7s", "Kc")
	compute := func(v *View) [6]float64 { return [6]float64{float64(v.Node), float64(v.Node + 1)} }
	var wg sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := 0; i < 1000; i++ {
				v := View{Hand: hand, Node: int32(1 + (id*1000+i)%100), Facing: true, CanRaise: true}
				if got, want := pm.lookup(&v, false, compute), compute(&v); got != want {
					t.Errorf("cached %v, expected %v", got, want)
					return
				}
			}
		}(worker)
	}
	wg.Wait()
}

func TestSizedMemoPreservesConfiguration(t *testing.T) {
	m := &Empirical{Version: 4, ResponseAlpha: 3}
	cached, err := m.WithMemoBits(4)
	if err != nil {
		t.Fatal(err)
	}
	if cached.ResponseAlpha != 3 || len(cached.memo.entries) != 16 || m.memo != nil {
		t.Fatal("cache size or configuration changed")
	}
	for _, bits := range []uint{0, 26} {
		if _, err := m.WithMemoBits(bits); err == nil {
			t.Fatalf("accepted size%d", bits)
		}
	}
}
