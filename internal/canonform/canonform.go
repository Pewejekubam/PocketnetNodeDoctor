// Package canonform produces canonical-form JSON bytes used by the doctor
// for trust-root verification and plan self-hashing: sorted object keys, no
// insignificant whitespace, UTF-8 bytes, no trailing newline. Consumers
// SHA-256 the exact bytes.
package canonform

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
)

// Marshal returns the canonical-form JSON encoding of v. v must be a value
// produced by encoding/json.Unmarshal into any (i.e., one of: nil, bool,
// float64, string, []any, map[string]any) or any value the standard library
// can JSON-marshal whose object keys are strings.
func Marshal(v any) ([]byte, error) {
	var buf bytes.Buffer
	if err := encode(&buf, v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func encode(buf *bytes.Buffer, v any) error {
	switch x := v.(type) {
	case nil:
		buf.WriteString("null")
		return nil
	case bool:
		if x {
			buf.WriteString("true")
		} else {
			buf.WriteString("false")
		}
		return nil
	case string:
		return encodeString(buf, x)
	case json.Number:
		buf.WriteString(string(x))
		return nil
	case map[string]any:
		return encodeMap(buf, x)
	case []any:
		return encodeArray(buf, x)
	}

	// Fallback for typed values (numbers, structs, typed maps/slices).
	// Re-marshal via encoding/json into a generic representation, then
	// encode canonically. We use json.Number to avoid float precision
	// surprises on integer-shaped values.
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var generic any
	if err := dec.Decode(&generic); err != nil {
		return err
	}
	return encode(buf, generic)
}

func encodeMap(buf *bytes.Buffer, m map[string]any) error {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	buf.WriteByte('{')
	for i, k := range keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		if err := encodeString(buf, k); err != nil {
			return err
		}
		buf.WriteByte(':')
		if err := encode(buf, m[k]); err != nil {
			return err
		}
	}
	buf.WriteByte('}')
	return nil
}

func encodeArray(buf *bytes.Buffer, a []any) error {
	buf.WriteByte('[')
	for i, v := range a {
		if i > 0 {
			buf.WriteByte(',')
		}
		if err := encode(buf, v); err != nil {
			return err
		}
	}
	buf.WriteByte(']')
	return nil
}

func encodeString(buf *bytes.Buffer, s string) error {
	// stdlib json.Marshal of a string handles escaping per RFC 8259 and
	// preserves UTF-8 (HTMLEscape disabled so we don't get < etc.).
	enc := json.NewEncoder(&bytes.Buffer{})
	enc.SetEscapeHTML(false)
	tmp := &bytes.Buffer{}
	enc2 := json.NewEncoder(tmp)
	enc2.SetEscapeHTML(false)
	if err := enc2.Encode(s); err != nil {
		return fmt.Errorf("encode string: %w", err)
	}
	out := tmp.Bytes()
	// json.Encoder appends a trailing newline; strip it.
	if n := len(out); n > 0 && out[n-1] == '\n' {
		out = out[:n-1]
	}
	buf.Write(out)
	return nil
}
