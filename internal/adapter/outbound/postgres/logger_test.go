package postgres

import (
	"testing"

	pgdomain "github.com/BCBP-SOLUTIONS-FZC-LLC/platform-pgcommon/pkg/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// captureLogger records the last structured log call so assertions can be
// made without depending on a concrete logger implementation.
type captureLogger struct {
	msg    string
	fields map[string]interface{}
}

func (l *captureLogger) Debug(msg string, f map[string]interface{}) { l.msg = msg; l.fields = f }
func (l *captureLogger) Info(msg string, f map[string]interface{})  { l.msg = msg; l.fields = f }
func (l *captureLogger) Warn(msg string, f map[string]interface{})  { l.msg = msg; l.fields = f }
func (l *captureLogger) Error(msg string, f map[string]interface{}) { l.msg = msg; l.fields = f }

func TestNewDomainLogger_ImplementsInterface(t *testing.T) {
	cl := &captureLogger{}
	dl := NewDomainLogger(cl)
	require.NotNil(t, dl)
}

func TestDomainLogger_AllLevels(t *testing.T) {
	cl := &captureLogger{}
	dl := NewDomainLogger(cl)

	field := pgdomain.Field{Key: "k", Value: "v"}

	dl.Debug("debug msg", field)
	assert.Equal(t, "debug msg", cl.msg)
	assert.Equal(t, "v", cl.fields["k"])

	dl.Info("info msg", field)
	assert.Equal(t, "info msg", cl.msg)

	dl.Warn("warn msg", field)
	assert.Equal(t, "warn msg", cl.msg)

	dl.Error("error msg", field)
	assert.Equal(t, "error msg", cl.msg)
}

func TestFieldsToMap_Empty(t *testing.T) {
	m := fieldsToMap(nil)
	assert.Nil(t, m)

	m2 := fieldsToMap([]pgdomain.Field{})
	assert.Nil(t, m2)
}

func TestFieldsToMap_NonEmpty(t *testing.T) {
	fields := []pgdomain.Field{
		{Key: "table", Value: "departments"},
		{Key: "op", Value: "insert"},
	}
	m := fieldsToMap(fields)
	require.Len(t, m, 2)
	assert.Equal(t, "departments", m["table"])
	assert.Equal(t, "insert", m["op"])
}
