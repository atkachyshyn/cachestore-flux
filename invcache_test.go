package invcache_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	invstore "github.com/goware/cachestore-flux"
	memcache "github.com/goware/cachestore-mem"
	cachestore "github.com/goware/cachestore2"
	"github.com/goware/pubsub"
	"github.com/stretchr/testify/require"
)

const (
	N = 5
)

func TestInvalidatingCache_Set_Success(t *testing.T) {
	ctx := context.Background()
	store, err := memcache.NewCacheWithSize[string](N, cachestore.WithDefaultKeyExpiry(time.Minute))
	require.NoError(t, err)

	publishCalled := false
	mockPS := &mockPubSub[invstore.StoreInvalidationMessage]{}
	ic := invstore.NewInvalidatingStore(store, mockPS)

	mockPS.publishFunc = func(ctx context.Context, channelID string, msg invstore.StoreInvalidationMessage) error {
		publishCalled = true
		require.Equal(t, invstore.DefaultChannelID, channelID)
		require.Equal(t, "key", msg.Entries[0].Key)
		require.Equal(t, getHash(t, "val"), msg.Entries[0].ContentHash)
		require.Equal(t, ic.GetInstanceID(), msg.Origin)
		return nil
	}

	err = ic.Set(ctx, "key", "val")
	require.NoError(t, err)

	val, ok, err := store.Get(ctx, "key")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "val", val)
	require.True(t, publishCalled)
}

func TestInvalidatingCache_Set_PublishError(t *testing.T) {
	ctx := context.Background()

	store, err := memcache.NewCacheWithSize[string](N, cachestore.WithDefaultKeyExpiry(time.Minute))
	require.NoError(t, err)

	publishErr := errors.New("publish failed: Set")
	mockPS := &mockPubSub[invstore.StoreInvalidationMessage]{
		publishFunc: func(ctx context.Context, channelID string, msg invstore.StoreInvalidationMessage) error {
			return publishErr
		},
	}
	ic := invstore.NewInvalidatingStore(store, mockPS)

	err = ic.Set(ctx, "key", "val")
	require.ErrorIs(t, err, publishErr)

	val, ok, err := store.Get(ctx, "key")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "val", val)
}

func TestInvalidatingCache_SetEx_Success(t *testing.T) {
	ctx := context.Background()
	store, err := memcache.NewCacheWithSize[string](N, cachestore.WithDefaultKeyExpiry(time.Minute))
	require.NoError(t, err)

	published := false
	mockPS := &mockPubSub[invstore.StoreInvalidationMessage]{}
	ic := invstore.NewInvalidatingStore(store, mockPS)

	mockPS.publishFunc = func(ctx context.Context, channelID string, msg invstore.StoreInvalidationMessage) error {
		published = true
		require.Equal(t, "key", msg.Entries[0].Key)
		require.Equal(t, getHash(t, "val"), msg.Entries[0].ContentHash)
		require.Equal(t, ic.GetInstanceID(), msg.Origin)
		return nil
	}

	err = ic.SetEx(ctx, "key", "val", 5*time.Second)
	require.NoError(t, err)

	val, ok, err := store.Get(ctx, "key")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "val", val)
	require.True(t, published)
}

func TestInvalidatingCache_SetEx_PublishError(t *testing.T) {
	ctx := context.Background()
	store, _ := memcache.NewCacheWithSize[string](N, cachestore.WithDefaultKeyExpiry(time.Minute))

	publishErr := errors.New("publish failed: SetEx")
	mockPS := &mockPubSub[invstore.StoreInvalidationMessage]{
		publishFunc: func(ctx context.Context, channelID string, msg invstore.StoreInvalidationMessage) error {
			return publishErr
		},
	}
	ic := invstore.NewInvalidatingStore(store, mockPS)

	err := ic.SetEx(ctx, "key", "val", 5*time.Second)
	require.ErrorIs(t, err, publishErr)

	v, ok, getErr := store.Get(ctx, "key")
	require.NoError(t, getErr)
	require.True(t, ok)
	require.Equal(t, "val", v)
}

func TestInvalidatingCache_BatchSet_Success(t *testing.T) {
	ctx := context.Background()
	store, err := memcache.NewCacheWithSize[string](N, cachestore.WithDefaultKeyExpiry(time.Minute))
	require.NoError(t, err)

	var publishedEntries []invstore.CacheInvalidationEntry
	mockPS := &mockPubSub[invstore.StoreInvalidationMessage]{
		publishFunc: func(ctx context.Context, channelID string, msg invstore.StoreInvalidationMessage) error {
			publishedEntries = append(publishedEntries, msg.Entries...)
			return nil
		},
	}
	ic := invstore.NewInvalidatingStore(store, mockPS)

	keys := []string{"key1", "key2"}
	vals := []string{"val1", "val2"}
	err = ic.BatchSet(ctx, keys, vals)
	require.NoError(t, err)

	for i, k := range keys {
		got, ok, err := store.Get(ctx, k)
		require.NoError(t, err)
		require.True(t, ok)
		require.Equal(t, vals[i], got)
	}

	require.ElementsMatch(t, []invstore.CacheInvalidationEntry{{Key: "key1", ContentHash: getHash(t, "val1")}, {Key: "key2", ContentHash: getHash(t, "val2")}}, publishedEntries)
}

func TestInvalidatingCache_BatchSet_PublishError(t *testing.T) {
	ctx := context.Background()
	store, err := memcache.NewCacheWithSize[string](N, cachestore.WithDefaultKeyExpiry(time.Minute))
	require.NoError(t, err)

	publishErr := errors.New("BatchSet: publish invalidation failed for keys: [key2]")
	mockPS := &mockPubSub[invstore.StoreInvalidationMessage]{
		publishFunc: func(ctx context.Context, channelID string, msg invstore.StoreInvalidationMessage) error {
			if msg.Entries[1].Key == "key2" {
				return publishErr
			}
			return nil
		},
	}
	ic := invstore.NewInvalidatingStore(store, mockPS)

	keys := []string{"key1", "key2"}
	vals := []string{"val1", "val2"}
	err = ic.BatchSet(ctx, keys, vals)
	require.Error(t, err)
	require.ErrorContains(t, err, publishErr.Error())

	for i, k := range keys {
		got, ok, errGet := store.Get(ctx, k)
		require.NoError(t, errGet)
		require.True(t, ok)
		require.Equal(t, vals[i], got)
	}
}

func TestInvalidatingCache_BatchSetEx_Success(t *testing.T) {
	ctx := context.Background()
	store, err := memcache.NewCacheWithSize[string](N, cachestore.WithDefaultKeyExpiry(time.Minute))
	require.NoError(t, err)

	var published []invstore.CacheInvalidationEntry
	mockPS := &mockPubSub[invstore.StoreInvalidationMessage]{
		publishFunc: func(ctx context.Context, channelID string, msg invstore.StoreInvalidationMessage) error {
			published = append(published, msg.Entries...)
			return nil
		},
	}
	ic := invstore.NewInvalidatingStore(store, mockPS)

	keys := []string{"key1", "key2"}
	vals := []string{"val1", "val2"}
	err = ic.BatchSetEx(ctx, keys, vals, 2*time.Second)
	require.NoError(t, err)

	for i, k := range keys {
		got, ok, err := store.Get(ctx, k)
		require.NoError(t, err)
		require.True(t, ok)
		require.Equal(t, vals[i], got)
	}
	require.ElementsMatch(t, []invstore.CacheInvalidationEntry{{Key: "key1", ContentHash: getHash(t, "val1")}, {Key: "key2", ContentHash: getHash(t, "val2")}}, published)
}

func TestInvalidatingCache_BatchSetEx_PublishError(t *testing.T) {
	ctx := context.Background()
	store, err := memcache.NewCacheWithSize[string](N, cachestore.WithDefaultKeyExpiry(time.Minute))
	require.NoError(t, err)

	publishErr := errors.New("BatchSetEx: publish invalidation failed for keys: [key1 key2]")
	mockPS := &mockPubSub[invstore.StoreInvalidationMessage]{
		publishFunc: func(ctx context.Context, channelID string, msg invstore.StoreInvalidationMessage) error {
			return publishErr
		},
	}
	ic := invstore.NewInvalidatingStore(store, mockPS)

	keys := []string{"key1", "key2"}
	vals := []string{"val1", "val2"}
	err = ic.BatchSetEx(ctx, keys, vals, 2*time.Second)
	require.Error(t, err)
	require.ErrorContains(t, err, publishErr.Error())

	for i, k := range keys {
		v, ok, err := store.Get(ctx, k)
		require.NoError(t, err)
		require.True(t, ok)
		require.Equal(t, vals[i], v)
	}
}

func TestInvalidatingCache_Delete_Success(t *testing.T) {
	ctx := context.Background()
	store, err := memcache.NewCacheWithSize[string](N, cachestore.WithDefaultKeyExpiry(time.Minute))
	require.NoError(t, err)

	err = store.Set(ctx, "key", "val")
	require.NoError(t, err)

	published := false
	mockPS := &mockPubSub[invstore.StoreInvalidationMessage]{}
	ic := invstore.NewInvalidatingStore(store, mockPS)

	mockPS.publishFunc = func(ctx context.Context, channelID string, msg invstore.StoreInvalidationMessage) error {
		published = true
		require.Equal(t, "key", msg.Entries[0].Key)
		require.Equal(t, "", msg.Entries[0].ContentHash)
		require.Equal(t, ic.GetInstanceID(), msg.Origin)
		return nil
	}

	err = ic.Delete(ctx, "key")
	require.NoError(t, err)
	require.True(t, published)

	val, ok, err := store.Get(ctx, "val")
	require.NoError(t, err)
	require.False(t, ok)
	require.Equal(t, "", val)
}

func TestInvalidatingCache_Delete_PublishError(t *testing.T) {
	ctx := context.Background()
	store, err := memcache.NewCacheWithSize[string](N, cachestore.WithDefaultKeyExpiry(time.Minute))
	require.NoError(t, err)

	err = store.Set(ctx, "key", "val")
	require.NoError(t, err)

	pubErr := errors.New("publish fail: Delete")
	mockPS := &mockPubSub[invstore.StoreInvalidationMessage]{
		publishFunc: func(ctx context.Context, channelID string, msg invstore.StoreInvalidationMessage) error {
			return pubErr
		},
	}
	ic := invstore.NewInvalidatingStore(store, mockPS)

	err = ic.Delete(ctx, "key")
	require.ErrorIs(t, err, pubErr)

	val, ok, err := store.Get(ctx, "key")
	require.NoError(t, err)
	require.False(t, ok)
	require.Equal(t, "", val)
}

func TestInvalidatingCache_DeletePrefix_Success(t *testing.T) {
	ctx := context.Background()
	store, err := memcache.NewCacheWithSize[string](N, cachestore.WithDefaultKeyExpiry(time.Minute))
	require.NoError(t, err)

	err = store.Set(ctx, "abc1", "val1")
	require.NoError(t, err)
	err = store.Set(ctx, "abc2", "val2")
	require.NoError(t, err)
	err = store.Set(ctx, "xyz3", "val3")
	require.NoError(t, err)

	published := false
	mockPS := &mockPubSub[invstore.StoreInvalidationMessage]{}
	ic := invstore.NewInvalidatingStore(store, mockPS)

	mockPS.publishFunc = func(ctx context.Context, channelID string, msg invstore.StoreInvalidationMessage) error {
		published = true
		require.Equal(t, "abc*", msg.Entries[0].Key)
		require.Equal(t, "", msg.Entries[0].ContentHash)
		require.Equal(t, ic.GetInstanceID(), msg.Origin)
		return nil
	}

	err = ic.DeletePrefix(ctx, "abc")
	require.NoError(t, err)
	require.True(t, published)

	_, ok1, err := store.Get(ctx, "abc1")
	require.NoError(t, err)
	_, ok2, err := store.Get(ctx, "abc2")
	require.NoError(t, err)
	_, ok3, err := store.Get(ctx, "xyz3")
	require.NoError(t, err)
	require.False(t, ok1)
	require.False(t, ok2)
	require.True(t, ok3)
}

func TestInvalidatingCache_DeletePrefix_PublishError(t *testing.T) {
	ctx := context.Background()
	store, err := memcache.NewCacheWithSize[string](N, cachestore.WithDefaultKeyExpiry(time.Minute))
	require.NoError(t, err)

	err = store.Set(ctx, "abc1", "val1")
	require.NoError(t, err)

	pubErr := errors.New("publish fail: DeletePrefix")
	mockPS := &mockPubSub[invstore.StoreInvalidationMessage]{
		publishFunc: func(ctx context.Context, ch string, msg invstore.StoreInvalidationMessage) error {
			return pubErr
		},
	}
	ic := invstore.NewInvalidatingStore(store, mockPS)

	err = ic.DeletePrefix(ctx, "abc")
	require.ErrorIs(t, err, pubErr)

	_, ok1, err := store.Get(ctx, "abc1")
	require.NoError(t, err)
	require.False(t, ok1)
}

func TestInvalidatingCache_ClearAll_Success(t *testing.T) {
	ctx := context.Background()
	store, err := memcache.NewCacheWithSize[string](N, cachestore.WithDefaultKeyExpiry(time.Minute))
	require.NoError(t, err)

	err = store.Set(ctx, "key1", "val1")
	require.NoError(t, err)
	err = store.Set(ctx, "key2", "val2")
	require.NoError(t, err)

	published := false
	mockPS := &mockPubSub[invstore.StoreInvalidationMessage]{}
	ic := invstore.NewInvalidatingStore(store, mockPS)

	mockPS.publishFunc = func(ctx context.Context, channelID string, msg invstore.StoreInvalidationMessage) error {
		published = true
		require.Equal(t, "*", msg.Entries[0].Key)
		require.Equal(t, "", msg.Entries[0].ContentHash)
		require.Equal(t, ic.GetInstanceID(), msg.Origin)
		return nil
	}

	err = ic.ClearAll(ctx)
	require.NoError(t, err)
	require.True(t, published)

	exists1, err := store.Exists(ctx, "key1")
	require.NoError(t, err)
	exists2, err := store.Exists(ctx, "key2")
	require.NoError(t, err)
	require.False(t, exists1)
	require.False(t, exists2)
}

func TestInvalidatingCache_ClearAll_PublishError(t *testing.T) {
	ctx := context.Background()
	store, err := memcache.NewCacheWithSize[string](N, cachestore.WithDefaultKeyExpiry(time.Minute))
	require.NoError(t, err)

	err = store.Set(ctx, "key", "val")
	require.NoError(t, err)

	pubErr := errors.New("publish fail: ClearAll")
	mockPS := &mockPubSub[invstore.StoreInvalidationMessage]{
		publishFunc: func(ctx context.Context, ch string, msg invstore.StoreInvalidationMessage) error {
			return pubErr
		},
	}
	ic := invstore.NewInvalidatingStore(store, mockPS)

	err = ic.ClearAll(ctx)
	require.ErrorIs(t, err, pubErr)

	exists, err := store.Exists(ctx, "key")
	require.NoError(t, err)
	require.False(t, exists)
}

func TestInvalidatingCache_GetOrSetWithLock_Success(t *testing.T) {
	ctx := context.Background()
	store, err := memcache.NewCacheWithSize[string](N, cachestore.WithDefaultKeyExpiry(time.Minute))
	require.NoError(t, err)

	published := false
	mockPS := &mockPubSub[invstore.StoreInvalidationMessage]{}
	ic := invstore.NewInvalidatingStore(store, mockPS)

	mockPS.publishFunc = func(ctx context.Context, channelID string, msg invstore.StoreInvalidationMessage) error {
		published = true
		require.Equal(t, "lockedKey", msg.Entries[0].Key)
		require.Equal(t, getHash(t, "valFromGetter"), msg.Entries[0].ContentHash)
		require.Equal(t, ic.GetInstanceID(), msg.Origin)
		return nil
	}

	val, err := ic.GetOrSetWithLock(ctx, "lockedKey", func(ctx context.Context, key string) (string, error) {
		return "valFromGetter", nil
	})
	require.NoError(t, err)
	require.Equal(t, "valFromGetter", val)
	require.True(t, published)

	got, ok, err := store.Get(ctx, "lockedKey")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "valFromGetter", got)
}

func TestInvalidatingCache_GetOrSetWithLock_Error(t *testing.T) {
	ctx := context.Background()
	store, err := memcache.NewCacheWithSize[string](N, cachestore.WithDefaultKeyExpiry(time.Minute))
	require.NoError(t, err)

	mockPS := &mockPubSub[invstore.StoreInvalidationMessage]{
		publishFunc: func(ctx context.Context, channelID string, msg invstore.StoreInvalidationMessage) error {
			t.Error("Publish should NOT be called if getter fails")
			return nil
		},
	}
	ic := invstore.NewInvalidatingStore(store, mockPS)

	getterErr := errors.New("getter failed")
	_, err = ic.GetOrSetWithLock(ctx, "key", func(ctx context.Context, key string) (string, error) {
		return "", getterErr
	})
	require.ErrorIs(t, err, getterErr)
}

func TestInvalidatingCache_GetOrSetWithLock_PublishError(t *testing.T) {
	ctx := context.Background()
	store, err := memcache.NewCacheWithSize[string](N, cachestore.WithDefaultKeyExpiry(time.Minute))
	require.NoError(t, err)

	pubErr := errors.New("publish fail: GetOrSetWithLock")
	mockPS := &mockPubSub[invstore.StoreInvalidationMessage]{
		publishFunc: func(ctx context.Context, ch string, msg invstore.StoreInvalidationMessage) error {
			return pubErr
		},
	}
	ic := invstore.NewInvalidatingStore(store, mockPS)

	val, err := ic.GetOrSetWithLock(ctx, "lockKey", func(ctx context.Context, key string) (string, error) {
		return "valFromGetter", nil
	})
	require.ErrorIs(t, err, pubErr)
	require.Equal(t, "valFromGetter", val)

	storedVal, ok, err := store.Get(ctx, "lockKey")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "valFromGetter", storedVal)
}

func TestInvalidatingCache_GetOrSetWithLockEx_Success(t *testing.T) {
	ctx := context.Background()
	store, _ := memcache.NewCacheWithSize[string](N, cachestore.WithDefaultKeyExpiry(time.Minute))

	published := false
	mockPS := &mockPubSub[invstore.StoreInvalidationMessage]{}
	ic := invstore.NewInvalidatingStore(store, mockPS)

	mockPS.publishFunc = func(ctx context.Context, channelID string, msg invstore.StoreInvalidationMessage) error {
		published = true
		require.Equal(t, "lockExKey", msg.Entries[0].Key)
		require.Equal(t, getHash(t, "valExFromGetter"), msg.Entries[0].ContentHash)
		require.Equal(t, ic.GetInstanceID(), msg.Origin)
		return nil
	}

	val, err := ic.GetOrSetWithLockEx(ctx, "lockExKey", func(ctx context.Context, key string) (string, error) {
		return "valExFromGetter", nil
	}, 3*time.Second)
	require.NoError(t, err)
	require.Equal(t, "valExFromGetter", val)
	require.True(t, published)

	got, ok, err := store.Get(ctx, "lockExKey")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "valExFromGetter", got)
}

func TestInvalidatingCache_GetOrSetWithLockEx_Error(t *testing.T) {
	ctx := context.Background()
	store, err := memcache.NewCacheWithSize[string](5, cachestore.WithDefaultKeyExpiry(time.Minute))
	require.NoError(t, err)

	mockPS := &mockPubSub[invstore.StoreInvalidationMessage]{
		publishFunc: func(ctx context.Context, channelID string, msg invstore.StoreInvalidationMessage) error {
			t.Error("Publish should NOT be called if getter fails")
			return nil
		},
	}
	ic := invstore.NewInvalidatingStore(store, mockPS)

	getterErr := errors.New("getter failed")
	_, err = ic.GetOrSetWithLockEx(ctx, "lockExKeyErr", func(ctx context.Context, key string) (string, error) {
		return "", getterErr
	}, 2*time.Second)
	require.ErrorIs(t, err, getterErr)
}

func TestInvalidatingCache_GetOrSetWithLockEx_PublishError(t *testing.T) {
	ctx := context.Background()
	store, err := memcache.NewCacheWithSize[string](N, cachestore.WithDefaultKeyExpiry(time.Minute))
	require.NoError(t, err)

	pubErr := errors.New("publish fail: GetOrSetWithLockEx")
	mockPS := &mockPubSub[invstore.StoreInvalidationMessage]{
		publishFunc: func(ctx context.Context, ch string, msg invstore.StoreInvalidationMessage) error {
			return pubErr
		},
	}
	ic := invstore.NewInvalidatingStore(store, mockPS)

	val, err := ic.GetOrSetWithLockEx(ctx, "lockExKey", func(ctx context.Context, key string) (string, error) {
		return "valExFromGetter", nil
	}, 3*time.Second)
	require.ErrorIs(t, err, pubErr)
	require.Equal(t, "valExFromGetter", val)

	got, ok, err := store.Get(ctx, "lockExKey")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "valExFromGetter", got)
}

func TestComputeHash(t *testing.T) {
	// Simple string.
	hash1, err := invstore.ComputeHash("hello")
	require.NoError(t, err)
	hash2, err := invstore.ComputeHash("hello")
	require.NoError(t, err)
	require.Equal(t, hash1, hash2)

	// Empty string.
	emptyHash1, err := invstore.ComputeHash("")
	require.NoError(t, err)
	emptyHash2, err := invstore.ComputeHash("")
	require.NoError(t, err)
	require.Equal(t, emptyHash1, emptyHash2)

	// Struct.
	type Person struct {
		Name string `json:"name"`
		Age  int    `json:"age"`
	}
	p := Person{"Alice", 30}
	personHash1, err := invstore.ComputeHash(p)
	require.NoError(t, err)
	personHash2, err := invstore.ComputeHash(p)
	require.NoError(t, err)
	require.Equal(t, personHash1, personHash2)

	// Mismatch on struct change.
	p2 := Person{"Alice", 31}
	personHash3, err := invstore.ComputeHash(p2)
	require.NoError(t, err)
	require.NotEqual(t, personHash1, personHash3)
}

func getHash(t *testing.T, val string) string {
	h, err := invstore.ComputeHash(val)
	require.NoError(t, err)
	return h
}

// mockPubSub is a mock implementation of pubsub.PubSub.
type mockPubSub[M any] struct {
	publishFunc   func(ctx context.Context, channelID string, message M) error
	subscribeFunc func(ctx context.Context, channelID string, optSubcriptionID ...string) (pubsub.Subscription[M], error)
}

func (m mockPubSub[M]) IsRunning() bool {
	panic("unimplemented")
}

func (m mockPubSub[M]) NumSubscribers(channelID string) (int, error) {
	panic("unimplemented")
}

func (m mockPubSub[M]) Publish(ctx context.Context, channelID string, message M) error {
	if m.publishFunc != nil {
		return m.publishFunc(ctx, channelID, message)
	}
	return nil
}

func (m mockPubSub[M]) Run(ctx context.Context) error {
	panic("unimplemented")
}

func (m mockPubSub[M]) Stop() {
	panic("unimplemented")
}

func (m mockPubSub[M]) Subscribe(ctx context.Context, channelID string, optSubcriptionID ...string) (pubsub.Subscription[M], error) {
	if m.subscribeFunc != nil {
		return m.subscribeFunc(ctx, channelID, optSubcriptionID...)
	}
	return nil, fmt.Errorf("SubscribeFunc not set")
}

// mockSubscription is a mock implementation of pubsub.Subscription.
type mockSubscription[M any] struct {
	msgCh  chan M
	doneCh chan struct{}
}

func (m *mockSubscription[M]) ChannelID() string {
	panic("unimplemented")
}

func (m *mockSubscription[M]) SendMessage(ctx context.Context, message M) error {
	panic("unimplemented")
}

func (m *mockSubscription[M]) ReadMessage() <-chan M {
	return m.msgCh
}

func (m *mockSubscription[M]) Done() <-chan struct{} {
	panic("unimplemented")
}

func (m *mockSubscription[M]) Err() error {
	panic("unimplemented")
}

func (m *mockSubscription[M]) Unsubscribe() {
	close(m.msgCh)
}
