package v210

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"sort"
	"unicode/utf8"
)

type object = map[string]any

// decode rejects duplicate object keys rather than silently choosing a value.
func decode(raw []byte) (any, error) {
	if !utf8.Valid(raw) {
		return nil, fmt.Errorf("invalid UTF-8 JSON")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	var read func(int) (any, error)
	read = func(depth int) (any, error) {
		if depth > 128 {
			return nil, fmt.Errorf("JSON nesting exceeds limit")
		}
		t, err := d.Token()
		if err != nil {
			return nil, err
		}
		if delim, ok := t.(json.Delim); ok {
			switch delim {
			case '{':
				m := object{}
				for d.More() {
					key, err := d.Token()
					if err != nil {
						return nil, err
					}
					k, ok := key.(string)
					if !ok {
						return nil, fmt.Errorf("invalid key")
					}
					if _, exists := m[k]; exists {
						return nil, fmt.Errorf("duplicate JSON key %q", k)
					}
					v, err := read(depth + 1)
					if err != nil {
						return nil, err
					}
					m[k] = v
				}
				_, err = d.Token()
				return m, err
			case '[':
				a := []any{}
				for d.More() {
					v, err := read(depth + 1)
					if err != nil {
						return nil, err
					}
					a = append(a, v)
				}
				_, err = d.Token()
				return a, err
			default:
				return nil, fmt.Errorf("unexpected delimiter")
			}
		}
		return t, nil
	}
	v, err := read(0)
	if err != nil {
		return nil, err
	}
	if _, err = d.Token(); err != io.EOF {
		return nil, fmt.Errorf("trailing JSON data")
	}
	return v, nil
}

var integer = regexp.MustCompile(`^-?(0|[1-9][0-9]*)$`)

// SnapshotID hashes an input payload (without the envelope), preserving integer
// precision. Duplicate keys, fractional/exponent numbers and invalid UTF-8 fail.
func SnapshotID(payload []byte) (string, error) {
	v, err := decode(payload)
	if err != nil {
		return "", err
	}
	if _, ok := v.(object); !ok {
		return "", fmt.Errorf("snapshot payload must be an object")
	}
	return canonicalSnapshotID(v)
}

// canonicalSnapshotID implements the phase 1 integer-only UTF-8 format.
func canonicalSnapshotID(payload any) (string, error) {
	var b bytes.Buffer
	var write func(any) error
	write = func(v any) error {
		switch x := v.(type) {
		case object:
			keys := make([]string, 0, len(x))
			for k := range x {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			b.WriteByte('{')
			for i, k := range keys {
				if i > 0 {
					b.WriteByte(',')
				}
				if err := write(k); err != nil {
					return err
				}
				b.WriteByte(':')
				if err := write(x[k]); err != nil {
					return err
				}
			}
			b.WriteByte('}')
		case []any:
			b.WriteByte('[')
			for i, v := range x {
				if i > 0 {
					b.WriteByte(',')
				}
				if err := write(v); err != nil {
					return err
				}
			}
			b.WriteByte(']')
		case string:
			b.WriteByte('"')
			for _, r := range x {
				switch r {
				case '"', '\\':
					b.WriteByte('\\')
					b.WriteRune(r)
				case '\b':
					b.WriteString(`\b`)
				case '\f':
					b.WriteString(`\f`)
				case '\n':
					b.WriteString(`\n`)
				case '\r':
					b.WriteString(`\r`)
				case '\t':
					b.WriteString(`\t`)
				default:
					if r < 0x20 {
						fmt.Fprintf(&b, `\u%04x`, r)
					} else {
						b.WriteRune(r)
					}
				}
			}
			b.WriteByte('"')
		case json.Number:
			if !integer.MatchString(x.String()) {
				return fmt.Errorf("snapshot permits decimal integers only")
			}
			if x.String() == "-0" {
				b.WriteByte('0')
			} else {
				b.WriteString(x.String())
			}
		case bool:
			if x {
				b.WriteString("true")
			} else {
				b.WriteString("false")
			}
		case nil:
			b.WriteString("null")
		default:
			return fmt.Errorf("unsupported canonical value %T", v)
		}
		return nil
	}
	if err := write(payload); err != nil {
		return "", err
	}
	sum := sha256.Sum256(b.Bytes())
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func obj(v any) object { return v.(object) }
func arr(v any) []any  { return v.([]any) }
func str(v any) string { s, _ := v.(string); return s }
func stringsAt(v any) []string {
	out := []string{}
	for _, s := range arr(v) {
		out = append(out, str(s))
	}
	return out
}
func list(v []string) []any {
	out := []any{}
	for _, s := range v {
		out = append(out, s)
	}
	return out
}
