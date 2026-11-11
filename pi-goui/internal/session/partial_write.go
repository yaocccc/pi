package session

import (
	"encoding/json"
	"strconv"
	"strings"
)

// partialWriteArguments is display-only: recover complete fields and the safe
// prefix of a string value, never manufacture arguments for tool execution.
func partialWriteArguments(data string) json.RawMessage {
	d := json.NewDecoder(strings.NewReader(data))
	token, err := d.Token()
	if err != nil || token != json.Delim('{') {
		return nil
	}
	fields := map[string]json.RawMessage{}
	for d.More() {
		key, err := d.Token()
		if err != nil {
			break
		}
		name, ok := key.(string)
		if !ok {
			break
		}
		start := int(d.InputOffset())
		var value json.RawMessage
		if d.Decode(&value) == nil {
			fields[name] = value
			continue
		}
		rest := strings.TrimSpace(data[start:])
		rest = strings.TrimSpace(strings.TrimPrefix(rest, ":"))
		if value, ok := partialString(rest); ok {
			fields[name], _ = json.Marshal(value)
		}
		break
	}
	if len(fields) == 0 {
		return nil
	}
	result, _ := json.Marshal(fields)
	return result
}

// Keep unfinished escapes (including UTF-16 surrogate pairs) until the next
// delta, rather than showing escape fragments or replacement characters.
func partialString(s string) (string, bool) {
	if len(s) == 0 || s[0] != '"' {
		return "", false
	}
	end := 1
	for i := 1; i < len(s); {
		if s[i] == '"' {
			end = i
			break
		}
		if s[i] < 0x20 {
			break
		}
		if s[i] == '\\' {
			if i+1 >= len(s) {
				break
			}
			n := 2
			if s[i+1] == 'u' {
				if i+6 > len(s) {
					break
				}
				v, err := strconv.ParseUint(s[i+2:i+6], 16, 16)
				if err != nil {
					break
				}
				n = 6
				if v >= 0xd800 && v <= 0xdbff {
					if i+12 > len(s) || s[i+6:i+8] != `\u` {
						break
					}
					low, err := strconv.ParseUint(s[i+8:i+12], 16, 16)
					if err != nil || low < 0xdc00 || low > 0xdfff {
						break
					}
					n = 12
				}
			} else if !strings.ContainsRune(`"\/bfnrt`, rune(s[i+1])) {
				break
			}
			i += n
		} else {
			i++
		}
		end = i
	}
	var value string
	err := json.Unmarshal([]byte(s[:end]+`"`), &value)
	return value, err == nil
}
