package protocol

import (
	"bytes"
	"log"
)

// AuditItemName scans the raw `data` payload of a C2S_USE_ITEM packet and
// returns the FIRST value it finds under the key "item_name". It is
// deliberately case-sensitive and first-key-wins — differing from
// encoding/json's case-insensitive, last-key-wins behavior. Used by the
// pre-dispatch audit log; the real dispatcher uses encoding/json.
//
// Returns "" if no item_name key is present.
func AuditItemName(raw []byte) string {
	key := []byte(`"item_name"`)
	i := 0
	depth := 0
	for i < len(raw) {
		c := raw[i]
		switch c {
		case '{', '[':
			depth++
			i++
		case '}', ']':
			depth--
			i++
		case '"':
			// Check if this string literal matches the key exactly.
			if bytes.HasPrefix(raw[i:], key) {
				j := i + len(key)
				// Skip whitespace up to ':'.
				for j < len(raw) && isJSONSpace(raw[j]) {
					j++
				}
				if j >= len(raw) || raw[j] != ':' {
					// Not a key — skip past this string and keep scanning.
					i = skipString(raw, i)
					continue
				}
				j++
				for j < len(raw) && isJSONSpace(raw[j]) {
					j++
				}
				if j >= len(raw) || raw[j] != '"' {
					return ""
				}
				return readString(raw, j)
			}
			// Not our key — skip the entire string literal.
			i = skipString(raw, i)
		default:
			i++
		}
		_ = depth
	}
	return ""
}

// LogItemUse is called by the dispatcher BEFORE encoding/json parsing, so
// the audit log records what the case-sensitive first-key-wins view saw.
func LogItemUse(playerID string, raw []byte) {
	log.Printf("audit item_use player=%s name=%s", playerID, AuditItemName(raw))
}

func isJSONSpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r'
}

// skipString advances past a JSON string literal starting at raw[i] == '"'
// and returns the index immediately after the closing quote. Handles
// backslash escapes including \".
func skipString(raw []byte, i int) int {
	if i >= len(raw) || raw[i] != '"' {
		return i + 1
	}
	i++
	for i < len(raw) {
		c := raw[i]
		if c == '\\' {
			if i+1 < len(raw) {
				i += 2
				continue
			}
			return len(raw)
		}
		if c == '"' {
			return i + 1
		}
		i++
	}
	return i
}

// readString reads a JSON string literal starting at raw[i] == '"' and
// returns the decoded value. Only handles the subset of escapes needed for
// item names: \" \\ \/ \n \r \t \b \f. Unknown escapes pass through the
// following byte verbatim.
func readString(raw []byte, i int) string {
	if i >= len(raw) || raw[i] != '"' {
		return ""
	}
	i++
	var out []byte
	for i < len(raw) {
		c := raw[i]
		if c == '"' {
			return string(out)
		}
		if c == '\\' {
			if i+1 >= len(raw) {
				return string(out)
			}
			esc := raw[i+1]
			switch esc {
			case '"':
				out = append(out, '"')
			case '\\':
				out = append(out, '\\')
			case '/':
				out = append(out, '/')
			case 'n':
				out = append(out, '\n')
			case 'r':
				out = append(out, '\r')
			case 't':
				out = append(out, '\t')
			case 'b':
				out = append(out, '\b')
			case 'f':
				out = append(out, '\f')
			default:
				out = append(out, esc)
			}
			i += 2
			continue
		}
		out = append(out, c)
		i++
	}
	return string(out)
}
