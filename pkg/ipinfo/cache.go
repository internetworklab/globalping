package ipinfo

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/btree"
)

type IPInfoCacheEntry struct {
	Key       string
	Value     *BasicIPInfo
	ExpiresAt time.Time
}

func (entry *IPInfoCacheEntry) Less(other btree.Item) bool {
	otherEntry, ok := other.(*IPInfoCacheEntry)
	if !ok {
		panic("other is not a IPInfoCacheEntry")
	}
	return strings.Compare(entry.Key, otherEntry.Key) < 0
}

type RequestLoggerHook func(ctx context.Context, ip string, cacheHit bool, hasError bool)

type CacheIPInfoProvider struct {
	Upstream      GeneralIPInfoAdapter
	maxExpireTime time.Duration
	store         *btree.BTree
	serviceChan   chan chan CacheStoreAccess
	hook          RequestLoggerHook
}

type CacheStoreAccess struct {
	Fn    func(ctx context.Context) error
	Error chan error
}

func NewCacheIPInfoProvider(upstream GeneralIPInfoAdapter, maxExpireTime time.Duration, hook RequestLoggerHook) *CacheIPInfoProvider {
	return &CacheIPInfoProvider{
		Upstream:      upstream,
		maxExpireTime: maxExpireTime,
		store:         btree.New(2),
		serviceChan:   make(chan chan CacheStoreAccess),
		hook:          hook,
	}
}

func (ch *CacheIPInfoProvider) Run(ctx context.Context) {
	go func() {

		defer close(ch.serviceChan)
		for {

			serviceSubmitter := make(chan CacheStoreAccess)

			select {
			case <-ctx.Done():
				return
			case ch.serviceChan <- serviceSubmitter:
				serviceAccess := <-serviceSubmitter
				err := serviceAccess.Fn(ctx)
				serviceAccess.Error <- err
			}
		}
	}()
}

func (ch *CacheIPInfoProvider) GetName() string {
	return ch.Upstream.GetName()
}

func (ch *CacheIPInfoProvider) getCache(ctx context.Context, ip string) (*BasicIPInfo, error) {
	serviceSubmitter, ok := <-ch.serviceChan
	if !ok {
		return nil, fmt.Errorf("cache is closed")
	}

	storeChan := make(chan *btree.BTree, 1)

	serviceAccess := CacheStoreAccess{
		Fn: func(ctx context.Context) error {
			storeChan <- ch.store
			return nil
		},
		Error: make(chan error, 1),
	}
	serviceSubmitter <- serviceAccess
	store := <-storeChan
	if err := <-serviceAccess.Error; err != nil {
		return nil, fmt.Errorf("failed to get ipinfo for %s from cache: failed to obtain store reference: %v", ip, err)
	}

	if item := store.Get(&IPInfoCacheEntry{Key: ip}); item != nil {
		if cacheEntry, ok := item.(*IPInfoCacheEntry); ok {
			if cacheEntry.ExpiresAt.After(time.Now()) {
				return cacheEntry.Value, nil
			}
			return nil, nil
		}
		panic("item is not a *IPInfoCacheEntry")
	}

	return nil, nil
}

func (ch *CacheIPInfoProvider) updateCache(ctx context.Context, ip string, result *BasicIPInfo) error {

	storeChan := make(chan *btree.BTree, 1)
	serviceAccessSubmitter, ok := <-ch.serviceChan
	if !ok {
		return fmt.Errorf("cache is closed")
	}

	serviceAccess := CacheStoreAccess{
		Fn: func(ctx context.Context) error {
			storeChan <- ch.store.Clone()
			return nil
		},
		Error: make(chan error, 1),
	}

	serviceAccessSubmitter <- serviceAccess
	if err := <-serviceAccess.Error; err != nil {
		return fmt.Errorf("failed to update cache: failed to obtain store clone: %v", err)
	}

	clonedStore := <-storeChan
	clonedStore.ReplaceOrInsert(&IPInfoCacheEntry{
		Key:       ip,
		Value:     result,
		ExpiresAt: time.Now().Add(ch.maxExpireTime),
	})

	serviceAccess = CacheStoreAccess{
		Fn: func(ctx context.Context) error {
			ch.store = clonedStore
			return nil
		},
		Error: make(chan error, 1),
	}
	serviceAccessSubmitter <- serviceAccess
	if err := <-serviceAccess.Error; err != nil {
		return fmt.Errorf("failed to update cache: failed to update store: %v", err)
	}

	return nil
}

func (ch *CacheIPInfoProvider) GetIPInfo(ctx context.Context, ip string) (*BasicIPInfo, error) {

	cacheHit := new(bool)
	*cacheHit = false

	cacheErrored := new(bool)
	*cacheErrored = false

	defer func(cacheHit *bool, hasError *bool) {
		ch.hook(ctx, ip, *cacheHit, *hasError)
	}(cacheHit, cacheErrored)

	cached, err := ch.getCache(ctx, ip)
	if err != nil {
		*cacheErrored = true
		return nil, fmt.Errorf("failed to get ipinfo for %s from cache: %v", ip, err)
	}

	if cached != nil {
		*cacheHit = true
		return cached, nil
	}

	result, err := ch.Upstream.GetIPInfo(ctx, ip)
	if err != nil {
		*cacheErrored = true
		return nil, err
	}
	if result == nil {
		return nil, nil
	}
	if err := ch.updateCache(ctx, ip, result); err != nil {
		return nil, err
	}
	return result, nil
}
