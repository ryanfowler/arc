package arcx

import (
	"encoding"
	"fmt"
	"reflect"
	"strconv"
	"time"
)

var (
	textUnmarshalerType = reflect.TypeOf((*encoding.TextUnmarshaler)(nil)).Elem()
	timeType            = reflect.TypeOf(time.Time{})
	durationType        = reflect.TypeOf(time.Duration(0))
)

func validateFieldType(t reflect.Type) error {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() == reflect.Slice {
		return validateScalarType(t.Elem())
	}
	return validateScalarType(t)
}

func validateScalarType(t reflect.Type) error {
	if t == timeType || t == durationType {
		return nil
	}
	if t.Implements(textUnmarshalerType) || reflect.PointerTo(t).Implements(textUnmarshalerType) {
		return nil
	}
	switch t.Kind() {
	case reflect.String, reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr,
		reflect.Float32, reflect.Float64:
		return nil
	default:
		return fmt.Errorf("unsupported type %s", t)
	}
}

func setFieldStrings(field reflect.Value, meta bindField, values []string, present bool) error {
	if !present {
		if meta.hasDefault {
			values = []string{meta.defaultValue}
			present = true
		} else if meta.required {
			return newBindingError("%s %q is required", meta.sourceName, meta.name)
		} else {
			return nil
		}
	}
	if meta.required && isEmptyValues(values) {
		return newBindingError("%s %q is required", meta.sourceName, meta.name)
	}
	return setReflectValue(field, values)
}

func isEmptyValues(values []string) bool {
	if len(values) == 0 {
		return true
	}
	for _, value := range values {
		if value != "" {
			return false
		}
	}
	return true
}

func setReflectValue(dst reflect.Value, values []string) error {
	if dst.Kind() == reflect.Pointer {
		if len(values) == 0 {
			return nil
		}
		ptr := reflect.New(dst.Type().Elem())
		if err := setReflectValue(ptr.Elem(), values); err != nil {
			return err
		}
		dst.Set(ptr)
		return nil
	}

	if dst.Kind() == reflect.Slice {
		slice := reflect.MakeSlice(dst.Type(), 0, len(values))
		for _, value := range values {
			elem := reflect.New(dst.Type().Elem()).Elem()
			if err := setScalar(elem, value); err != nil {
				return err
			}
			slice = reflect.Append(slice, elem)
		}
		dst.Set(slice)
		return nil
	}

	if len(values) == 0 {
		return nil
	}
	return setScalar(dst, values[0])
}

func setScalar(dst reflect.Value, value string) error {
	if dst.Type() == timeType {
		parsed, err := time.Parse(time.RFC3339, value)
		if err != nil {
			return newBindingError("invalid time %q: %v", value, err)
		}
		dst.Set(reflect.ValueOf(parsed))
		return nil
	}
	if dst.Type() == durationType {
		parsed, err := time.ParseDuration(value)
		if err != nil {
			return newBindingError("invalid duration %q: %v", value, err)
		}
		dst.Set(reflect.ValueOf(parsed))
		return nil
	}
	if dst.CanAddr() && dst.Addr().Type().Implements(textUnmarshalerType) {
		if err := dst.Addr().Interface().(encoding.TextUnmarshaler).UnmarshalText([]byte(value)); err != nil {
			return newBindingError("invalid value %q: %v", value, err)
		}
		return nil
	}
	if dst.Type().Implements(textUnmarshalerType) {
		if err := dst.Interface().(encoding.TextUnmarshaler).UnmarshalText([]byte(value)); err != nil {
			return newBindingError("invalid value %q: %v", value, err)
		}
		return nil
	}

	switch dst.Kind() {
	case reflect.String:
		dst.SetString(value)
	case reflect.Bool:
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return newBindingError("invalid bool %q: %v", value, err)
		}
		dst.SetBool(parsed)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		parsed, err := strconv.ParseInt(value, 10, dst.Type().Bits())
		if err != nil {
			return newBindingError("invalid integer %q: %v", value, err)
		}
		dst.SetInt(parsed)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		parsed, err := strconv.ParseUint(value, 10, dst.Type().Bits())
		if err != nil {
			return newBindingError("invalid unsigned integer %q: %v", value, err)
		}
		dst.SetUint(parsed)
	case reflect.Float32, reflect.Float64:
		parsed, err := strconv.ParseFloat(value, dst.Type().Bits())
		if err != nil {
			return newBindingError("invalid float %q: %v", value, err)
		}
		dst.SetFloat(parsed)
	default:
		return newBindingError("unsupported type %s", dst.Type())
	}
	return nil
}
