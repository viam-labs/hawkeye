package util

import (
	"reflect"

	"github.com/pkg/errors"
)

// EnsureAllStructFieldsAreSet ensures that each field of a struct is set to a non-zero value.
func EnsureAllStructFieldsAreSet(someStruct any) error {
	theStruct := reflect.ValueOf(someStruct)
	if theStruct.Kind() != reflect.Struct {
		return errors.New("input is not a struct")
	}

	for i := range theStruct.NumField() {
		field := theStruct.Field(i)
		if field.IsZero() {
			return errors.Errorf("field %q must be set", theStruct.Type().Field(i).Name)
		}
	}

	return nil
}

// Ptr returns a pointer to the provided value, typically a constant.
func Ptr[T any](val T) *T {
	return &val
}
