package utils_test

import (
	"path/filepath"
	"testing"

	"github.com/fiffeek/hyprdynamicmonitors/internal/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExpandHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	tests := []struct {
		in   string
		want string
	}{
		{in: "~", want: home},
		{in: "~/hyprconfigs/a.conf", want: filepath.Join(home, "hyprconfigs/a.conf")},
		{in: "/abs/a.conf", want: "/abs/a.conf"},
		{in: "relative/a.conf", want: "relative/a.conf"},
		{in: "~user/a.conf", want: "~user/a.conf"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := utils.ExpandHome(tt.in)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}
