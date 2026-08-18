package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
)

func TestRegister_PreInitializesKnownLabels(t *testing.T) {
	// An isolated registry — Register rebuilds fresh collectors, so this
	// doesn't need (and must not use) prometheus.DefaultRegisterer, which
	// other tests/packages may already have registered catalog_admin_*
	// collectors against.
	Register(prometheus.NewRegistry(), nil)

	assert.InDelta(t, 0, testutil.ToFloat64(CacheHits.WithLabelValues("cat:departments")), 0)
	assert.InDelta(t, 0, testutil.ToFloat64(CacheMisses.WithLabelValues("cat:plans")), 0)

	CacheHits.WithLabelValues("cat:departments").Inc()
	assert.InDelta(t, 1, testutil.ToFloat64(CacheHits.WithLabelValues("cat:departments")), 0)
}
