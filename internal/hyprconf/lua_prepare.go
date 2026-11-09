package hyprconf

import (
	"regexp"
	"strings"
)

var (
	luaDisabledTrue = regexp.MustCompile(`\bdisabled(\s*)=(\s*)true\b`)
	luaMonitorCall  = regexp.MustCompile(`^hl\s*\.\s*monitor\s*[({]`)
)

// EnableDisabledLuaMonitors rewrites `disabled = true` into `disabled = false` inside every
// hl.monitor(...) call of a Lua config, so that no monitor stays disabled when Hyprland starts.
// Strings and comments are skipped, both single and multi line calls are handled.
// Returns the new content and the number of rules that were changed.
func EnableDisabledLuaMonitors(src string) (string, int) {
	var out strings.Builder
	out.Grow(len(src))
	changed := 0

	i := 0
	for i < len(src) {
		if next := skipLuaNonCode(src, i); next > i {
			out.WriteString(src[i:next])
			i = next
			continue
		}

		var loc []int
		if src[i] == 'h' && (i == 0 || !isLuaIdentChar(src[i-1])) {
			loc = luaMonitorCall.FindStringIndex(src[i:])
		}
		if loc == nil {
			out.WriteByte(src[i])
			i++
			continue
		}

		openIdx := i + loc[1] - 1
		closeIdx := findLuaClosing(src, openIdx)
		if closeIdx == -1 {
			// unbalanced call (broken file), leave the rest untouched
			out.WriteString(src[i:])
			break
		}

		call := src[i : closeIdx+1]
		rewritten := rewriteLuaCode(call, func(code string) string {
			return luaDisabledTrue.ReplaceAllString(code, "disabled${1}=${2}false")
		})
		if rewritten != call {
			changed++
		}
		out.WriteString(rewritten)
		i = closeIdx + 1
	}

	return out.String(), changed
}

func isLuaIdentChar(c byte) bool {
	return c == '_' || c == '.' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

// rewriteLuaCode applies fn to the code parts of src only (not to strings or comments).
func rewriteLuaCode(src string, fn func(string) string) string {
	var out strings.Builder
	codeStart := 0
	i := 0
	for i < len(src) {
		next := skipLuaNonCode(src, i)
		if next == i {
			i++
			continue
		}
		out.WriteString(fn(src[codeStart:i]))
		out.WriteString(src[i:next])
		i = next
		codeStart = i
	}
	out.WriteString(fn(src[codeStart:]))
	return out.String()
}

// findLuaClosing returns the index of the bracket closing the one at openIdx, or -1.
func findLuaClosing(src string, openIdx int) int {
	depth := 0
	i := openIdx
	for i < len(src) {
		if next := skipLuaNonCode(src, i); next > i {
			i = next
			continue
		}
		switch src[i] {
		case '(', '{', '[':
			depth++
		case ')', '}', ']':
			depth--
			if depth == 0 {
				return i
			}
		}
		i++
	}
	return -1
}

// skipLuaNonCode returns the index right after the string literal or comment starting at i,
// or i when src[i:] does not start one. Unterminated literals extend to the end of src.
func skipLuaNonCode(src string, i int) int {
	if i >= len(src) {
		return i
	}
	switch {
	case strings.HasPrefix(src[i:], "--"):
		if end, ok := skipLongBracket(src, i+2); ok {
			return end
		}
		if nl := strings.IndexByte(src[i:], '\n'); nl != -1 {
			return i + nl
		}
		return len(src)
	case src[i] == '"' || src[i] == '\'':
		quote := src[i]
		j := i + 1
		for j < len(src) {
			switch src[j] {
			case '\\':
				j += 2
				continue
			case quote, '\n':
				return j + 1
			}
			j++
		}
		return len(src)
	case src[i] == '[':
		if end, ok := skipLongBracket(src, i); ok {
			return end
		}
	}
	return i
}

// skipLongBracket handles [[...]] and [==[...]==] starting at i.
func skipLongBracket(src string, i int) (int, bool) {
	if i >= len(src) || src[i] != '[' {
		return i, false
	}
	j := i + 1
	for j < len(src) && src[j] == '=' {
		j++
	}
	if j >= len(src) || src[j] != '[' {
		return i, false
	}
	closing := "]" + strings.Repeat("=", j-i-1) + "]"
	end := strings.Index(src[j+1:], closing)
	if end == -1 {
		return len(src), true
	}
	return j + 1 + end + len(closing), true
}
