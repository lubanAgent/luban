package luban

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

func TestConfigCacheUsesFixedTTLAndReturnsIndependentSnapshots(t *testing.T) {
	now := time.Date(2026, time.October, 10, 12, 0, 0, 0, time.UTC)
	cache := newConfigCacheWithClock(func() time.Time { return now })
	loads := 0
	load := func() (agentSnapshot, error) {
		loads++
		return agentSnapshot{
			config: AgentConfig{Flow: []NodeConfig{{ModelName: "model"}}},
			skills: map[string]Skill{"guide": {Name: "guide", Content: "original"}},
		}, nil
	}

	first, err := cache.get("review", load)
	if err != nil {
		t.Fatal(err)
	}
	first.config.Flow[0].ModelName = "caller-change"
	first.skills["guide"] = Skill{Content: "caller-change"}
	now = now.Add(9 * time.Second)
	second, err := cache.get("review", load)
	if err != nil {
		t.Fatal(err)
	}
	if loads != 1 {
		t.Fatalf("loader called %d times before expiry, want 1", loads)
	}
	if second.config.Flow[0].ModelName != "model" || second.skills["guide"].Content != "original" {
		t.Fatalf("caller mutated cached snapshot: %+v %+v", second.config, second.skills)
	}

	now = now.Add(time.Second)
	if _, err := cache.get("review", load); err != nil {
		t.Fatal(err)
	}
	if loads != 2 {
		t.Fatalf("loader called %d times after fixed TTL, want 2", loads)
	}
}

func TestConfigCacheDoesNotStoreLoadErrors(t *testing.T) {
	cache := newConfigCache()
	loadErr := errors.New("configuration load failed")
	loads := 0
	load := func() (agentSnapshot, error) {
		loads++
		return agentSnapshot{}, loadErr
	}
	for i := 0; i < 2; i++ {
		if _, err := cache.get("review", load); !errors.Is(err, loadErr) {
			t.Fatalf("error chain = %v, want %v", err, loadErr)
		}
	}
	if loads != 2 {
		t.Fatalf("failed loads were cached; loader called %d times", loads)
	}
}

func TestConfigCacheConcurrentMissesRemainRaceSafe(t *testing.T) {
	cache := newConfigCache()
	var loads atomic.Int64
	load := func() (agentSnapshot, error) {
		loads.Add(1)
		return agentSnapshot{config: AgentConfig{Flow: []NodeConfig{{ModelName: "model"}}}}, nil
	}
	const workers = 32
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		go func() {
			snapshot, err := cache.get("review", load)
			if err == nil && snapshot.config.Flow[0].ModelName != "model" {
				err = errors.New("unexpected snapshot value")
			}
			errs <- err
		}()
	}
	for i := 0; i < workers; i++ {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	if loads.Load() < 1 {
		t.Fatal("loader was not called")
	}
}
