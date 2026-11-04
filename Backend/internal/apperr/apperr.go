// Package apperr carries safe, client-facing errors from services to HTTP handlers.
package apperr

import "errors"

type Error struct {
	Status  int
	Code    string
	Message string
}

func (e *Error) Error() string { return e.Code }

func New(status int, code, message string) *Error { return &Error{status, code, message} }

var (
	Unauthorized = New(401, "UNAUTHORIZED", "Your session is invalid or has expired.")
	NotFound     = New(404, "RESOURCE_NOT_FOUND", "Resource not found.")
	Unavailable  = New(503, "SERVICE_UNAVAILABLE", "Service temporarily unavailable.")
	Internal     = New(500, "INTERNAL_ERROR", "Something went wrong.")
)

// From maps any error to a client-safe one; unknown errors never leak details.
func From(err error) *Error {
	var e *Error
	if errors.As(err, &e) {
		return e
	}
	return Internal
}
