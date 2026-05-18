package arcx

import (
	"errors"
	"net/http"
)

// ErrorHandler writes a response for a handler error.
type ErrorHandler func(*Context, error)

// HTTPError describes an HTTP error response.
type HTTPError struct {
	Status  int
	Code    string
	Message string
	Err     error
}

// Error returns the error message.
func (e *HTTPError) Error() string {
	if e == nil {
		return ""
	}
	if e.Message != "" {
		return e.Message
	}
	if e.Err != nil {
		return e.Err.Error()
	}
	return http.StatusText(e.Status)
}

// Unwrap returns the underlying error.
func (e *HTTPError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// BadRequest returns a 400 error.
func BadRequest(err error) error {
	return httpError(http.StatusBadRequest, "bad_request", err, "")
}

// Unauthorized returns a 401 error.
func Unauthorized(message string) error {
	return httpError(http.StatusUnauthorized, "unauthorized", nil, message)
}

// Forbidden returns a 403 error.
func Forbidden(message string) error {
	return httpError(http.StatusForbidden, "forbidden", nil, message)
}

// NotFound returns a 404 error.
func NotFound(message string) error {
	return httpError(http.StatusNotFound, "not_found", nil, message)
}

// Conflict returns a 409 error.
func Conflict(message string) error {
	return httpError(http.StatusConflict, "conflict", nil, message)
}

// Unprocessable returns a 422 error.
func Unprocessable(err error) error {
	return httpError(http.StatusUnprocessableEntity, "unprocessable_entity", err, "")
}

// Internal returns a 500 error.
func Internal(err error) error {
	return httpError(http.StatusInternalServerError, "internal_error", err, "")
}

func httpError(status int, code string, err error, message string) error {
	if message == "" && err != nil {
		message = err.Error()
	}
	if message == "" {
		message = http.StatusText(status)
	}
	return &HTTPError{
		Status:  status,
		Code:    code,
		Message: message,
		Err:     err,
	}
}

type errorResponse struct {
	Error errorBody `json:"error"`
}

type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func defaultErrorHandler(c *Context, err error) {
	status := http.StatusInternalServerError
	code := "internal_error"
	message := http.StatusText(http.StatusInternalServerError)

	var httpErr *HTTPError
	switch {
	case errors.As(err, &httpErr):
		status = httpErr.Status
		if status == 0 {
			status = http.StatusInternalServerError
		}
		code = httpErr.Code
		if code == "" {
			code = statusCode(status)
		}
		message = httpErr.Message
		if message == "" {
			message = http.StatusText(status)
		}
	case isBindingError(err), isDecodeError(err):
		status = http.StatusBadRequest
		code = "bad_request"
		message = err.Error()
	case isValidationError(err):
		status = http.StatusUnprocessableEntity
		code = "unprocessable_entity"
		message = err.Error()
	}

	_ = c.JSON(status, errorResponse{
		Error: errorBody{
			Code:    code,
			Message: message,
		},
	})
}

func statusCode(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "bad_request"
	case http.StatusUnauthorized:
		return "unauthorized"
	case http.StatusForbidden:
		return "forbidden"
	case http.StatusNotFound:
		return "not_found"
	case http.StatusConflict:
		return "conflict"
	case http.StatusUnprocessableEntity:
		return "unprocessable_entity"
	default:
		return "http_error"
	}
}

func isDecodeError(err error) bool {
	var target *decodeError
	return errors.As(err, &target)
}
