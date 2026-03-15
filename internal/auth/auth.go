package auth

import "errors"

var (
	ErrEmptyUsername     = errors.New("empty username")
	ErrAlreadyAuthorized = errors.New("already authorized")
)

// Auth tracks per-session authentication state.
type Auth struct {
	Username string
}

// Authorize authenticates the session with the given username.
func (a *Auth) Authorize(username string) error {
	if a.Username != "" {
		return ErrAlreadyAuthorized
	}
	if username == "" {
		return ErrEmptyUsername
	}
	a.Username = username
	return nil
}

// IsAuthorized reports whether the session has been authenticated.
func (a *Auth) IsAuthorized() bool {
	return a.Username != ""
}
