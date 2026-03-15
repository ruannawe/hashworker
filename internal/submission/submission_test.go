package submission

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// expectedHash is SHA256("123" + "456") = SHA256("123456").
const expectedHash = "8d969eef6ecad3c29a3a629280e686cf0c3f5d5a86aff3ca12020c923adc6c92"

func TestCalcResult_CorrectHash(t *testing.T) {
	result := CalcResult("123", "456")
	assert.Equal(t, expectedHash, result)
}

func TestCalcResult_OrderMatters(t *testing.T) {
	forward := CalcResult("123", "456")
	backward := CalcResult("456", "123")
	assert.NotEqual(t, forward, backward)
}

func newState(serverNonce string) *ValidationState {
	return &ValidationState{
		UsedNonces:   make(map[string]struct{}),
		JobHistory:   map[int]string{1: serverNonce},
		CurrentJobID: 1,
	}
}

func TestValidateSubmission_Valid(t *testing.T) {
	state := newState("server")
	result := CalcResult("server", "client")
	err := ValidateSubmission(state, 1, "client", result, time.Now())
	assert.NoError(t, err)
}

func TestValidateSubmission_InvalidJobID(t *testing.T) {
	state := newState("server")
	result := CalcResult("server", "client")
	err := ValidateSubmission(state, 999, "client", result, time.Now())
	assert.ErrorIs(t, err, ErrTaskNotExist)
}

func TestValidateSubmission_ExpiredJobID(t *testing.T) {
	state := &ValidationState{
		UsedNonces:   make(map[string]struct{}),
		JobHistory:   map[int]string{1: "old_nonce", 2: "new_nonce"},
		CurrentJobID: 2,
	}
	result := CalcResult("old_nonce", "client")
	err := ValidateSubmission(state, 1, "client", result, time.Now())
	assert.ErrorIs(t, err, ErrTaskExpired)
}

func TestValidateSubmission_WrongResult(t *testing.T) {
	state := newState("server")
	err := ValidateSubmission(state, 1, "client", "wronghash", time.Now())
	assert.ErrorIs(t, err, ErrInvalidResult)
}

func TestValidateSubmission_RateLimit(t *testing.T) {
	state := newState("server")
	now := time.Now()

	result1 := CalcResult("server", "client1")
	err := ValidateSubmission(state, 1, "client1", result1, now)
	assert.NoError(t, err)

	result2 := CalcResult("server", "client2")
	err = ValidateSubmission(state, 1, "client2", result2, now.Add(500*time.Millisecond))
	assert.ErrorIs(t, err, ErrTooFrequent)
}

func TestValidateSubmission_DuplicateNonce(t *testing.T) {
	state := newState("server")
	result := CalcResult("server", "client1")
	now := time.Now()

	err := ValidateSubmission(state, 1, "client1", result, now)
	assert.NoError(t, err)

	// 2s later to bypass rate limit, same nonce.
	err = ValidateSubmission(state, 1, "client1", result, now.Add(2*time.Second))
	assert.ErrorIs(t, err, ErrDuplicateNonce)
}

func TestValidateSubmission_RateLimit_Resets(t *testing.T) {
	state := newState("server")
	state.LastSubmit = time.Now().Add(-2 * time.Second)

	result := CalcResult("server", "client")
	err := ValidateSubmission(state, 1, "client", result, time.Now())
	assert.NoError(t, err)
}
