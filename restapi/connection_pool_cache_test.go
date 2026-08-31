package restapi

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	pool "github.com/silenceper/pool"
)

func TestConnectionPoolCacheCreatesOnePoolForConcurrentLogins(t *testing.T) {
	cache := newConnectionPoolCache()
	var creations atomic.Int32
	var sharedPool pool.Pool
	want := &sharedPool

	create := func() (*pool.Pool, error) {
		creations.Add(1)
		time.Sleep(10 * time.Millisecond)
		return want, nil
	}

	const callers = 20
	results := make(chan *pool.Pool, callers)
	errs := make(chan error, callers)
	var wg sync.WaitGroup
	for range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := cache.GetOrCreate(credentialsCacheKey("service", "secret"), create)
			results <- got
			errs <- err
		}()
	}
	wg.Wait()
	close(results)
	close(errs)

	for err := range errs {
		if err != nil {
			t.Fatalf("GetOrCreate() error = %v", err)
		}
	}
	for got := range results {
		if got != want {
			t.Fatalf("GetOrCreate() pool = %p, want %p", got, want)
		}
	}
	if got := creations.Load(); got != 1 {
		t.Fatalf("pool creation count = %d, want 1", got)
	}
}

func TestConnectionPoolCacheRetriesFailedCreation(t *testing.T) {
	cache := newConnectionPoolCache()
	wantErr := errors.New("authentication failed")

	if _, err := cache.GetOrCreate("credentials", func() (*pool.Pool, error) {
		return nil, wantErr
	}); !errors.Is(err, wantErr) {
		t.Fatalf("first GetOrCreate() error = %v, want %v", err, wantErr)
	}

	var replacement pool.Pool
	want := &replacement
	got, err := cache.GetOrCreate("credentials", func() (*pool.Pool, error) {
		return want, nil
	})
	if err != nil {
		t.Fatalf("second GetOrCreate() error = %v", err)
	}
	if got != want {
		t.Fatalf("second GetOrCreate() pool = %p, want %p", got, want)
	}
}

func TestCredentialsCacheKeySeparatesUsersAndSecrets(t *testing.T) {
	if credentialsCacheKey("service-a", "secret") == credentialsCacheKey("service-b", "secret") {
		t.Fatal("different users produced the same cache key")
	}
	if credentialsCacheKey("service", "secret-a") == credentialsCacheKey("service", "secret-b") {
		t.Fatal("different secrets produced the same cache key")
	}
}
