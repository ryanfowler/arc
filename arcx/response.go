package arcx

import "net/http"

type responseValue interface {
	writeResponse(*Context) error
}

// Response carries explicit status, headers, and body for a JSON handler.
type Response[T any] struct {
	Status int
	Header http.Header
	Body   T

	noBody bool
}

// OK returns a 200 response.
func OK[T any](body T) Response[T] {
	return Status(http.StatusOK, body)
}

// Created returns a 201 response.
func Created[T any](body T) Response[T] {
	return Status(http.StatusCreated, body)
}

// Accepted returns a 202 response.
func Accepted[T any](body T) Response[T] {
	return Status(http.StatusAccepted, body)
}

// Status returns a response with status and body.
func Status[T any](status int, body T) Response[T] {
	return Response[T]{
		Status: status,
		Body:   body,
	}
}

// NoBody returns a response with status and no body.
func NoBody(status int) Response[struct{}] {
	return Response[struct{}]{
		Status: status,
		noBody: true,
	}
}

func (r Response[T]) writeResponse(c *Context) error {
	for key, values := range r.Header {
		for _, value := range values {
			c.Header().Add(key, value)
		}
	}
	if r.noBody {
		return c.NoContent(r.Status)
	}
	return c.JSON(r.Status, r.Body)
}
