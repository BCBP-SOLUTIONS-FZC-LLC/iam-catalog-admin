package http

import (
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
)

func TestIsOperatorOrSystemErrorSQLState(t *testing.T) {
	assert.True(t, isOperatorOrSystemErrorSQLState(&pgconn.PgError{Code: "57014"}))
	assert.True(t, isOperatorOrSystemErrorSQLState(&pgconn.PgError{Code: "57P01"}))
	assert.True(t, isOperatorOrSystemErrorSQLState(&pgconn.PgError{Code: "58030"}))
	assert.False(t, isOperatorOrSystemErrorSQLState(&pgconn.PgError{Code: "08006"}),
		"class 08 is covered by pgcommon.IsConnectionException, not this helper")
	assert.False(t, isOperatorOrSystemErrorSQLState(&pgconn.PgError{Code: "53300"}),
		"class 53 is covered by pgcommon.IsInsufficientResources, not this helper")
	assert.False(t, isOperatorOrSystemErrorSQLState(&pgconn.PgError{Code: "23505"}))
	assert.False(t, isOperatorOrSystemErrorSQLState(nil))
}
