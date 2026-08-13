package http

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsDBUnavailableSQLState(t *testing.T) {
	assert.True(t, isDBUnavailableSQLState("08006"))
	assert.True(t, isDBUnavailableSQLState("53300"))
	assert.True(t, isDBUnavailableSQLState("57014"))
	assert.True(t, isDBUnavailableSQLState("58030"))
	assert.False(t, isDBUnavailableSQLState("23505"))
	assert.False(t, isDBUnavailableSQLState(""))
	assert.False(t, isDBUnavailableSQLState("4"))
}
