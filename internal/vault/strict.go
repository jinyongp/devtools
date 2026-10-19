package vault

import (
	"bytes"
	"encoding/json"
	"io"
	"reflect"
	"strings"
	"unicode/utf8"
)

// StrictDecode rejects ambiguities before schema decoding. encoding/json alone
// accepts duplicate object keys and replaces invalid UTF-8 in strings.
func StrictDecode(data []byte, target any) error {
	if len(data) > MaxDocumentBytes {
		return ErrTooLarge
	}
	if !utf8.Valid(data) {
		return ErrInvalid
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	fieldsCache := map[reflect.Type]map[string]reflect.Type{}
	var fieldsFor func(reflect.Type) map[string]reflect.Type
	fieldsFor = func(t reflect.Type) map[string]reflect.Type {
		for t != nil && t.Kind() == reflect.Pointer {
			t = t.Elem()
		}
		if t == nil || t.Kind() != reflect.Struct {
			return nil
		}
		if fields, ok := fieldsCache[t]; ok {
			return fields
		}
		fields := map[string]reflect.Type{}
		fieldsCache[t] = fields
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			if !f.IsExported() {
				continue
			}
			tag := strings.Split(f.Tag.Get("json"), ",")[0]
			if tag == "-" {
				continue
			}
			embedded := f.Type
			for embedded.Kind() == reflect.Pointer {
				embedded = embedded.Elem()
			}
			if f.Anonymous && tag == "" && embedded.Kind() == reflect.Struct {
				for name, typ := range fieldsFor(f.Type) {
					fields[name] = typ
				}
				continue
			}
			if tag == "" {
				tag = f.Name
			}
			fields[tag] = f.Type
		}
		return fields
	}
	var value func(int, reflect.Type) error
	value = func(depth int, expected reflect.Type) error {
		if depth > 64 {
			return ErrInvalid
		}
		t, err := d.Token()
		if err != nil {
			return ErrInvalid
		}
		delim, ok := t.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			for expected != nil && expected.Kind() == reflect.Pointer {
				expected = expected.Elem()
			}
			fields := fieldsFor(expected)
			keys := map[string]bool{}
			for d.More() {
				t, err := d.Token()
				if err != nil {
					return ErrInvalid
				}
				key, ok := t.(string)
				if !ok || keys[key] {
					return ErrInvalid
				}
				keys[key] = true
				var child reflect.Type
				if fields != nil {
					var known bool
					child, known = fields[key]
					// Go's decoder accepts case-folded aliases. Our closed wire
					// schema uses exact field names and must not accept two
					// differently cased spellings of the same struct field.
					if !known {
						return ErrInvalid
					}
				} else if expected != nil && expected.Kind() == reflect.Map {
					child = expected.Elem()
				}
				if err := value(depth+1, child); err != nil {
					return err
				}
			}
		case '[':
			for expected != nil && expected.Kind() == reflect.Pointer {
				expected = expected.Elem()
			}
			var child reflect.Type
			if expected != nil && (expected.Kind() == reflect.Array || expected.Kind() == reflect.Slice) {
				child = expected.Elem()
			}
			for d.More() {
				if err := value(depth+1, child); err != nil {
					return err
				}
			}
		default:
			return ErrInvalid
		}
		end, err := d.Token()
		if err != nil || (delim == '{' && end != json.Delim('}')) || (delim == '[' && end != json.Delim(']')) {
			return ErrInvalid
		}
		return nil
	}
	if err := value(0, reflect.TypeOf(target)); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return ErrInvalid
	}
	d = json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(target); err != nil {
		return ErrInvalid
	}
	return nil
}
