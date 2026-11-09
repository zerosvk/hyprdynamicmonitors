package hypr_test

import (
	"context"
	"testing"
	"time"

	"github.com/fiffeek/hyprdynamicmonitors/internal/hypr"
	"github.com/fiffeek/hyprdynamicmonitors/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEvalLua(t *testing.T) {
	code := `hl.monitor({ output = "desc:A/B \"x\"", disabled = true }); hl.monitor({ output = "DP-1", mode = "preferred" })`
	tests := []struct {
		name     string
		response string
		wantErr  string
	}{
		{name: "ok", response: "ok"},
		{name: "ok with trailing newline", response: "ok\n"},
		{name: "lua config manager not active", response: "eval is only supported with the lua config manager", wantErr: "eval is only supported"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()

			xdg, signature := testutils.SetupHyprEnvVars(t)
			listener, teardown := testutils.SetupHyprSocket(ctx, t, xdg, signature, hypr.GetHyprSocket)
			defer teardown()
			done := testutils.SetupFakeHyprIPCWriter(t, listener, [][]byte{[]byte(tt.response)},
				[]string{"/eval " + code}, true)

			err := hypr.EvalLua(ctx, code)
			<-done
			if tt.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
				return
			}
			assert.NoError(t, err)
		})
	}
}

func TestEvalLua_RejectsMultiline(t *testing.T) {
	err := hypr.EvalLua(context.Background(), "hl.monitor({ output = \"a\" })\nos.exit()")
	assert.Error(t, err)
}
