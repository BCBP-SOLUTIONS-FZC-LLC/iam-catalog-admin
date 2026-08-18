package postgres

import (
	"github.com/BCBP-SOLUTIONS-FZC-LLC/platform-pgcommon/pkg/domain"
)

// Logger is the minimal structured-logging capability this package needs —
// satisfied structurally by platform-gincommon/pkg/logger's port.Logger (an
// internal type, so this package declares its own duck-typed interface
// rather than importing it directly). Mirrors internal/adapter/outbound/
// valkey's Logger interface.
type Logger interface {
	Debug(msg string, fields map[string]interface{})
	Info(msg string, fields map[string]interface{})
	Warn(msg string, fields map[string]interface{})
	Error(msg string, fields map[string]interface{})
}

// domainLogger adapts Logger to platform-pgcommon/pkg/domain.Logger (added in
// pgcommon v1.2.0 specifically so consuming services could implement it
// without depending on pgcommon's internal port.Logger). Wrap this service's
// own logger with NewDomainLogger and pass it as pgcommon.Config.Logger /
// migrate.Runner.Logger so slow-query and migration-step logs go through the
// same structured logger as everything else, instead of being silently
// dropped (both fields are optional and no-op when nil).
type domainLogger struct{ l Logger }

// NewDomainLogger wraps l for use as pgcommon.Config.Logger or
// migrate.Runner.Logger.
func NewDomainLogger(l Logger) domain.Logger {
	return domainLogger{l: l}
}

func fieldsToMap(fields []domain.Field) map[string]interface{} {
	if len(fields) == 0 {
		return nil
	}
	m := make(map[string]interface{}, len(fields))
	for _, f := range fields {
		m[f.Key] = f.Value
	}
	return m
}

func (d domainLogger) Debug(msg string, fields ...domain.Field) { d.l.Debug(msg, fieldsToMap(fields)) }
func (d domainLogger) Info(msg string, fields ...domain.Field)  { d.l.Info(msg, fieldsToMap(fields)) }
func (d domainLogger) Warn(msg string, fields ...domain.Field)  { d.l.Warn(msg, fieldsToMap(fields)) }
func (d domainLogger) Error(msg string, fields ...domain.Field) { d.l.Error(msg, fieldsToMap(fields)) }
