package arcx

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
)

// Codec decodes request bodies and encodes responses.
type Codec interface {
	Decode(*http.Request, any) error
	Encode(http.ResponseWriter, *http.Request, int, any) error
}

// JSONOptions configures the default JSON codec.
type JSONOptions struct {
	MaxBodyBytes          int64
	DisallowUnknownFields bool
}

type jsonCodec struct {
	options JSONOptions
}

func (c jsonCodec) Decode(req *http.Request, v any) error {
	if req.Body == nil {
		return &decodeError{err: errors.New("missing request body")}
	}
	body := req.Body
	if c.options.MaxBodyBytes > 0 {
		body = http.MaxBytesReader(nil, body, c.options.MaxBodyBytes)
	}

	dec := json.NewDecoder(body)
	if c.options.DisallowUnknownFields {
		dec.DisallowUnknownFields()
	}
	if err := dec.Decode(v); err != nil {
		return &decodeError{err: err}
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		if err == nil {
			err = errors.New("request body must contain a single JSON value")
		}
		return &decodeError{err: err}
	}
	return nil
}

func (c jsonCodec) Encode(w http.ResponseWriter, _ *http.Request, status int, v any) error {
	if status == 0 {
		status = http.StatusOK
	}
	header := w.Header()
	if header.Get("Content-Type") == "" {
		header.Set("Content-Type", "application/json; charset=utf-8")
	}
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		return fmt.Errorf("encode JSON response: %w", err)
	}
	return nil
}

type decodeError struct {
	err error
}

func (e *decodeError) Error() string {
	return e.err.Error()
}

func (e *decodeError) Unwrap() error {
	return e.err
}
