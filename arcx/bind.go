package arcx

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"reflect"
)

// Validator validates a bound input value.
type Validator interface {
	Validate() error
}

// ContextValidator validates a bound input value with request context.
type ContextValidator interface {
	Validate(context.Context) error
}

type bindingError struct {
	message string
}

func (e *bindingError) Error() string {
	return e.message
}

type validationError struct {
	err error
}

func (e *validationError) Error() string {
	return e.err.Error()
}

func (e *validationError) Unwrap() error {
	return e.err
}

func newBindingError(format string, args ...any) error {
	return &bindingError{message: fmt.Sprintf(format, args...)}
}

func isBindingError(err error) bool {
	var target *bindingError
	return errors.As(err, &target)
}

func isValidationError(err error) bool {
	var target *validationError
	return errors.As(err, &target)
}

func bindInput[In any](c *Context, plan *bindPlan) (In, error) {
	var zero In
	if plan == nil {
		return zero, nil
	}

	root := reflect.New(plan.structType)
	if err := plan.bind(c, root.Elem()); err != nil {
		return zero, err
	}
	if plan.pointer {
		return root.Interface().(In), nil
	}
	return root.Elem().Interface().(In), nil
}

func validateInput[In any](c *Context, in In) error {
	if validator, ok := any(in).(ContextValidator); ok {
		if err := validator.Validate(c.Context()); err != nil {
			return &validationError{err: err}
		}
		return nil
	}
	if validator, ok := any(in).(Validator); ok {
		if err := validator.Validate(); err != nil {
			return &validationError{err: err}
		}
		return nil
	}

	value := reflect.ValueOf(in)
	if !value.IsValid() {
		return nil
	}
	if value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return nil
		}
	} else if value.CanAddr() {
		value = value.Addr()
	} else {
		copy := reflect.New(value.Type())
		copy.Elem().Set(value)
		value = copy
	}
	if validator, ok := value.Interface().(ContextValidator); ok {
		if err := validator.Validate(c.Context()); err != nil {
			return &validationError{err: err}
		}
		return nil
	}
	if validator, ok := value.Interface().(Validator); ok {
		if err := validator.Validate(); err != nil {
			return &validationError{err: err}
		}
	}
	return nil
}

func (p *bindPlan) bind(c *Context, root reflect.Value) error {
	for _, field := range p.fields {
		if err := field.bind(c, root); err != nil {
			return err
		}
	}
	if p.body != nil {
		field := fieldByIndexAlloc(root, p.body.index)
		if isEmptyBody(c.Request) && field.Kind() == reflect.Pointer && !p.body.required {
			return nil
		}
		if err := c.Decode(field.Addr().Interface()); err != nil {
			return err
		}
	}
	return nil
}

func (f bindField) bind(c *Context, root reflect.Value) error {
	field := fieldByIndexAlloc(root, f.index)
	switch f.source {
	case sourceParam:
		value := c.Request.PathValue(f.name)
		return setFieldStrings(field, f, []string{value}, value != "")
	case sourceQuery:
		values, ok := c.Request.URL.Query()[f.name]
		return setFieldStrings(field, f, values, ok)
	case sourceHeader:
		values, ok := c.Request.Header[http.CanonicalHeaderKey(f.name)]
		if !ok {
			values = c.Request.Header.Values(f.name)
			ok = len(values) != 0
		}
		return setFieldStrings(field, f, values, ok)
	case sourceCookie:
		cookie, err := c.Request.Cookie(f.name)
		if err != nil {
			return setFieldStrings(field, f, nil, false)
		}
		return setFieldStrings(field, f, []string{cookie.Value}, true)
	default:
		return nil
	}
}

func isEmptyBody(req *http.Request) bool {
	return req.Body == nil || req.Body == http.NoBody || req.ContentLength == 0
}

func fieldByIndexAlloc(root reflect.Value, index []int) reflect.Value {
	field := root
	for i, x := range index {
		if field.Kind() == reflect.Pointer {
			if field.IsNil() {
				field.Set(reflect.New(field.Type().Elem()))
			}
			field = field.Elem()
		}
		field = field.Field(x)
		if i == len(index)-1 {
			return field
		}
	}
	return field
}
