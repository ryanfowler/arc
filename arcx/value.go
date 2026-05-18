package arcx

import (
	"strconv"
	"time"
)

// Value is a request value with convenience parsers.
type Value struct {
	value string
	ok    bool
	name  string
	where string
}

// String returns the raw value, or an empty string when absent.
func (v Value) String() string {
	return v.value
}

// Required returns the raw value or an error when absent or empty.
func (v Value) Required() (string, error) {
	if !v.ok || v.value == "" {
		return "", newBindingError("%s %q is required", v.where, v.name)
	}
	return v.value, nil
}

// Bool parses the value as a bool.
func (v Value) Bool() (bool, error) {
	s, err := v.Required()
	if err != nil {
		return false, err
	}
	parsed, err := strconv.ParseBool(s)
	if err != nil {
		return false, newBindingError("invalid %s %q: %v", v.where, v.name, err)
	}
	return parsed, nil
}

// Int parses the value as an int.
func (v Value) Int() (int, error) {
	s, err := v.Required()
	if err != nil {
		return 0, err
	}
	parsed, err := strconv.ParseInt(s, 10, strconv.IntSize)
	if err != nil {
		return 0, newBindingError("invalid %s %q: %v", v.where, v.name, err)
	}
	return int(parsed), nil
}

// Int64 parses the value as an int64.
func (v Value) Int64() (int64, error) {
	s, err := v.Required()
	if err != nil {
		return 0, err
	}
	parsed, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, newBindingError("invalid %s %q: %v", v.where, v.name, err)
	}
	return parsed, nil
}

// Uint64 parses the value as a uint64.
func (v Value) Uint64() (uint64, error) {
	s, err := v.Required()
	if err != nil {
		return 0, err
	}
	parsed, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0, newBindingError("invalid %s %q: %v", v.where, v.name, err)
	}
	return parsed, nil
}

// Float64 parses the value as a float64.
func (v Value) Float64() (float64, error) {
	s, err := v.Required()
	if err != nil {
		return 0, err
	}
	parsed, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, newBindingError("invalid %s %q: %v", v.where, v.name, err)
	}
	return parsed, nil
}

// Duration parses the value as a time.Duration.
func (v Value) Duration() (time.Duration, error) {
	s, err := v.Required()
	if err != nil {
		return 0, err
	}
	parsed, err := time.ParseDuration(s)
	if err != nil {
		return 0, newBindingError("invalid %s %q: %v", v.where, v.name, err)
	}
	return parsed, nil
}

// Time parses the value using layout.
func (v Value) Time(layout string) (time.Time, error) {
	s, err := v.Required()
	if err != nil {
		return time.Time{}, err
	}
	parsed, err := time.Parse(layout, s)
	if err != nil {
		return time.Time{}, newBindingError("invalid %s %q: %v", v.where, v.name, err)
	}
	return parsed, nil
}
