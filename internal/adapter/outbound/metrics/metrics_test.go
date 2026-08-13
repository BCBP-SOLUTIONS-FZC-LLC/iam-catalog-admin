package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
)

func TestRegister_PreInitializesKnownLabels(t *testing.T) {
	CacheHits.Reset()
	CacheMisses.Reset()
	Register()

	assert.InDelta(t, 0, testutil.ToFloat64(CacheHits.WithLabelValues("cat:departments")), 0)
	assert.InDelta(t, 0, testutil.ToFloat64(CacheMisses.WithLabelValues("cat:plans")), 0)

	CacheHits.WithLabelValues("cat:departments").Inc()
	assert.InDelta(t, 1, testutil.ToFloat64(CacheHits.WithLabelValues("cat:departments")), 0)
}
