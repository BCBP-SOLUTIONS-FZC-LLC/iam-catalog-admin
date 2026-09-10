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

// wrapConnErr remaps SQLSTATE class 08/53/57/58 (availability failures)
// to domain.ErrDBUnavailable. Other PgErrors (e.g. 23505 unique_violation)
// still pass through so the service layer can classify them.
func TestWrapConnErr_AvailabilitySQLStateMapsToDBUnavailable(t *testing.T) {
	for _, code := range []string{"08006", "53300", "57P01", "58030"} {
		t.Run(code, func(t *testing.T) {
			pgErr := &pgconn.PgError{Code: code}
			got := wrapConnErr(pgErr)
			assert.ErrorIs(t, got, domain.ErrDBUnavailable)
		})
	}
}

func TestWrapConnErr_ConstraintPgErrorPassesThroughUnchanged(t *testing.T) {
	pgErr := &pgconn.PgError{Code: "23505"}
	got := wrapConnErr(pgErr)
	assert.Same(t, pgErr, got, "non-availability PgError must pass through, not get remapped")
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
	assert.ErrorIs(t, err, domain.ErrDependencyUnavailable)
}
