package errs

import "errors"

var (
	ErrNoCharacters       = errors.New("no characters are available")
	ErrAIConfigIncomplete = errors.New("user AI config is incomplete")
	ErrTurnPreempted      = errors.New("turn preempted by a newer message")
	ErrStateStopped       = errors.New("user turn loop has stopped")
)
