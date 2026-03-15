package auth

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAuthorize_ValidUsername(t *testing.T) {
	a := &Auth{}
	err := a.Authorize("admin")
	assert.NoError(t, err)
	assert.True(t, a.IsAuthorized())
	assert.Equal(t, "admin", a.Username)
}

func TestAuthorize_EmptyUsername(t *testing.T) {
	a := &Auth{}
	err := a.Authorize("")
	assert.ErrorIs(t, err, ErrEmptyUsername)
	assert.False(t, a.IsAuthorized())
}

func TestAuthorize_AlreadyAuthorized(t *testing.T) {
	a := &Auth{}
	err := a.Authorize("admin")
	assert.NoError(t, err)

	err = a.Authorize("other")
	assert.ErrorIs(t, err, ErrAlreadyAuthorized)
	assert.Equal(t, "admin", a.Username)
}
