package jsonout

import (
	"reflect"

	"github.com/pethost-dev/sdk/go/sandboxes"
)

// A sandbox's Connection is written without its token, which is root in the sandbox: wherever
// a value holds one (ConnectSandbox's answer, a Sandbox that CreateSandbox, ForkSandbox or a
// start answered), what is written is a copy with the token left out, and the value stays as it
// was, since the SDK keeps the connection for the calls that need it.

var connectionType = reflect.TypeFor[sandboxes.Connection]()

// withoutTokens is value with no Connection's token in it.
func withoutTokens(value any) any {
	if cleared, changed := clearTokens(reflect.ValueOf(value)); changed {
		return cleared.Interface()
	}
	return value
}

// clearTokens is v with every Connection's token in it cleared, copying what leads to one and
// sharing the rest; false = v holds no token, and is returned as it is. Only exported fields are
// followed: a Connection is an SDK type's exported field, at any depth, or an element of one.
func clearTokens(v reflect.Value) (reflect.Value, bool) {
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		if v.IsNil() {
			return v, false
		}
		inner, changed := clearTokens(v.Elem())
		if !changed || v.Kind() == reflect.Interface {
			return inner, changed
		}
		pointer := reflect.New(inner.Type())
		pointer.Elem().Set(inner)
		return pointer, true

	case reflect.Struct:
		if v.Type() == connectionType {
			return clearedConnection(v)
		}
		return clearFields(v)

	case reflect.Slice, reflect.Array:
		return clearElements(v)

	case reflect.Map:
		return clearValues(v)
	}
	return v, false
}

func clearedConnection(v reflect.Value) (reflect.Value, bool) {
	if v.FieldByName("Token").String() == "" {
		return v, false
	}
	copied := copyOf(v)
	copied.FieldByName("Token").SetString("")
	return copied, true
}

func clearFields(v reflect.Value) (reflect.Value, bool) {
	var copied reflect.Value
	for i := range v.NumField() {
		if !v.Type().Field(i).IsExported() {
			continue
		}
		field, changed := clearTokens(v.Field(i))
		if !changed {
			continue
		}
		if !copied.IsValid() {
			copied = copyOf(v)
		}
		copied.Field(i).Set(field)
	}
	return copied, copied.IsValid()
}

func clearElements(v reflect.Value) (reflect.Value, bool) {
	var copied reflect.Value
	for i := range v.Len() {
		element, changed := clearTokens(v.Index(i))
		if !changed {
			continue
		}
		if !copied.IsValid() {
			copied = elementsOf(v)
		}
		copied.Index(i).Set(element)
	}
	return copied, copied.IsValid()
}

func clearValues(v reflect.Value) (reflect.Value, bool) {
	var copied reflect.Value
	for entry := v.MapRange(); entry.Next(); {
		value, changed := clearTokens(entry.Value())
		if !changed {
			continue
		}
		if !copied.IsValid() {
			copied = reflect.MakeMapWithSize(v.Type(), v.Len())
			for all := v.MapRange(); all.Next(); {
				copied.SetMapIndex(all.Key(), all.Value())
			}
		}
		copied.SetMapIndex(entry.Key(), value)
	}
	return copied, copied.IsValid()
}

// elementsOf is a settable copy of an array or a slice: a slice's in an array of its own.
func elementsOf(v reflect.Value) reflect.Value {
	if v.Kind() == reflect.Array {
		return copyOf(v)
	}
	copied := reflect.MakeSlice(v.Type(), v.Len(), v.Len())
	reflect.Copy(copied, v)
	return copied
}

// copyOf is a settable copy of v.
func copyOf(v reflect.Value) reflect.Value {
	copied := reflect.New(v.Type()).Elem()
	copied.Set(v)
	return copied
}
