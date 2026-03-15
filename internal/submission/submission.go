package submission

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"time"
)

var (
	ErrTaskNotExist   = errors.New("Task does not exist")
	ErrTaskExpired    = errors.New("Task expired")
	ErrInvalidResult  = errors.New("Invalid result")
	ErrTooFrequent    = errors.New("Submission too frequent")
	ErrDuplicateNonce = errors.New("Duplicate submission")
)

// ValidationState holds the per-session state needed for submission validation.
type ValidationState struct {
	LastSubmit   time.Time
	UsedNonces   map[string]struct{}
	JobHistory   map[int]string // job_id -> server_nonce
	CurrentJobID int
}

// NewValidationState creates a zero-value ValidationState with initialised maps.
func NewValidationState() *ValidationState {
	return &ValidationState{
		UsedNonces: make(map[string]struct{}),
		JobHistory: make(map[int]string),
	}
}

// CalcResult computes SHA256(serverNonce + clientNonce) as a hex string.
func CalcResult(serverNonce, clientNonce string) string {
	h := sha256.Sum256([]byte(serverNonce + clientNonce))
	return fmt.Sprintf("%x", h)
}

// ValidateSubmission checks a client submission against the session state.
// On success it updates LastSubmit and records the clientNonce.
func ValidateSubmission(state *ValidationState, jobID int, clientNonce, result string, now time.Time) error {
	// 1. Rate limit: at most 1 submission per second.
	if !state.LastSubmit.IsZero() && now.Sub(state.LastSubmit) < time.Second {
		return ErrTooFrequent
	}

	// 2. Job must exist in session history.
	serverNonce, exists := state.JobHistory[jobID]
	if !exists {
		return ErrTaskNotExist
	}

	// 3. Job must be the current (latest) job.
	if jobID != state.CurrentJobID {
		return ErrTaskExpired
	}

	// 4. client_nonce must not have been used before.
	if _, used := state.UsedNonces[clientNonce]; used {
		return ErrDuplicateNonce
	}

	// 5. SHA256 must match.
	expected := CalcResult(serverNonce, clientNonce)
	if result != expected {
		return ErrInvalidResult
	}

	// Update state on success.
	state.LastSubmit = now
	state.UsedNonces[clientNonce] = struct{}{}
	return nil
}
