package arcx

import "net/http"

// JSON adapts a typed function into a Handler that binds request data and
// writes a JSON response.
func JSON[In, Out any](fn func(*Context, In) (Out, error)) Handler {
	plan := mustBindPlanFor[In]()
	return func(c *Context) error {
		in, err := bindInput[In](c, plan)
		if err != nil {
			return err
		}
		if err := validateInput(c, in); err != nil {
			return err
		}
		out, err := fn(c, in)
		if err != nil {
			return err
		}
		if response, ok := any(out).(responseValue); ok {
			return response.writeResponse(c)
		}
		return c.JSON(http.StatusOK, out)
	}
}

// NoContent adapts a typed function into a Handler that binds request data and
// writes 204 No Content when the function returns nil.
func NoContent[In any](fn func(*Context, In) error) Handler {
	plan := mustBindPlanFor[In]()
	return func(c *Context) error {
		in, err := bindInput[In](c, plan)
		if err != nil {
			return err
		}
		if err := validateInput(c, in); err != nil {
			return err
		}
		if err := fn(c, in); err != nil {
			return err
		}
		return c.NoContent(http.StatusNoContent)
	}
}

// Raw returns h unchanged.
func Raw(h Handler) Handler {
	return h
}

// FromHTTP adapts an ordinary http.Handler into an arcx Handler.
func FromHTTP(h http.Handler) Handler {
	return func(c *Context) error {
		if h == nil {
			http.NotFound(c.ResponseWriter, c.Request)
			return nil
		}
		h.ServeHTTP(c.ResponseWriter, c.Request)
		return nil
	}
}
