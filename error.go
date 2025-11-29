package nioclient

// problemer is an error interface for errors that can yield a hint
// how to fix the error. This is useful in HTTP 400, 404, 422, or 409 responses.
type problemer interface {
	error
	Detail() string
	Status() int
}
