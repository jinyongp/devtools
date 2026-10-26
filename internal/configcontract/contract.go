// Package configcontract owns the embedded protocol contract and strict JSON input.
package configcontract

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/jinyongp/devtools/internal/versionrange"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/santhosh-tekuri/jsonschema/v6/kind"
	"golang.org/x/text/message"
)

const Limit = 2 << 20
const schemaURL = "urn:devtools:config-contract:protocol-15"

//go:embed schema.json
var schemaBytes []byte

var schemaTemplate struct {
	sync.Once
	value map[string]any
}

func template() map[string]any {
	schemaTemplate.Do(func() {
		if err := json.Unmarshal(schemaBytes, &schemaTemplate.value); err != nil {
			panic(err)
		}
	})
	return schemaTemplate.value
}
func copyJSON(v any) any {
	switch v := v.(type) {
	case map[string]any:
		out := map[string]any{}
		for k, item := range v {
			out[k] = copyJSON(item)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = copyJSON(item)
		}
		return out
	default:
		return v
	}
}

// Schema returns a fresh value so callers cannot modify the runtime contract.
func Schema() map[string]any {
	return copyJSON(template()).(map[string]any)
}

func Definition(name string) map[string]any {
	s := template()
	all := s["$defs"].(map[string]any)
	defs := map[string]any{}
	var include func(string)
	var visit func(any)
	visit = func(value any) {
		switch value := value.(type) {
		case map[string]any:
			if ref, ok := value["$ref"].(string); ok && strings.HasPrefix(ref, "#/$defs/") {
				include(strings.TrimPrefix(ref, "#/$defs/"))
			}
			for _, child := range value {
				visit(child)
			}
		case []any:
			for _, child := range value {
				visit(child)
			}
		}
	}
	include = func(name string) {
		if _, ok := defs[name]; ok {
			return
		}
		definition, ok := all[name]
		if !ok {
			panic("unknown contract definition: " + name)
		}
		defs[name] = copyJSON(definition)
		visit(definition)
	}
	include(name)
	return map[string]any{"$schema": s["$schema"], "$id": schemaURL + ":" + name, "$ref": "#/$defs/" + name, "$defs": defs}
}

type Issue struct {
	Facts  map[string]any
	Path   []string
	Reason string
	Offset int64
	Rule   string
}

func (e *Issue) Error() string { return e.Reason }

// Decode rejects lossy encoding/json recovery and numeric conversions before the
// schema engine sees an instance. Errors never include an input value.
func Decode(data []byte) (any, *Issue) {
	if len(data) > Limit {
		return nil, &Issue{Reason: "limit_exceeded", Rule: "maximum_bytes", Offset: -1}
	}
	if !utf8.Valid(data) || !validEscapes(data) {
		return nil, &Issue{Reason: "invalid_value", Rule: "unicode", Offset: -1}
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	var read func([]string) (any, *Issue)
	read = func(path []string) (any, *Issue) {
		if len(path) > 128 {
			return nil, &Issue{Path: []string{}, Reason: "limit_exceeded", Offset: d.InputOffset(), Rule: "maximum_depth_128"}
		}
		t, err := d.Token()
		bad := func(reason string) (any, *Issue) {
			return nil, &Issue{Path: append([]string{}, path...), Reason: reason, Offset: d.InputOffset()}
		}
		if err != nil {
			return bad("invalid_value")
		}
		switch t := t.(type) {
		case json.Delim:
			if t == '{' {
				m := map[string]any{}
				for d.More() {
					key, e := d.Token()
					if e != nil {
						return bad("invalid_value")
					}
					k, ok := key.(string)
					if !ok {
						return bad("invalid_type")
					}
					child := append(append([]string{}, path...), k)
					if _, exists := m[k]; exists {
						return nil, &Issue{Path: child, Reason: "duplicate_field", Offset: d.InputOffset()}
					}
					v, issue := read(child)
					if issue != nil {
						return nil, issue
					}
					m[k] = v
				}
				if _, e := d.Token(); e != nil {
					return bad("invalid_value")
				}
				return m, nil
			}
			if t == '[' {
				a := []any{}
				for d.More() {
					v, e := read(append(append([]string{}, path...), strconv.Itoa(len(a))))
					if e != nil {
						return nil, e
					}
					a = append(a, v)
				}
				if _, e := d.Token(); e != nil {
					return bad("invalid_value")
				}
				return a, nil
			}
			return bad("invalid_value")
		case json.Number:
			s := string(t)
			if len(s) > 20 || strings.ContainsAny(s, ".eE") {
				return bad("invalid_type")
			}
			n, e := strconv.ParseInt(s, 10, 64)
			if e != nil {
				return bad("invalid_value")
			}
			return n, nil
		default:
			return t, nil
		}
	}
	v, e := read(nil)
	if e != nil {
		return nil, e
	}
	if _, err := d.Token(); err != io.EOF {
		return nil, &Issue{Reason: "invalid_value", Offset: d.InputOffset(), Rule: "single_document"}
	}
	return v, nil
}

func validEscapes(data []byte) bool {
	for i := 0; i < len(data); i++ {
		if data[i] != '"' {
			continue
		}
		for i++; i < len(data) && data[i] != '"'; i++ {
			if data[i] != '\\' {
				continue
			}
			i++
			if i >= len(data) {
				return false
			}
			if data[i] != 'u' {
				continue
			}
			if i+4 >= len(data) {
				return false
			}
			n, e := strconv.ParseUint(string(data[i+1:i+5]), 16, 16)
			if e != nil {
				return false
			}
			i += 4
			if n >= 0xdc00 && n <= 0xdfff {
				return false
			}
			if n >= 0xd800 && n <= 0xdbff {
				if i+6 >= len(data) || data[i+1] != '\\' || data[i+2] != 'u' {
					return false
				}
				low, e := strconv.ParseUint(string(data[i+3:i+7]), 16, 16)
				if e != nil || low < 0xdc00 || low > 0xdfff {
					return false
				}
				i += 6
			}
		}
	}
	return true
}

type offlineLoader struct{}

func (offlineLoader) Load(string) (any, error) {
	return nil, errors.New("external schema loading is disabled")
}

type byteLimit int

func (b byteLimit) Validate(ctx *jsonschema.ValidatorContext, v any) {
	if s, ok := v.(string); ok && len(s) > int(b) {
		ctx.AddError(b)
	}
}
func (b byteLimit) KeywordPath() []string { return []string{"x-maxUtf8Bytes"} }
func (b byteLimit) LocalizedString(p *message.Printer) string {
	return p.Sprintf("UTF-8 byte limit is %d", int(b))
}

var compiled struct {
	sync.Once
	definitions map[string]*jsonschema.Schema
	err         error
}

type npmRange struct{}

func (npmRange) Validate(ctx *jsonschema.ValidatorContext, v any) {
	if s, ok := v.(string); ok && !versionrange.Valid(s) {
		ctx.AddError(npmRange{})
	}
}
func (npmRange) KeywordPath() []string { return []string{"x-npmRange"} }
func (npmRange) LocalizedString(p *message.Printer) string {
	return p.Sprintf("Expected a nonempty npm SemVer range of at most 128 bytes")
}

func compile() {
	c := jsonschema.NewCompiler()
	c.UseLoader(offlineLoader{})
	c.AssertVocabs()
	c.RegisterVocabulary(&jsonschema.Vocabulary{URL: "urn:devtools:npm-range", Compile: func(_ *jsonschema.CompilerContext, obj map[string]any) (jsonschema.SchemaExt, error) {
		if obj["x-npmRange"] == true {
			return npmRange{}, nil
		}
		return nil, nil
	}})
	c.RegisterVocabulary(&jsonschema.Vocabulary{URL: "urn:devtools:utf8-bytes", Compile: func(_ *jsonschema.CompilerContext, obj map[string]any) (jsonschema.SchemaExt, error) {
		if n, ok := obj["x-maxUtf8Bytes"].(float64); ok {
			return byteLimit(int(n)), nil
		}
		return nil, nil
	}})
	s := Schema()
	if err := c.AddResource(schemaURL, s); err != nil {
		compiled.err = err
		return
	}
	compiled.definitions = map[string]*jsonschema.Schema{}
	for name := range s["$defs"].(map[string]any) {
		sch, err := c.Compile(schemaURL + "#/$defs/" + name)
		if err != nil {
			compiled.err = err
			return
		}
		compiled.definitions[name] = sch
	}
}

func Validate(name string, v any) []Issue {
	compiled.Do(compile)
	if compiled.err != nil {
		panic(compiled.err)
	}
	sch := compiled.definitions[name]
	if sch == nil {
		panic("unknown contract definition: " + name)
	}
	err := sch.Validate(v)
	if err == nil {
		return nil
	}
	var validation *jsonschema.ValidationError
	if !errors.As(err, &validation) {
		panic(err)
	}
	issues := []Issue{}
	var walk func(*jsonschema.ValidationError)
	walk = func(e *jsonschema.ValidationError) {
		if len(e.Causes) > 0 {
			for _, child := range e.Causes {
				walk(child)
			}
			return
		}
		rule := strings.Join(e.ErrorKind.KeywordPath(), "/")
		facts := map[string]any{}
		switch k := e.ErrorKind.(type) {
		case *kind.Type:
			facts["expected_type"] = strings.Join(k.Want, " or ")
			facts["observed_type"] = k.Got
		case *kind.Enum:
			allowed := []string{}
			for _, v := range k.Want {
				if s, ok := v.(string); ok {
					allowed = append(allowed, s)
				}
			}
			facts["allowed"] = allowed
		case *kind.Minimum:
			n, _ := k.Want.Float64()
			facts["minimum"] = n
		case *kind.Maximum:
			n, _ := k.Want.Float64()
			facts["maximum"] = n
		case *kind.MinLength:
			facts["minimum"] = k.Want
		case *kind.MaxLength:
			facts["maximum"] = k.Want
		case *kind.Pattern:
			facts["rule"] = k.Want
		}
		switch k := e.ErrorKind.(type) {
		case *kind.AdditionalProperties:
			for _, key := range k.Properties {
				issues = append(issues, Issue{Path: append(append([]string{}, e.InstanceLocation...), key), Reason: "unknown_field", Rule: rule})
			}
			return
		case *kind.Required:
			for _, key := range k.Missing {
				issues = append(issues, Issue{Path: append(append([]string{}, e.InstanceLocation...), key), Reason: "missing_argument", Rule: rule})
			}
			return
		}
		reason := "invalid_value"
		if rule == "type" {
			reason = "invalid_type"
		}
		if strings.HasPrefix(rule, "additionalProperties") {
			reason = "unknown_field"
		}
		issues = append(issues, Issue{Path: append([]string{}, e.InstanceLocation...), Reason: reason, Rule: rule, Facts: facts})
	}
	walk(validation)
	return issues
}
