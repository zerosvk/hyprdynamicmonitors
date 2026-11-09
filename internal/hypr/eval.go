package hypr

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/fiffeek/hyprdynamicmonitors/internal/dial"
	"github.com/fiffeek/hyprdynamicmonitors/internal/utils"
	"github.com/sirupsen/logrus"
)

// EvalLua runs Lua code inside Hyprland's Lua config state, the same as `hyprctl eval '<code>'`.
// It requires Hyprland 0.55+ running with a Lua config (hyprland.lua). The code has to be a single line.
// Rules added this way (e.g. hl.monitor) last until the next config reload.
func EvalLua(ctx context.Context, code string) error {
	if strings.ContainsAny(code, "\r\n") {
		return errors.New("lua code passed to eval has to be a single line")
	}

	signature := os.Getenv("HYPRLAND_INSTANCE_SIGNATURE")
	if signature == "" {
		return errors.New("HYPRLAND_INSTANCE_SIGNATURE environment variable not set - are you running under Hyprland?")
	}
	xdgRuntimeDir, err := utils.GetXDGRuntimeDir()
	if err != nil {
		return fmt.Errorf("cant get xdg runtime dir: %w", err)
	}

	socketPath := GetHyprSocket(xdgRuntimeDir, signature)
	conn, teardown, err := dial.GetUnixSocketConnection(ctx, socketPath)
	if err != nil {
		return fmt.Errorf("cant open socket to %s: %w", socketPath, err)
	}
	defer teardown()

	// the leading "/" marks an empty flags section, exactly like hyprctl sends it,
	// so nothing in the code is interpreted as request flags
	if _, err := conn.Write([]byte("/eval " + code + "\n")); err != nil {
		return fmt.Errorf("failed to send eval request: %w", err)
	}

	response, err := io.ReadAll(conn)
	if err != nil {
		return fmt.Errorf("failed to read eval response: %w", err)
	}

	result := strings.TrimSpace(string(response))
	logrus.WithFields(logrus.Fields{"code": code, "response": result}).Debug("hyprland eval")
	if result != "ok" {
		return fmt.Errorf("hyprland rejected the lua code: %s", result)
	}
	return nil
}
