// Package tooldata decodes generic saved tool metadata without rendering it.
package tooldata

import (
	"bytes"
	"encoding/json"
	"io"
	"strconv"
	"strings"
)

func CopyMap(m map[string]any) map[string]any {
	copy := make(map[string]any, len(m))
	for k, value := range m {
		copy[k] = value
	}
	return copy
}

func DecodeJSON(data []byte) (any, bool) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, true
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	var v any
	if d.Decode(&v) != nil {
		return nil, false
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return nil, false
	}
	return v, true
}
func DecodeMap(raw json.RawMessage) map[string]any {
	v, ok := DecodeJSON(raw)
	if !ok {
		return nil
	}
	m, _ := v.(map[string]any)
	return m
}
func StringField(m map[string]any, keys ...string) string {
	for _, key := range keys {
		if s, ok := m[key].(string); ok && s != "" {
			return s
		}
	}
	return ""
}
func IntField(m map[string]any, key string, fallback int) int {
	v := m[key]
	switch n := v.(type) {
	case json.Number:
		i, e := strconv.Atoi(string(n))
		if e == nil && i > 0 && i < 1<<30 {
			return i
		}
	case float64:
		if n > 0 && n < 1<<30 {
			return int(n)
		}
	}
	return fallback
}
func ToolName(name string) string {
	name = strings.ToLower(name)
	if i := strings.LastIndex(name, "."); i >= 0 {
		name = name[i+1:]
	}
	return name
}
