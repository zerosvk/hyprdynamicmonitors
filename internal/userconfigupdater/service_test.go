package userconfigupdater_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/fiffeek/hyprdynamicmonitors/internal/config"
	"github.com/fiffeek/hyprdynamicmonitors/internal/generators"
	"github.com/fiffeek/hyprdynamicmonitors/internal/hypr"
	"github.com/fiffeek/hyprdynamicmonitors/internal/matchers"
	"github.com/fiffeek/hyprdynamicmonitors/internal/notifications"
	"github.com/fiffeek/hyprdynamicmonitors/internal/power"
	"github.com/fiffeek/hyprdynamicmonitors/internal/testutils"
	"github.com/fiffeek/hyprdynamicmonitors/internal/userconfigupdater"
	"github.com/fiffeek/hyprdynamicmonitors/internal/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeMonitors struct{ monitors hypr.MonitorSpecs }

func (f *fakeMonitors) Listen() <-chan hypr.MonitorSpecs        { return nil }
func (f *fakeMonitors) GetConnectedMonitors() hypr.MonitorSpecs { return f.monitors }

type fakePower struct{}

func (f *fakePower) GetCurrentState() power.PowerState { return power.ACPowerState }
func (f *fakePower) Listen() <-chan power.PowerEvent   { return nil }

type fakeLid struct{}

func (f *fakeLid) GetCurrentState() power.LidState { return power.OpenedLidState }
func (f *fakeLid) Listen() <-chan power.LidEvent   { return nil }

func newService(t *testing.T, cfg *config.Config) *userconfigupdater.Service {
	generator, err := generators.NewConfigGenerator(cfg)
	require.NoError(t, err)
	monitors := &fakeMonitors{monitors: hypr.MonitorSpecs{
		{Name: "eDP-1", ID: utils.IntPtr(0), Description: "Built-in Display"},
	}}
	return userconfigupdater.NewService(cfg, monitors, &fakePower{}, &userconfigupdater.Config{},
		matchers.NewMatcher(), generator, notifications.NewService(cfg), &fakeLid{})
}

func countLines(t *testing.T, path string) int {
	//nolint:gosec
	contents, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return 0
	}
	require.NoError(t, err)
	return strings.Count(string(contents), "\n")
}

func TestService_CallbacksOnlyRunOnChange(t *testing.T) {
	for _, configType := range []config.ConfigFileType{config.Static, config.Template} {
		t.Run(configType.Value(), func(t *testing.T) {
			tmpDir := t.TempDir()
			preLog := filepath.Join(tmpDir, "pre.log")
			postLog := filepath.Join(tmpDir, "post.log")
			cfg := testutils.NewTestConfig(t).
				WithNotifications(&config.Notifications{Disabled: utils.BoolPtr(true)}).
				WithProfiles(map[string]*config.Profile{
					"laptop": {
						ConfigType: utils.JustPtr(configType),
						Conditions: &config.ProfileCondition{
							RequiredMonitors: []*config.RequiredMonitor{{Name: utils.StringPtr("eDP-1")}},
						},
					},
				}).
				WithPreExec("echo pre >> " + preLog).
				WithPostExec("echo post >> " + postLog).
				Get()
			svc := newService(t, cfg)
			ctx := context.Background()

			require.NoError(t, svc.RunOnce(ctx))
			assert.Equal(t, 1, countLines(t, preLog), "pre callback should run when the config is applied")
			assert.Equal(t, 1, countLines(t, postLog), "post callback should run when the config is applied")

			// nothing changed, e.g. an unrelated event or SIGUSR1
			require.NoError(t, svc.UpdateOnce(ctx))
			require.NoError(t, svc.UpdateOnce(ctx))
			assert.Equal(t, 1, countLines(t, preLog), "pre callback should not run when nothing changes")
			assert.Equal(t, 1, countLines(t, postLog), "post callback should not run when nothing changes")
		})
	}
}

func TestService_UpdateOnceIsSerialized(t *testing.T) {
	cfg := testutils.NewTestConfig(t).
		WithNotifications(&config.Notifications{Disabled: utils.BoolPtr(true)}).
		WithProfiles(map[string]*config.Profile{
			"laptop": {
				ConfigType: utils.JustPtr(config.Static),
				Conditions: &config.ProfileCondition{
					RequiredMonitors: []*config.RequiredMonitor{{Name: utils.StringPtr("eDP-1")}},
				},
			},
		}).Get()
	svc := newService(t, cfg)
	require.NoError(t, svc.RunOnce(context.Background()))

	// concurrent applications used to race on removing and re-creating the symlink
	require.NoError(t, os.Remove(*cfg.Get().General.Destination))
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- svc.UpdateOnce(context.Background())
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		assert.NoError(t, err)
	}
	target, err := os.Readlink(*cfg.Get().General.Destination)
	require.NoError(t, err)
	assert.Equal(t, cfg.Get().Profiles["laptop"].ConfigFile, target)
}
