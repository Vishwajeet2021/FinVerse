package eventbus

import "errors"

// permanentError marks a handler failure that retrying cannot fix (a payload
// that fails validation, a reference to something that will never exist).
// The consumer dead-letters it on the first attempt instead of burning
// MaxDeliveries retries — poison messages should reach the DLQ fast so they
// don't delay the healthy messages queued behind them.
type permanentError struct{ err error }

func (e permanentError) Error() string { return e.err.Error() }
func (e permanentError) Unwrap() error { return e.err }

// Permanent wraps err so the consumer skips retries and dead-letters it.
func Permanent(err error) error {
	if err == nil {
		return nil
	}
	return permanentError{err: err}
}

// IsPermanent reports whether err (or anything it wraps) was marked Permanent.
func IsPermanent(err error) bool {
	var p permanentError
	return errors.As(err, &p)
}
