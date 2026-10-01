package cartridge

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
)

// decodeValues copies form, query, or route values into the struct out.
// A field matches only the name in its tag (for example `form:"email"`),
// ignoring case. Fields without the tag, or with "-", are never set, so a
// client cannot set a field the struct does not expose. A value that does
// not fit its field gives an *Error with status 400.
func decodeValues(values url.Values, tag string, out any) error {
	v := reflect.ValueOf(out)
	if v.Kind() != reflect.Pointer || v.Elem().Kind() != reflect.Struct {
		return fmt.Errorf("cartridge: decode target must be a pointer to a struct")
	}
	return decodeStruct(values, tag, v.Elem())
}

// errUnsupportedType marks a field type that decodeValues cannot set.
var errUnsupportedType = errors.New("unsupported type")

func decodeStruct(values url.Values, tag string, v reflect.Value) error {
	t := v.Type()
	for i := range t.NumField() {
		field := t.Field(i)
		if !field.IsExported() {
			continue
		}
		if field.Anonymous && field.Type.Kind() == reflect.Struct {
			if err := decodeStruct(values, tag, v.Field(i)); err != nil {
				return err
			}
			continue
		}

		name, _, _ := strings.Cut(field.Tag.Get(tag), ",")
		if name == "" || name == "-" {
			continue
		}

		raw, ok := lookupFold(values, name)
		if !ok {
			continue
		}
		if err := setField(v.Field(i), raw); errors.Is(err, errUnsupportedType) {
			return fmt.Errorf("cartridge: field %q: %w", name, err)
		} else if err != nil {
			return NewError(http.StatusBadRequest, fmt.Sprintf("invalid value for %q", name))
		}
	}
	return nil
}

func lookupFold(values url.Values, name string) ([]string, bool) {
	if raw, ok := values[name]; ok {
		return raw, true
	}
	for key, raw := range values {
		if strings.EqualFold(key, name) {
			return raw, true
		}
	}
	return nil, false
}

func setField(f reflect.Value, raw []string) error {
	if len(raw) == 0 {
		return nil
	}
	switch f.Kind() {
	case reflect.Pointer:
		elem := reflect.New(f.Type().Elem())
		if err := setField(elem.Elem(), raw); err != nil {
			return err
		}
		f.Set(elem)
		return nil
	case reflect.Slice:
		slice := reflect.MakeSlice(f.Type(), len(raw), len(raw))
		for i, s := range raw {
			if err := setScalar(slice.Index(i), s); err != nil {
				return err
			}
		}
		f.Set(slice)
		return nil
	}
	return setScalar(f, raw[0])
}

func setScalar(f reflect.Value, s string) error {
	switch f.Kind() {
	case reflect.String:
		f.SetString(s)
	case reflect.Bool:
		if s == "" {
			f.SetBool(false)
			return nil
		}
		if s == "on" {
			f.SetBool(true)
			return nil
		}
		b, err := strconv.ParseBool(s)
		if err != nil {
			return err
		}
		f.SetBool(b)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if s == "" {
			return nil
		}
		n, err := strconv.ParseInt(s, 10, f.Type().Bits())
		if err != nil {
			return err
		}
		f.SetInt(n)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if s == "" {
			return nil
		}
		n, err := strconv.ParseUint(s, 10, f.Type().Bits())
		if err != nil {
			return err
		}
		f.SetUint(n)
	case reflect.Float32, reflect.Float64:
		if s == "" {
			return nil
		}
		n, err := strconv.ParseFloat(s, f.Type().Bits())
		if err != nil {
			return err
		}
		f.SetFloat(n)
	default:
		return fmt.Errorf("%w %s", errUnsupportedType, f.Type())
	}
	return nil
}
