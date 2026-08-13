package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/BCBP-SOLUTIONS-FZC-LLC/iam-catalog-admin/internal/core/domain"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWrapConnErr_Nil(t *testing.T) {
	assert.NoError(t, wrapConnErr(nil))
}

func TestWrapConnErr_DomainErrorPassesThroughUnchanged(t *testing.T) {
	de := domain.NewError(domain.ErrDepartmentNotFound, "not found")
	assert.Same(t, de, wrapConnErr(de))
}

func TestWrapConnErr_PgErrorPassesThroughUnchanged(t *testing.T) {
	pgErr := &pgconn.PgError{Code: "23505", Message: "duplicate key"}
	assert.Same(t, pgErr, wrapConnErr(pgErr))
}

func TestWrapConnErr_NoRowsPassesThroughUnchanged(t *testing.T) {
	err := wrapConnErr(pgx.ErrNoRows)
	assert.ErrorIs(t, err, pgx.ErrNoRows)
}

func TestWrapConnErr_ContextCanceledPassesThroughUnchanged(t *testing.T) {
	err := wrapConnErr(context.Canceled)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestWrapConnErr_DeadlineExceededPassesThroughUnchanged(t *testing.T) {
	err := wrapConnErr(context.DeadlineExceeded)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestWrapConnErr_GenericErrorBecomesDependencyUnavailable(t *testing.T) {
	err := wrapConnErr(errors.New("connection refused"))
	require.Error(t, err)
	var de *domain.DomainError
	require.ErrorAs(t, err, &de)
	assert.Equal(t, domain.ErrDependencyUnavailable.Error(), de.Code)
}
