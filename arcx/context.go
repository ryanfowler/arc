package arcx

import (
	"context"
	"net/http"
)

// Context exposes request and response helpers for arcx handlers.
type Context struct {
	ResponseWriter http.ResponseWriter
	Request        *http.Request

	cfg *Config
}

// Context returns the request context.
func (c *Context) Context() context.Context {
	return c.Request.Context()
}

// Pattern returns the Arc route pattern selected for the request.
func (c *Context) Pattern() string {
	return c.Request.Pattern
}

// Header returns the response headers.
func (c *Context) Header() http.Header {
	return c.ResponseWriter.Header()
}

// Status writes status with no response body.
func (c *Context) Status(status int) error {
	c.ResponseWriter.WriteHeader(status)
	return nil
}

// NoContent writes status with no response body.
func (c *Context) NoContent(status int) error {
	c.ResponseWriter.WriteHeader(status)
	return nil
}

// JSON writes v as a JSON response with status.
func (c *Context) JSON(status int, v any) error {
	return c.cfg.codec("application/json").Encode(c.ResponseWriter, c.Request, status, v)
}

// Decode decodes the request body as JSON into v.
func (c *Context) Decode(v any) error {
	return c.cfg.codec("application/json").Decode(c.Request, v)
}

// Param returns a path parameter value.
func (c *Context) Param(name string) Value {
	value := c.Request.PathValue(name)
	return Value{value: value, ok: value != "", name: name, where: "path parameter"}
}

// Query returns the first query parameter value for name.
func (c *Context) Query(name string) Value {
	values, ok := c.Request.URL.Query()[name]
	if !ok || len(values) == 0 {
		return Value{name: name, where: "query parameter"}
	}
	return Value{value: values[0], ok: true, name: name, where: "query parameter"}
}

// HeaderValue returns the first request header value for name.
func (c *Context) HeaderValue(name string) Value {
	values, ok := c.Request.Header[name]
	if !ok || len(values) == 0 {
		values, ok = c.Request.Header[http.CanonicalHeaderKey(name)]
	}
	if !ok || len(values) == 0 {
		return Value{name: name, where: "header"}
	}
	return Value{value: values[0], ok: true, name: name, where: "header"}
}

// Cookie returns a request cookie value for name.
func (c *Context) Cookie(name string) Value {
	cookie, err := c.Request.Cookie(name)
	if err != nil {
		return Value{name: name, where: "cookie"}
	}
	return Value{value: cookie.Value, ok: true, name: name, where: "cookie"}
}
