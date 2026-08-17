package valkey

import (
	"bytes"
	"context"
	"errors"
	"log"
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

// TestCache_Delete_FailureIsLogged verifies LLD §11.1's failure-matrix
// entry ("A CAT-1/CAT-2 write succeeds but the local cache DEL fails" →
// "Logged error, post-commit"): a Delete failure must be observable in
// logs, not silently discarded, even though it's still advisory and never
// fails the caller.
func TestCache_Delete_FailureIsLogged(t *testing.T) {
	mr := miniredis.RunT(t)
	addr := mr.Addr()
	mr.Close()
	c := New("redis://" + addr)

	var buf bytes.Buffer
	origOutput := log.Writer()
	origFlags := log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	defer func() {
		log.SetOutput(origOutput)
		log.SetFlags(origFlags)
	}()

	err := c.Delete(context.Background(), DepartmentsKey)
	require.Error(t, err, "Delete still returns the error — only the caller in the service layer swallows it")
	assert.Contains(t, buf.String(), "cache invalidation failed")
	assert.Contains(t, buf.String(), DepartmentsKey)
}

// fakeLogger captures Error() calls so tests can assert on structured
// logging without depending on platform-gincommon's concrete Zap logger.
type fakeLogger struct {
	msg    string
	fields map[string]interface{}
}

func (f *fakeLogger) Error(msg string, fields map[string]interface{}) {
	f.msg = msg
	f.fields = fields
}

// TestCache_Delete_FailureUsesStructuredLoggerWhenSet verifies the same
// failure logs through SetLogger's structured logger once installed,
// instead of stdlib log.Printf — production-readiness fix.
func TestCache_Delete_FailureUsesStructuredLoggerWhenSet(t *testing.T) {
	mr := miniredis.RunT(t)
	addr := mr.Addr()
	mr.Close()
	c := New("redis://" + addr)

	fl := &fakeLogger{}
	SetLogger(fl)
	t.Cleanup(func() { SetLogger(nil) })

	err := c.Delete(context.Background(), DepartmentsKey)
	require.Error(t, err)
	assert.Equal(t, "cache invalidation failed", fl.msg)
	assert.Equal(t, []string{DepartmentsKey}, fl.fields["keys"])
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
