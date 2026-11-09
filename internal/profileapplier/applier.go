// Package profileapplier writes a matched profile to the destination and runs the user callbacks
// and notification around it; shared by the daemon and the TUI so both behave the same
package profileapplier

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"sync"

	"github.com/fiffeek/hyprdynamicmonitors/internal/config"
	"github.com/fiffeek/hyprdynamicmonitors/internal/generators"
	"github.com/fiffeek/hyprdynamicmonitors/internal/hypr"
	"github.com/fiffeek/hyprdynamicmonitors/internal/matchers"
	"github.com/fiffeek/hyprdynamicmonitors/internal/notifications"
	"github.com/fiffeek/hyprdynamicmonitors/internal/power"
	"github.com/fiffeek/hyprdynamicmonitors/internal/utils"
	"github.com/sirupsen/logrus"
)

type Applier struct {
	generator     *generators.ConfigGenerator
	notifications *notifications.Service
	dryRun        bool
	// mu serializes applications, they write the same destination and run user callbacks
	mu sync.Mutex
}

func New(generator *generators.ConfigGenerator, notifications *notifications.Service, dryRun bool) *Applier {
	return &Applier{generator: generator, notifications: notifications, dryRun: dryRun}
}

// Result describes what Apply did
type Result struct {
	// Changed is true when the destination changed (or would change, in dry run)
	Changed bool
	// CallbackErr holds pre/post apply callback failures; they do not stop the apply,
	// the destination is written regardless, same as before
	CallbackErr error
}

// Apply renders or links the matched profile into the destination. Only when the destination changes
// (or would change, in dry run) it runs pre_apply_exec before the write, post_apply_exec after it, and
// sends the "profile applied" notification. Notification failures are logged and swallowed.
func (a *Applier) Apply(ctx context.Context, cfg *config.RawConfig, matchedProfile *matchers.MatchedProfile,
	monitors []*hypr.MonitorSpec, powerState power.PowerState, lidState power.LidState,
) (Result, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	profile := matchedProfile.Profile
	profileFields := logrus.Fields{
		"profile_name": profile.Name,
		"config_file":  profile.ConfigFile,
		"config_type":  profile.ConfigType.Value(),
	}

	// pre apply callbacks only run when the destination is about to change,
	// in dry run when it would change
	willChange := false
	var preErr error
	runPreExec := func() {
		willChange = true
		preErr = a.tryExec(ctx, profile.PreApplyExec, cfg.General.PreApplyExec, utils.PreExecLogID)
	}

	destination := *cfg.General.Destination
	changed, err := a.generator.GenerateConfigWithHook(cfg, matchedProfile, monitors, powerState,
		lidState, destination, a.dryRun, runPreExec)
	if err != nil {
		return Result{CallbackErr: preErr}, fmt.Errorf("failed to generate config: %w", err)
	}
	if a.dryRun {
		changed = willChange
	}

	if !changed {
		logrus.Info("Not sending notifications since the config has not been changed")
		return Result{}, nil
	}

	postErr := a.tryExec(ctx, profile.PostApplyExec, cfg.General.PostApplyExec, utils.PostExecLogID)

	if err := a.notifications.NotifyProfileApplied(profile, a.dryRun); err != nil {
		logrus.WithFields(profileFields).WithError(err).Error("swallowing notification error")
	}

	return Result{Changed: true, CallbackErr: errors.Join(preErr, postErr)}, nil
}

// tryExec runs the user callback; a failure is logged and returned but never stops the apply
func (a *Applier) tryExec(ctx context.Context, command, fallbackCommand *string, logID utils.LogID) error {
	// fallback on a default command when it's not provided for a profile
	if command == nil || *command == "" {
		command = fallbackCommand
	}
	// if it's still empty then nothing to be done
	if command == nil || *command == "" {
		return nil
	}
	// if running with dry run then just output the commands
	if a.dryRun {
		logrus.WithFields(utils.NewLogrusCustomFields(map[string]interface{}{
			"command": *command,
			"order":   logID,
		}).WithLogID(utils.DryRunExedLogID)).
			Info("[DRY RUN] Would run command")
		return nil
	}

	logrus.WithFields(
		utils.NewLogrusCustomFields(logrus.Fields{"command": *command}).WithLogID(logID)).Info("Executing user callback")
	// nolint:gosec
	out, err := exec.CommandContext(ctx, "bash", "-c", *command).CombinedOutput()
	if err != nil {
		logrus.Errorf("error %v while executing %s output %s, continuing as normal", err, *command, string(out))
		return fmt.Errorf("callback %q failed: %w", *command, err)
	}
	return nil
}
