package idle

import (
	"sort"
	"sync"
	"testing"
)

func TestOnlineSet_AddRemoveSnapshot(t *testing.T) {
	p := &Plugin{online: make(map[int64]struct{})}

	p.setOnline(1, true)
	p.setOnline(2, true)
	p.setOnline(3, true)
	p.setOnline(2, false)

	got := p.onlineSnapshot()
	sort.Slice(got, func(i, j int) bool { return got[i] < got[j] })
	if len(got) != 2 || got[0] != 1 || got[1] != 3 {
		t.Fatalf("expected [1 3], got %v", got)
	}
}

func TestOnlineSet_ConcurrentAccess(t *testing.T) {
	p := &Plugin{online: make(map[int64]struct{})}

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(id int64) {
			defer wg.Done()
			p.setOnline(id, true)
			_ = p.onlineSnapshot()
			p.setOnline(id, false)
		}(int64(i))
	}
	wg.Wait()

	if len(p.onlineSnapshot()) != 0 {
		t.Fatalf("expected empty set after all removed, got %d", len(p.onlineSnapshot()))
	}
}
