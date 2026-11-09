package tui

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/fiffeek/hyprdynamicmonitors/internal/config"
	"github.com/fiffeek/hyprdynamicmonitors/internal/generators"
	"github.com/fiffeek/hyprdynamicmonitors/internal/hypr"
	"github.com/fiffeek/hyprdynamicmonitors/internal/hyprconf"
	"github.com/fiffeek/hyprdynamicmonitors/internal/matchers"
	"github.com/fiffeek/hyprdynamicmonitors/internal/power"
	"github.com/fiffeek/hyprdynamicmonitors/internal/profilemaker"
	"github.com/sirupsen/logrus"
)

type HyprApply struct {
	profileMaker *profilemaker.Service
	generator    *generators.ConfigGenerator
}

func NewHyprApply(profileMaker *profilemaker.Service, generator *generators.ConfigGenerator) *HyprApply {
	return &HyprApply{
		profileMaker: profileMaker,
		generator:    generator,
	}
}

// ApplyCurrent applies the edited monitors to the running Hyprland instance without persisting them.
// The rules are rendered right away (the monitors keep being edited), but Hyprland is called from
// the returned command so the UI is not blocked. No shell is involved, so descriptions are passed verbatim.
func (h *HyprApply) ApplyCurrent(monitors []*MonitorSpec, format hyprconf.Format) tea.Cmd {
	rules := make([]string, 0, len(monitors))
	for _, monitor := range monitors {
		if format == hyprconf.FormatLua {
			rules = append(rules, monitor.ToHyprConfig(hyprconf.FormatLua))
		} else {
			rules = append(rules, monitor.ToHypr())
		}
	}

	return func() tea.Msg {
		var err error
		if format == hyprconf.FormatLua {
			err = applyLua(rules)
		} else {
			err = applyHyprlang(rules)
		}
		return OperationStatusCmd(OperationNameEphemeralApply, err)()
	}
}

func applyHyprlang(rules []string) error {
	var lastError error
	for _, rule := range rules {
		// nolint:gosec,noctx
		cmd := exec.Command("hyprctl", "keyword", "monitor", rule)
		if out, err := cmd.CombinedOutput(); err != nil {
			lastError = fmt.Errorf("hyprctl keyword monitor failed: %w: %s", err, strings.TrimSpace(string(out)))
			logrus.WithError(lastError).Error("cant apply hypr settings")
		}
	}
	return lastError
}

// applyLua sends all hl.monitor rules in a single eval so Hyprland applies them in one monitor refresh.
func applyLua(rules []string) error {
	if len(rules) == 0 {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := hypr.EvalLua(ctx, strings.Join(rules, "; ")); err != nil {
		logrus.WithError(err).Error("cant apply hypr settings")
		return fmt.Errorf("cant apply monitors via hyprland lua eval: %w", err)
	}
	return nil
}

func (h *HyprApply) CreateProfile(monitors []*MonitorSpec, name, file string) tea.Cmd {
	hyprMonitors, err := ConvertToHyprMonitors(monitors)
	if err != nil {
		return OperationStatusCmd(OperationNameCreateProfile, err)
	}
	err = h.profileMaker.FreezeGivenAs(name, file, hyprMonitors)
	return OperationStatusCmd(OperationNameCreateProfile, err)
}

func (h *HyprApply) EditProfile(monitors []*MonitorSpec, name string) tea.Cmd {
	hyprMonitors, err := ConvertToHyprMonitors(monitors)
	if err != nil {
		return OperationStatusCmd(OperationNameEditProfile, err)
	}
	err = h.profileMaker.EditExisting(name, hyprMonitors)
	return OperationStatusCmd(OperationNameEditProfile, err)
}

func (h *HyprApply) GenerateThroughHDM(cfg *config.Config, profile *matchers.MatchedProfile,
	monitors []*MonitorSpec, powerState power.PowerState, lidState power.LidState,
) tea.Cmd {
	if profile == nil {
		return OperationStatusCmd(OperationNameHydrate, errors.New("profile is nil"))
	}
	hyprMonitors, err := ConvertToHyprMonitors(monitors)
	if err != nil {
		return OperationStatusCmd(OperationNameHydrate, err)
	}
	destination := *cfg.Get().General.Destination
	_, err = h.generator.GenerateConfig(cfg.Get(), profile, hyprMonitors, powerState, lidState, destination, false)
	return OperationStatusCmd(OperationNameHydrate, err)
}
