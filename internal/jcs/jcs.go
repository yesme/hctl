// Package jcs implements the RFC 8785 subset used by hctl obligation
// identities. Parsing is deliberately stricter than encoding/json: duplicate
// object names, invalid UTF-8, and lone UTF-16 surrogates are rejected before
// values can be normalized or overwritten.
package jcs

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

func Parse(raw []byte) (any, error) {
	if !utf8.Valid(raw) {
		return nil, errors.New("JSON is not valid UTF-8")
	}
	if err := validateRawStrings(raw); err != nil {
		return nil, err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	value, err := decodeValue(dec)
	if err != nil {
		return nil, err
	}
	if _, err := dec.Token(); err != io.EOF {
		if err == nil {
			return nil, errors.New("multiple top-level JSON values")
		}
		return nil, fmt.Errorf("trailing JSON data: %w", err)
	}
	return value, nil
}

func Canonicalize(raw []byte) ([]byte, error) {
	value, err := Parse(raw)
	if err != nil {
		return nil, err
	}
	return Marshal(value)
}

func Marshal(value any) ([]byte, error) {
	var out bytes.Buffer
	if err := appendValue(&out, value); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func decodeValue(dec *json.Decoder) (any, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '{':
			obj := make(map[string]any)
			for dec.More() {
				keyToken, err := dec.Token()
				if err != nil {
					return nil, err
				}
				key, ok := keyToken.(string)
				if !ok {
					return nil, errors.New("JSON object name is not a string")
				}
				if _, duplicate := obj[key]; duplicate {
					return nil, fmt.Errorf("duplicate JSON object name %q", key)
				}
				v, err := decodeValue(dec)
				if err != nil {
					return nil, err
				}
				obj[key] = v
			}
			end, err := dec.Token()
			if err != nil {
				return nil, err
			}
			if end != json.Delim('}') {
				return nil, errors.New("JSON object is not terminated")
			}
			return obj, nil
		case '[':
			var array []any
			for dec.More() {
				v, err := decodeValue(dec)
				if err != nil {
					return nil, err
				}
				array = append(array, v)
			}
			end, err := dec.Token()
			if err != nil {
				return nil, err
			}
			if end != json.Delim(']') {
				return nil, errors.New("JSON array is not terminated")
			}
			return array, nil
		default:
			return nil, fmt.Errorf("unexpected JSON delimiter %q", t)
		}
	case string, bool, nil, json.Number:
		return t, nil
	default:
		return nil, fmt.Errorf("unsupported JSON token %T", tok)
	}
}

func appendValue(out *bytes.Buffer, value any) error {
	switch v := value.(type) {
	case nil:
		out.WriteString("null")
	case bool:
		if v {
			out.WriteString("true")
		} else {
			out.WriteString("false")
		}
	case string:
		appendString(out, v)
	case json.Number:
		n, err := canonicalNumber(string(v))
		if err != nil {
			return err
		}
		out.WriteString(n)
	case float64:
		n, err := canonicalFloat(v)
		if err != nil {
			return err
		}
		out.WriteString(n)
	case map[string]any:
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		sort.Slice(keys, func(i, j int) bool {
			return utf16Less(keys[i], keys[j])
		})
		out.WriteByte('{')
		for i, key := range keys {
			if i > 0 {
				out.WriteByte(',')
			}
			appendString(out, key)
			out.WriteByte(':')
			if err := appendValue(out, v[key]); err != nil {
				return err
			}
		}
		out.WriteByte('}')
	case []any:
		out.WriteByte('[')
		for i, item := range v {
			if i > 0 {
				out.WriteByte(',')
			}
			if err := appendValue(out, item); err != nil {
				return err
			}
		}
		out.WriteByte(']')
	default:
		// Protocol code commonly starts from typed Go structs. Round-tripping
		// through encoding/json is safe because Parse re-applies I-JSON checks.
		raw, err := json.Marshal(v)
		if err != nil {
			return fmt.Errorf("marshal JCS value: %w", err)
		}
		parsed, err := Parse(raw)
		if err != nil {
			return err
		}
		return appendValue(out, parsed)
	}
	return nil
}

func appendString(out *bytes.Buffer, s string) {
	out.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"', '\\':
			out.WriteByte('\\')
			out.WriteRune(r)
		case '\b':
			out.WriteString(`\b`)
		case '\t':
			out.WriteString(`\t`)
		case '\n':
			out.WriteString(`\n`)
		case '\f':
			out.WriteString(`\f`)
		case '\r':
			out.WriteString(`\r`)
		default:
			if r < 0x20 {
				fmt.Fprintf(out, `\u%04x`, r)
			} else {
				out.WriteRune(r)
			}
		}
	}
	out.WriteByte('"')
}

func canonicalNumber(s string) (string, error) {
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return "", fmt.Errorf("number %q is outside IEEE-754 binary64", s)
	}
	return canonicalFloat(f)
}

func canonicalFloat(f float64) (string, error) {
	if math.IsInf(f, 0) || math.IsNaN(f) {
		return "", errors.New("non-finite number is not valid JCS")
	}
	if f == 0 {
		return "0", nil
	}
	abs := math.Abs(f)
	// ECMAScript uses plain decimal notation on [1e-6, 1e21). Go's 'f',
	// precision -1 retains the shortest round-trippable representation.
	if abs >= 1e-6 && abs < 1e21 {
		return strconv.FormatFloat(f, 'f', -1, 64), nil
	}
	s := strconv.FormatFloat(f, 'e', -1, 64)
	mantissa, exponent, _ := strings.Cut(s, "e")
	sign := "+"
	if strings.HasPrefix(exponent, "-") {
		sign, exponent = "-", exponent[1:]
	} else {
		exponent = strings.TrimPrefix(exponent, "+")
	}
	exponent = strings.TrimLeft(exponent, "0")
	if exponent == "" {
		exponent = "0"
	}
	return mantissa + "e" + sign + exponent, nil
}

func utf16Less(a, b string) bool {
	aa := utf16.Encode([]rune(a))
	bb := utf16.Encode([]rune(b))
	for i := 0; i < len(aa) && i < len(bb); i++ {
		if aa[i] != bb[i] {
			return aa[i] < bb[i]
		}
	}
	return len(aa) < len(bb)
}

// validateRawStrings validates escapes without allowing encoding/json to
// replace a lone surrogate with U+FFFD.
func validateRawStrings(raw []byte) error {
	for i := 0; i < len(raw); {
		if raw[i] != '"' {
			i++
			continue
		}
		i++
		for i < len(raw) {
			switch raw[i] {
			case '"':
				i++
				goto next
			case '\\':
				i++
				if i >= len(raw) {
					return errors.New("unterminated JSON escape")
				}
				switch raw[i] {
				case '"', '\\', '/', 'b', 'f', 'n', 'r', 't':
					i++
				case 'u':
					first, next, err := readHexRune(raw, i+1)
					if err != nil {
						return err
					}
					i = next
					if first >= 0xd800 && first <= 0xdbff {
						if i+2 > len(raw) || raw[i] != '\\' || raw[i+1] != 'u' {
							return errors.New("lone high surrogate in JSON string")
						}
						second, after, err := readHexRune(raw, i+2)
						if err != nil {
							return err
						}
						if second < 0xdc00 || second > 0xdfff {
							return errors.New("high surrogate is not followed by a low surrogate")
						}
						i = after
					} else if first >= 0xdc00 && first <= 0xdfff {
						return errors.New("lone low surrogate in JSON string")
					}
				default:
					return fmt.Errorf("invalid JSON escape \\%c", raw[i])
				}
			default:
				if raw[i] < 0x20 {
					return errors.New("unescaped control character in JSON string")
				}
				_, n := utf8.DecodeRune(raw[i:])
				if n == 0 {
					return errors.New("invalid UTF-8 in JSON string")
				}
				i += n
			}
		}
		return errors.New("unterminated JSON string")
	next:
	}
	return nil
}

func readHexRune(raw []byte, start int) (rune, int, error) {
	if start+4 > len(raw) {
		return 0, start, errors.New("short Unicode escape in JSON string")
	}
	n, err := strconv.ParseUint(string(raw[start:start+4]), 16, 16)
	if err != nil {
		return 0, start, errors.New("invalid Unicode escape in JSON string")
	}
	return rune(n), start + 4, nil
}
