package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fiffeek/hyprdynamicmonitors/internal/config"
	"github.com/fiffeek/hyprdynamicmonitors/internal/generators"
	"github.com/fiffeek/hyprdynamicmonitors/internal/hypr"
	"github.com/fiffeek/hyprdynamicmonitors/internal/matchers"
	"github.com/fiffeek/hyprdynamicmonitors/internal/power"
	"github.com/fiffeek/hyprdynamicmonitors/internal/testutils"
	"github.com/fiffeek/hyprdynamicmonitors/internal/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func countLines(t *testing.T, path string) int {
	//nolint:gosec
	contents, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return 0
	}
	require.NoError(t, err)
	return strings.Count(string(contents), "\n")
}

func TestHyprApply_GenerateThroughHDM_RunsCallbacks(t *testing.T) {
	tmpDir := t.TempDir()
	preLog := filepath.Join(tmpDir, "pre.log")
	postLog := filepath.Join(tmpDir, "post.log")
	cfg := testutils.NewTestConfig(t).
		WithNotifications(&config.Notifications{Disabled: utils.BoolPtr(true)}).
		WithProfiles(map[string]*config.Profile{
			"laptop": {
				ConfigType: utils.JustPtr(config.Static),
				Conditions: &config.ProfileCondition{
					RequiredMonitors: []*config.RequiredMonitor{{Name: utils.StringPtr("eDP-1")}},
				},
			},
		}).
		WithPreExec("echo pre >> " + preLog).
		WithPostExec("echo post >> " + postLog).
		Get()

	generator, err := generators.NewConfigGenerator(cfg)
	require.NoError(t, err)
	apply := NewHyprApply(nil, generator)

	monitors := []*MonitorSpec{NewMonitorSpec(&hypr.MonitorSpec{
		Name: "eDP-1", ID: utils.IntPtr(0), Description: "Built-in Display",
		Width: 1920, Height: 1080, RefreshRate: 60, Scale: 1, Mirror: "none",
	})}
	profile := matchers.NewMatchedProfile(cfg.Get().Profiles["laptop"], map[int]*config.RequiredMonitor{})

	run := func() {
		msg := apply.GenerateThroughHDM(cfg, profile, monitors, power.ACPowerState, power.OpenedLidState)()
		status, ok := msg.(OperationStatus)
		require.True(t, ok)
		require.NoError(t, status.err)
	}

	run()
	assert.Equal(t, 1, countLines(t, preLog), "pre callback should run when the TUI applies a profile")
	assert.Equal(t, 1, countLines(t, postLog), "post callback should run when the TUI applies a profile")

	// applying the same profile again changes nothing, like in the daemon
	run()
	assert.Equal(t, 1, countLines(t, preLog))
	assert.Equal(t, 1, countLines(t, postLog))
}

func TestHyprApply_GenerateThroughHDM_ReportsCallbackFailure(t *testing.T) {
	cfg := testutils.NewTestConfig(t).
		WithNotifications(&config.Notifications{Disabled: utils.BoolPtr(true)}).
		WithProfiles(map[string]*config.Profile{
			"laptop": {
				ConfigType: utils.JustPtr(config.Static),
				Conditions: &config.ProfileCondition{
					RequiredMonitors: []*config.RequiredMonitor{{Name: utils.StringPtr("eDP-1")}},
				},
			},
		}).
		WithPostExec("exit 3").
		Get()

	generator, err := generators.NewConfigGenerator(cfg)
	require.NoError(t, err)
	apply := NewHyprApply(nil, generator)

	monitors := []*MonitorSpec{NewMonitorSpec(&hypr.MonitorSpec{
		Name: "eDP-1", ID: utils.IntPtr(0), Description: "Built-in Display",
		Width: 1920, Height: 1080, RefreshRate: 60, Scale: 1, Mirror: "none",
	})}
	profile := matchers.NewMatchedProfile(cfg.Get().Profiles["laptop"], map[int]*config.RequiredMonitor{})

	msg := apply.GenerateThroughHDM(cfg, profile, monitors, power.ACPowerState, power.OpenedLidState)()
	status, ok := msg.(OperationStatus)
	require.True(t, ok)
	require.Error(t, status.err, "a failing callback should be shown in the TUI")
	assert.Contains(t, status.err.Error(), "config written but")
	assert.Contains(t, status.err.Error(), "exit 3")

	// the destination is written regardless, same as in the daemon
	_, err = os.Lstat(*cfg.Get().General.Destination)
	require.NoError(t, err)
}
