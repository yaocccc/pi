package render

import (
	"net/url"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
)

// messageTarget is a half-open run of terminal cells on one transcript row.
// Targets are inert data; only the UI can resolve files or launch a program.
type messageTarget struct {
	row, start, end int
	target          string
}

// MessageTargetAt returns the destination under a document-relative terminal cell.
func (d Document) MessageTargetAt(row, cell int) (string, bool) {
	i := sort.Search(len(d.messageTargets), func(i int) bool {
		s := d.messageTargets[i]
		return s.row > row || (s.row == row && s.end > cell)
	})
	if i < len(d.messageTargets) {
		s := d.messageTargets[i]
		if s.row == row && s.start <= cell {
			return s.target, true
		}
	}
	return "", false
}

func appendMessageTargets(dst, src []messageTarget, row, cell int) []messageTarget {
	for _, s := range src {
		s.row += row
		s.start += cell
		s.end += cell
		dst = append(dst, s)
	}
	return dst
}

func tokenSeparator(r rune) bool {
	return unicode.IsSpace(r) || strings.ContainsRune("`\"'<>[](){}*,;|，。；：（）【】《》「」、“”‘’", r)
}

func plainTarget(token string) string {
	token = strings.TrimRight(token, ".:!?")
	if strings.Contains(token, "://") {
		if u, err := url.Parse(token); err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Hostname() != "" {
			return token
		}
		return ""
	}
	if len(token) > 1 && (strings.ContainsAny(token, "/.\\") || token == "Makefile" || token == "Dockerfile" || token == "LICENSE" || token == "README") {
		return token
	}
	return ""
}

func markdownDestination(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || raw == "" {
		return ""
	}
	if u.Scheme == "http" || u.Scheme == "https" {
		if u.Hostname() != "" {
			return raw
		}
	} else if u.Scheme == "" && u.Host == "" && u.Path != "" && strings.IndexFunc(u.Path, unicode.IsControl) < 0 {
		return u.Path
	}
	return ""
}

// Inspect renderer-generated OSC 8 before framing and consume it (don't let
// the terminal open arbitrary schemes). For ordinary paths, require an exact
// source token to avoid manufacturing destinations by joining unrelated lines.
func messageRows(source string, rows []string) ([]string, []messageTarget) {
	candidates := map[string]bool{}
	lengths := map[int]bool{}
	for token := range strings.FieldsFuncSeq(Clean(source), tokenSeparator) {
		if s := plainTarget(token); s != "" {
			candidates[s] = true
			lengths[len(s)] = true
		}
	}
	var targets, parts []messageTarget
	var word strings.Builder
	flush := func() {
		target := strings.TrimRight(word.String(), ".:!?")
		if candidates[target] {
			// Only ASCII punctuation was trimmed. It may occupy a separate row.
			trim := word.Len() - len(target)
			for i := len(parts) - 1; i >= 0 && trim > 0; i-- {
				n := min(trim, parts[i].end-parts[i].start)
				parts[i].end -= n
				trim -= n
			}
			for _, p := range parts {
				if p.start < p.end {
					p.target = target
					targets = append(targets, p)
				}
			}
		}
		parts = parts[:0]
		word.Reset()
	}
	var cleaned []string
	var link string
	var inLink bool
	for y, row := range rows {
		hasOSC := strings.Contains(row, "\x1b]8;")
		if !hasOSC && !inLink && len(candidates) == 0 {
			continue
		}
		var out strings.Builder
		var state byte
		x := 0
		for rest := row; len(rest) > 0; {
			s, cells, n, next := ansi.DecodeSequence(rest, state, nil)
			rest, state = rest[n:], next
			if params, ok := strings.CutPrefix(s, "\x1b]8;"); ok {
				flush()
				_, raw, _ := strings.Cut(params, ";")
				raw = strings.TrimSuffix(strings.TrimSuffix(raw, "\a"), "\x1b\\")
				inLink, link = raw != "", markdownDestination(raw)
				continue
			}
			if hasOSC {
				out.WriteString(s)
			}
			if cells == 0 {
				continue
			}
			if inLink {
				if link != "" {
					i := len(targets) - 1
					if i >= 0 && targets[i].target == link && targets[i].row == y && targets[i].end == x {
						targets[i].end += cells
					} else {
						targets = append(targets, messageTarget{y, x, x + cells, link})
					}
				}
			} else if len(candidates) > 0 {
				r, _ := utf8.DecodeRuneInString(s)
				if tokenSeparator(r) {
					flush()
				} else {
					word.WriteString(s)
					i := len(parts) - 1
					if i >= 0 && parts[i].row == y {
						parts[i].end += cells
					} else {
						parts = append(parts, messageTarget{row: y, start: x, end: x + cells})
					}
				}
			}
			x += cells
		}
		if hasOSC {
			if cleaned == nil {
				cleaned = append([]string(nil), rows...)
			}
			cleaned[y] = out.String()
		}
		// Avoid rehashing every prefix of a long wrapped path.
		if x == 0 || (lengths[word.Len()] && candidates[word.String()]) {
			flush()
		}
	}
	flush()
	if cleaned != nil {
		rows = cleaned
	}
	return rows, targets
}
