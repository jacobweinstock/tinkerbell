package kube

import (
	"context"

	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/cache"
)

// NewCache is a cache.NewCacheFunc that hands controller managers the backend's
// cache, so a process holds one set of informers instead of one per manager.
// The backend starts the cache; managers only use it.
func (b *Backend) NewCache(*rest.Config, cache.Options) (cache.Cache, error) {
	return sharedCache{b.cluster.GetCache()}, nil
}

// sharedCache is a cache that its user does not start.
type sharedCache struct {
	cache.Cache
}

// Start blocks until ctx is done without starting the informers, which would
// fail: they are started once, by the backend.
func (sharedCache) Start(ctx context.Context) error {
	<-ctx.Done()
	return nil
}
