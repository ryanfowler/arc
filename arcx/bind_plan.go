package arcx

import (
	"fmt"
	"reflect"
	"strings"
	"sync"
)

type bindSource int

const (
	sourceNone bindSource = iota
	sourceParam
	sourceQuery
	sourceHeader
	sourceCookie
	sourceBody
)

type bindPlan struct {
	inputType  reflect.Type
	structType reflect.Type
	pointer    bool
	fields     []bindField
	body       *bindField
}

type bindField struct {
	index        []int
	source       bindSource
	sourceName   string
	name         string
	required     bool
	hasDefault   bool
	defaultValue string
	fieldType    reflect.Type
}

var bindPlans sync.Map

func mustBindPlanFor[In any]() *bindPlan {
	t := reflect.TypeFor[In]()
	plan, err := getBindPlan(t)
	if err != nil {
		panic(err)
	}
	return plan
}

func getBindPlan(t reflect.Type) (*bindPlan, error) {
	if t == nil {
		return nil, fmt.Errorf("arcx: input type must not be nil")
	}
	if cached, ok := bindPlans.Load(t); ok {
		return cached.(*bindPlan), nil
	}
	plan, err := buildBindPlan(t)
	if err != nil {
		return nil, err
	}
	actual, _ := bindPlans.LoadOrStore(t, plan)
	return actual.(*bindPlan), nil
}

func buildBindPlan(inputType reflect.Type) (*bindPlan, error) {
	structType := inputType
	pointer := false
	if structType.Kind() == reflect.Pointer {
		pointer = true
		structType = structType.Elem()
	}
	if structType.Kind() != reflect.Struct {
		return nil, fmt.Errorf("arcx: input type %s must be a struct or pointer to struct", inputType)
	}

	plan := &bindPlan{
		inputType:  inputType,
		structType: structType,
		pointer:    pointer,
	}

	for _, field := range reflect.VisibleFields(structType) {
		if !field.IsExported() {
			continue
		}
		bf, ok, err := parseBindField(field)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		if bf.source == sourceBody {
			if plan.body != nil {
				return nil, fmt.Errorf("arcx: input type %s has multiple body fields", inputType)
			}
			if bf.name != "json" {
				return nil, fmt.Errorf("arcx: field %s body tag must be json", field.Name)
			}
			if bf.hasDefault {
				return nil, fmt.Errorf("arcx: field %s body tag does not support default", field.Name)
			}
			if !bf.required && field.Type.Kind() != reflect.Pointer {
				bf.required = true
			}
			body := bf
			plan.body = &body
			continue
		}
		if err := validateFieldType(field.Type); err != nil {
			return nil, fmt.Errorf("arcx: field %s: %w", field.Name, err)
		}
		plan.fields = append(plan.fields, bf)
	}

	return plan, nil
}

func parseBindField(field reflect.StructField) (bindField, bool, error) {
	var found bindField
	for _, tag := range []struct {
		key    string
		source bindSource
	}{
		{"param", sourceParam},
		{"query", sourceQuery},
		{"header", sourceHeader},
		{"cookie", sourceCookie},
		{"body", sourceBody},
	} {
		raw, ok := field.Tag.Lookup(tag.key)
		if !ok || raw == "-" {
			continue
		}
		if found.source != sourceNone {
			return bindField{}, false, fmt.Errorf("arcx: field %s has multiple binding tags", field.Name)
		}
		name, opts := parseTag(raw)
		if name == "" {
			if tag.source == sourceBody {
				name = "json"
			} else {
				return bindField{}, false, fmt.Errorf("arcx: field %s %s tag needs a name", field.Name, tag.key)
			}
		}
		found = bindField{
			index:      field.Index,
			source:     tag.source,
			sourceName: tag.key,
			name:       name,
			fieldType:  field.Type,
		}
		for _, opt := range opts {
			switch {
			case opt == "required":
				found.required = true
			case strings.HasPrefix(opt, "default="):
				found.hasDefault = true
				found.defaultValue = strings.TrimPrefix(opt, "default=")
			case opt == "":
			default:
				return bindField{}, false, fmt.Errorf("arcx: field %s has unknown %s option %q", field.Name, tag.key, opt)
			}
		}
	}
	if found.source == sourceNone {
		return bindField{}, false, nil
	}
	return found, true, nil
}

func parseTag(raw string) (string, []string) {
	parts := strings.Split(raw, ",")
	if len(parts) == 0 {
		return "", nil
	}
	return strings.TrimSpace(parts[0]), parts[1:]
}
