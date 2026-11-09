package hyprconf_test

import (
	"testing"

	"github.com/fiffeek/hyprdynamicmonitors/internal/hyprconf"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseFormat(t *testing.T) {
	f, err := hyprconf.ParseFormat("lua")
	require.NoError(t, err)
	assert.Equal(t, hyprconf.FormatLua, f)

	f, err = hyprconf.ParseFormat(" Hyprlang ")
	require.NoError(t, err)
	assert.Equal(t, hyprconf.FormatHyprlang, f)

	_, err = hyprconf.ParseFormat("conf")
	assert.Error(t, err)
}

func TestFormatFromPath(t *testing.T) {
	f, ok := hyprconf.FormatFromPath("/home/u/.config/hypr/monitors.lua")
	assert.True(t, ok)
	assert.Equal(t, hyprconf.FormatLua, f)

	f, ok = hyprconf.FormatFromPath("/home/u/.config/hypr/monitors.conf")
	assert.True(t, ok)
	assert.Equal(t, hyprconf.FormatHyprlang, f)

	_, ok = hyprconf.FormatFromPath("/home/u/.config/hypr/monitors")
	assert.False(t, ok)
}

func TestMonitorRule_Render(t *testing.T) {
	tests := []struct {
		name     string
		rule     hyprconf.MonitorRule
		hyprlang string
		lua      string
	}{
		{
			name:     "disabled by name",
			rule:     hyprconf.MonitorRule{Name: "eDP-1", Disabled: true},
			hyprlang: "monitor=eDP-1,disable",
			lua:      `hl.monitor({ output = "eDP-1", disabled = true })`,
		},
		{
			name: "description wins over the name and is escaped per format",
			rule: hyprconf.MonitorRule{
				Name: "DP-1", Description: `LG #1 "QHD"`, Mode: "2560x1440@144.00000",
				Position: "-2560x0", Scale: 1.0666666666,
				Fields: []hyprconf.Field{
					{Key: "transform", Value: hyprconf.Int(1)},
					{Key: "vrr", Value: hyprconf.Int(0)},
					{Key: "bitdepth", Value: hyprconf.Literal("10")},
					{Key: "cm", Value: hyprconf.String("hdr")},
					{Key: "sdrbrightness", Value: hyprconf.Literal("1.20")},
					{Key: "mirror", Value: hyprconf.String("eDP-1")},
				},
			},
			hyprlang: `monitor=desc:LG ##1 "QHD",2560x1440@144.00000,-2560x0,1.06666667,transform,1,vrr,0,bitdepth,10,cm,hdr,sdrbrightness,1.20,mirror,eDP-1`,
			lua: `hl.monitor({ output = "desc:LG #1 \"QHD\"", mode = "2560x1440@144.00000", position = "-2560x0", ` +
				`scale = 1.06666667, transform = 1, vrr = 0, bitdepth = 10, cm = "hdr", sdrbrightness = 1.20, mirror = "eDP-1" })`,
		},
		{
			name:     "integer scale",
			rule:     hyprconf.MonitorRule{Name: "HDMI-A-1", Mode: "1920x1080@60.00000", Position: "0x0", Scale: 2},
			hyprlang: "monitor=HDMI-A-1,1920x1080@60.00000,0x0,2.00000000",
			lua:      `hl.monitor({ output = "HDMI-A-1", mode = "1920x1080@60.00000", position = "0x0", scale = 2 })`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.hyprlang, tt.rule.Render(hyprconf.FormatHyprlang))
			assert.Equal(t, tt.lua, tt.rule.Render(hyprconf.FormatLua))
		})
	}
}

func TestLuaNumber(t *testing.T) {
	assert.Equal(t, "1", hyprconf.LuaNumber(1))
	assert.Equal(t, "1.25", hyprconf.LuaNumber(1.25))
	assert.Equal(t, "1.06666667", hyprconf.LuaNumber(1.0666666666))
	assert.Equal(t, "0", hyprconf.LuaNumber(-0.000000001))
	assert.Equal(t, "-1.5", hyprconf.LuaNumber(-1.5))
}

func TestLuaString(t *testing.T) {
	assert.Equal(t, `"plain"`, hyprconf.LuaString("plain"))
	assert.Equal(t, `"a\"b\\c"`, hyprconf.LuaString(`a"b\c`))
	assert.Equal(t, `"l1\nl2\r\t"`, hyprconf.LuaString("l1\nl2\r\t"))
	assert.Equal(t, `"x\0011"`, hyprconf.LuaString("x\x011"))
	assert.Equal(t, `"ünï [[ ]] '"`, hyprconf.LuaString("ünï [[ ]] '"))
}

func TestCommentPrefixAndDefaults(t *testing.T) {
	assert.Equal(t, "-- ", hyprconf.CommentPrefix(hyprconf.FormatLua))
	assert.Equal(t, "# ", hyprconf.CommentPrefix(hyprconf.FormatHyprlang))
	assert.Equal(t, "$HOME/.config/hypr/monitors.lua",
		hyprconf.DefaultDestination(hyprconf.FormatLua))
	assert.Equal(t, "$HOME/.config/hypr/monitors.conf",
		hyprconf.DefaultDestination(hyprconf.FormatHyprlang))
}

func TestEnableDisabledLuaMonitors(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		out     string
		changed int
	}{
		{
			name:    "single line",
			in:      `hl.monitor({ output = "DP-1", disabled = true })`,
			out:     `hl.monitor({ output = "DP-1", disabled = false })`,
			changed: 1,
		},
		{
			name:    "table call syntax and no spaces",
			in:      `hl.monitor{output="DP-1",disabled=true}`,
			out:     `hl.monitor{output="DP-1",disabled=false}`,
			changed: 1,
		},
		{
			name:    "multi line with nested table",
			in:      "hl.monitor({\n  output = \"DP-1\",\n  reserved_area = { top = 10 },\n  disabled   =   true,\n})\n",
			out:     "hl.monitor({\n  output = \"DP-1\",\n  reserved_area = { top = 10 },\n  disabled   =   false,\n})\n",
			changed: 1,
		},
		{
			name:    "strings and comments are untouched",
			in:      "-- hl.monitor({ output = \"x\", disabled = true })\nhl.monitor({ output = \"disabled = true\" }) -- disabled = true\n",
			out:     "-- hl.monitor({ output = \"x\", disabled = true })\nhl.monitor({ output = \"disabled = true\" }) -- disabled = true\n",
			changed: 0,
		},
		{
			name:    "long comments and strings are untouched",
			in:      "--[[ hl.monitor({ output = \"x\", disabled = true }) ]]\nlocal s = [==[ hl.monitor({ disabled = true }) ]==]\n",
			out:     "--[[ hl.monitor({ output = \"x\", disabled = true }) ]]\nlocal s = [==[ hl.monitor({ disabled = true }) ]==]\n",
			changed: 0,
		},
		{
			name:    "other calls and lookalike keys are untouched",
			in:      "hl.config({ misc = { disabled = true } })\nmyhl.monitor({ disabled = true })\nhl.monitor({ output = \"a\", not_disabled = true, disabled = trueish })\n",
			out:     "hl.config({ misc = { disabled = true } })\nmyhl.monitor({ disabled = true })\nhl.monitor({ output = \"a\", not_disabled = true, disabled = trueish })\n",
			changed: 0,
		},
		{
			name:    "unbalanced call leaves the rest of the file untouched",
			in:      "hl.monitor({ output = \"a\", disabled = true })\nhl.monitor({ output = \"b\", disabled = true }\n",
			out:     "hl.monitor({ output = \"a\", disabled = false })\nhl.monitor({ output = \"b\", disabled = true }\n",
			changed: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, changed := hyprconf.EnableDisabledLuaMonitors(tt.in)
			assert.Equal(t, tt.out, out)
			assert.Equal(t, tt.changed, changed)
		})
	}
}
