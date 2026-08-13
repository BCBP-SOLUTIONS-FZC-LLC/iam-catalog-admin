package valkey

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestCache(t *testing.T) *Cache {
	t.Helper()
	mr := miniredis.RunT(t)
	return New("redis://" + mr.Addr())
}

func TestCache_SetGetDelete(t *testing.T) {
	c := newTestCache(t)
	ctx := context.Background()

	got, err := c.Get(ctx, DepartmentsKey)
	require.NoError(t, err)
	require.Nil(t, got, "miss must return (nil, nil), not an error")

	require.NoError(t, c.Set(ctx, DepartmentsKey, []byte(`[{"code":"LEGAL"}]`), 60*time.Second))

	got, err = c.Get(ctx, DepartmentsKey)
	require.NoError(t, err)
	require.Equal(t, `[{"code":"LEGAL"}]`, string(got))

	require.NoError(t, c.Delete(ctx, DepartmentsKey))
	got, err = c.Get(ctx, DepartmentsKey)
	require.NoError(t, err)
	require.Nil(t, got)
}

func TestCache_Health(t *testing.T) {
	c := newTestCache(t)
	require.NoError(t, c.Health(context.Background()))
}

func TestCache_DeleteNoKeysIsNoop(t *testing.T) {
	c := newTestCache(t)
	require.NoError(t, c.Delete(context.Background()))
}

func TestCache_Close(t *testing.T) {
	c := newTestCache(t)
	require.NoError(t, c.Close())
}

func TestNew_FallsBackToPlainAddrOnParseError(t *testing.T) {
	// "localhost:6379" (no redis:// scheme) fails redis.ParseURL, exercising
	// New's fallback to a bare *redis.Options{Addr: addr}.
	c := New("localhost:6379")
	require.NotNil(t, c)
	require.NotNil(t, c.client)
}

func TestCache_Get_TransportErrorIsNotSwallowed(t *testing.T) {
	mr := miniredis.RunT(t)
	addr := mr.Addr()
	mr.Close()

	c := New("redis://" + addr)
	_, err := c.Get(context.Background(), DepartmentsKey)
	require.Error(t, err)
	assert.False(t, errors.Is(err, redis.Nil), "a connection failure must not be mistaken for a cache miss")
}
