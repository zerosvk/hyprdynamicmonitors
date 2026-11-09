// Package hyprconf renders Hyprland monitor rules in either the Lua config syntax
// (Hyprland 0.55+, the only syntax supported since 0.57) or the legacy hyprlang syntax.
package hyprconf

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
)

// Format is the syntax of the Hyprland configuration that hyprdynamicmonitors writes.
type Format string

const (
	// FormatLua is the Lua config (hyprland.lua), e.g. hl.monitor({ output = "DP-1", ... }).
	FormatLua Format = "lua"
	// FormatHyprlang is the legacy hyprlang config (hyprland.conf), e.g. monitor=DP-1,...
	FormatHyprlang Format = "hyprlang"
)

// AllFormats lists the supported formats.
var AllFormats = []Format{FormatLua, FormatHyprlang}

// DefaultFormat is used when the format is neither configured nor inferable.
const DefaultFormat = FormatLua

func (f Format) Value() string {
	return string(f)
}

// ParseFormat converts a user provided string into a Format.
func ParseFormat(value string) (Format, error) {
	for _, f := range AllFormats {
		if string(f) == strings.ToLower(strings.TrimSpace(value)) {
			return f, nil
		}
	}
	return "", fmt.Errorf("unsupported hyprland config format %q, expected one of [lua, hyprlang]", value)
}

// FormatFromPath infers the format from a destination file extension.
// Returns false when the extension does not identify a format.
func FormatFromPath(path string) (Format, bool) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".lua":
		return FormatLua, true
	case ".conf":
		return FormatHyprlang, true
	default:
		return "", false
	}
}

// DefaultDestination is the default generated file for the given format.
func DefaultDestination(f Format) string {
	if f == FormatHyprlang {
		return "$HOME/.config/hypr/monitors.conf"
	}
	return "$HOME/.config/hypr/monitors.lua"
}

// CommentPrefix is the line comment prefix of the format, including a trailing space.
func CommentPrefix(f Format) string {
	if f == FormatHyprlang {
		return "# "
	}
	return "-- "
}

// Value is a single monitor rule field value.
type Value struct {
	raw    string
	quoted bool
}

// String is a string field (quoted in Lua).
func String(s string) Value {
	return Value{raw: s, quoted: true}
}

// Literal is a numeric field written verbatim in both formats (e.g. "10" or "1.20").
func Literal(s string) Value {
	return Value{raw: s, quoted: false}
}

// Int is an integer field.
func Int(i int) Value {
	return Literal(strconv.Itoa(i))
}

// Field is an optional key/value monitor rule setting, e.g. transform or vrr.
// Field keys are the same in hyprlang and Lua.
type Field struct {
	Key   string
	Value Value
}

// MonitorRule is a format agnostic Hyprland monitor rule.
type MonitorRule struct {
	// Name is the connector name (e.g. eDP-1), used when Description is empty.
	Name string
	// Description is the monitor description, matched with the desc: selector.
	Description string
	Disabled    bool
	// Mode is the resolution and refresh rate, e.g. 1920x1080@60.00000.
	Mode string
	// Position is e.g. 0x0.
	Position string
	// Scale is the scale factor; it is formatted with %.8f in hyprlang and trimmed in Lua.
	Scale float64
	// Fields are the trailing optional settings, in output order.
	Fields []Field
}

// Render renders the monitor rule as a single line in the given format.
func (r *MonitorRule) Render(f Format) string {
	if f == FormatHyprlang {
		return "monitor=" + r.HyprlangValue()
	}
	return r.Lua()
}

// HyprlangValue renders the value of a hyprlang monitor keyword (without the leading "monitor=").
func (r *MonitorRule) HyprlangValue() string {
	identifier := r.Name
	if r.Description != "" {
		// in hyprlang # starts a comment and has to be doubled
		identifier = "desc:" + strings.ReplaceAll(r.Description, "#", "##")
	}
	if r.Disabled {
		return identifier + ",disable"
	}

	fields := []string{identifier, r.Mode, r.Position, fmt.Sprintf("%.8f", r.Scale)}
	for _, field := range r.Fields {
		fields = append(fields, field.Key, field.Value.raw)
	}
	return strings.Join(fields, ",")
}

// Lua renders the rule as a hl.monitor({...}) call.
func (r *MonitorRule) Lua() string {
	output := r.Name
	if r.Description != "" {
		output = "desc:" + r.Description
	}

	parts := []string{"output = " + LuaString(output)}
	if r.Disabled {
		parts = append(parts, "disabled = true")
		return luaCall(parts)
	}

	parts = append(parts,
		"mode = "+LuaString(r.Mode),
		"position = "+LuaString(r.Position),
		"scale = "+LuaNumber(r.Scale),
	)
	for _, field := range r.Fields {
		value := field.Value.raw
		if field.Value.quoted {
			value = LuaString(value)
		}
		parts = append(parts, field.Key+" = "+value)
	}
	return luaCall(parts)
}

func luaCall(parts []string) string {
	return "hl.monitor({ " + strings.Join(parts, ", ") + " })"
}

// LuaNumber formats a float as a Lua number literal with up to 8 decimal places,
// without trailing zeros (1.25000000 -> 1.25, 1.00000000 -> 1).
func LuaNumber(v float64) string {
	s := strconv.FormatFloat(v, 'f', 8, 64)
	s = strings.TrimRight(s, "0")
	s = strings.TrimSuffix(s, ".")
	if s == "-0" || s == "" {
		return "0"
	}
	return s
}

// LuaString quotes s as a Lua string literal, escaping everything that could end the
// literal or be misinterpreted (quotes, backslashes, control characters).
func LuaString(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 2)
	b.WriteByte('"')
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if c < 0x20 || c == 0x7f {
				// decimal escape, zero padded so a following digit is not absorbed
				fmt.Fprintf(&b, `\%03d`, c)
			} else {
				b.WriteByte(c)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}
