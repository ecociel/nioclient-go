package nioclient

import (
	"fmt"
	"net/http"
)

// problemer is an error interface for errors that can yield a hint
// how to fix the error. This is useful in HTTP 400, 404, 422, or 409 responses.
type problemer interface {
	error
	Detail() string
	Status() int
}

type userError struct {
	cause  error
	status int
}

func (e userError) Error() string {
	return fmt.Sprintf("%d: %v", e.status, e.cause)
}

func (e userError) Detail() string {
	return e.Error()
}

func (e userError) Status() int {
	return e.status
}

func notFound(err error) userError {
	return userError{cause: err, status: http.StatusNotFound}
}
