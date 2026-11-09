// Package userconfigupdater provides the main service coordination and event handling.
package userconfigupdater

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/fiffeek/hyprdynamicmonitors/internal/config"
	"github.com/fiffeek/hyprdynamicmonitors/internal/generators"
	"github.com/fiffeek/hyprdynamicmonitors/internal/hypr"
	"github.com/fiffeek/hyprdynamicmonitors/internal/matchers"
	"github.com/fiffeek/hyprdynamicmonitors/internal/notifications"
	"github.com/fiffeek/hyprdynamicmonitors/internal/power"
	"github.com/fiffeek/hyprdynamicmonitors/internal/profileapplier"
	"github.com/fiffeek/hyprdynamicmonitors/internal/utils"
	"github.com/sirupsen/logrus"
	"golang.org/x/sync/errgroup"
)

type IPowerDetector interface {
	GetCurrentState() power.PowerState
	Listen() <-chan power.PowerEvent
}

type IMonitorDetector interface {
	Listen() <-chan hypr.MonitorSpecs
	GetConnectedMonitors() hypr.MonitorSpecs
}

type ILidDetector interface {
	Listen() <-chan power.LidEvent
	GetCurrentState() power.LidState
}

type Service struct {
	config               *config.Config
	monitorDetector      IMonitorDetector
	powerDetector        IPowerDetector
	lidDetector          ILidDetector
	matcher              *matchers.Matcher
	serviceConfig        *Config
	generator            *generators.ConfigGenerator
	notificationsService *notifications.Service
	applier              *profileapplier.Applier

	// applyMu serializes matching and applying coming from events, reloads and signals
	applyMu          sync.Mutex
	stateMu          sync.RWMutex
	cachedMonitors   []*hypr.MonitorSpec
	cachedPowerState power.PowerState
	cachedLidState   power.LidState
	debouncer        *utils.Debouncer
}

type Config struct {
	DryRun bool
}

func NewService(cfg *config.Config, monitorDetector IMonitorDetector,
	powerDetector IPowerDetector, svcCfg *Config, matcher *matchers.Matcher, generator *generators.ConfigGenerator,
	notifications *notifications.Service, lidDetector ILidDetector,
) *Service {
	return &Service{
		config:               cfg,
		monitorDetector:      monitorDetector,
		powerDetector:        powerDetector,
		serviceConfig:        svcCfg,
		matcher:              matcher,
		generator:            generator,
		cachedPowerState:     power.BatteryPowerState,
		debouncer:            utils.NewDebouncer(),
		notificationsService: notifications,
		lidDetector:          lidDetector,
		applier:              profileapplier.New(generator, notifications, svcCfg.DryRun),
	}
}

func (s *Service) Run(ctx context.Context) error {
	if err := s.RunOnce(ctx); err != nil {
		return fmt.Errorf("unable to update configuration on start: %w", err)
	}

	monitorEventsChannel := s.monitorDetector.Listen()
	powerEventsChannel := s.powerDetector.Listen()
	lidEventsChannel := s.lidDetector.Listen()
	logrus.Info("Listening for monitor and power events...")

	eg, ctx := errgroup.WithContext(ctx)

	eg.Go(func() error {
		<-ctx.Done()
		s.debouncer.Cancel()
		logrus.Debug("Context cancelled for service, shutting down")
		return context.Cause(ctx)
	})

	eg.Go(func() error {
		logrus.Debug("Running debouncer for userconfigupdater")
		if err := s.debouncer.Run(ctx); err != nil {
			return fmt.Errorf("debouncer failed: %w", err)
		}
		return nil
	})

	eg.Go(func() error {
		for {
			select {
			case lidEvent, ok := <-lidEventsChannel:
				if !ok {
					return errors.New("lid events channel closed")
				}
				s.stateMu.Lock()
				s.cachedLidState = lidEvent.State
				s.stateMu.Unlock()
				s.debouncer.Do(ctx, time.Duration(*s.config.Get().General.DebounceTimeMs)*time.Millisecond, s.debounceUpdate)
			case monitors, ok := <-monitorEventsChannel:
				if !ok {
					return errors.New("monitor events channel closed")
				}
				logrus.WithField("monitor_count", len(monitors)).Debug("Monitor event received")
				s.stateMu.Lock()
				s.cachedMonitors = monitors
				s.stateMu.Unlock()
				s.debouncer.Do(ctx, time.Duration(*s.config.Get().General.DebounceTimeMs)*time.Millisecond, s.debounceUpdate)

			case powerEvent, ok := <-powerEventsChannel:
				if !ok {
					return errors.New("power event channel closed")
				}
				logrus.WithField("power_state", powerEvent.State.String()).Debug("Power event received")
				s.stateMu.Lock()
				s.cachedPowerState = powerEvent.State
				s.stateMu.Unlock()
				s.debouncer.Do(ctx, time.Duration(*s.config.Get().General.DebounceTimeMs)*time.Millisecond, s.debounceUpdate)

			case <-ctx.Done():
				logrus.Debug("Event processor context cancelled, shutting down")
				return context.Cause(ctx)
			}
		}
	})

	if err := eg.Wait(); err != nil {
		return fmt.Errorf("goroutines for service failed %w", err)
	}
	return nil
}

func (s *Service) RunOnce(ctx context.Context) error {
	monitors := s.monitorDetector.GetConnectedMonitors()
	powerState := s.powerDetector.GetCurrentState()
	lidState := s.lidDetector.GetCurrentState()

	s.stateMu.Lock()
	s.cachedMonitors = monitors
	s.cachedPowerState = powerState
	s.cachedLidState = lidState
	s.stateMu.Unlock()

	if err := s.UpdateOnce(ctx); err != nil {
		return fmt.Errorf("unable to update configuration: %w", err)
	}

	return nil
}

// debounceUpdate applies the matching profile after monitor, power or lid events,
// failures are reported but do not stop the daemon, the next event retries
func (s *Service) debounceUpdate(ctx context.Context) error {
	// a cancelled context means superseded by a newer event or shutting down, not an error
	if ctx.Err() == nil {
		s.updateAndReport(ctx)
	}
	return nil
}

// Handle applies the matching profile on SIGUSR1
func (s *Service) Handle(ctx context.Context) error {
	s.updateAndReport(ctx)
	return nil
}

func (s *Service) updateAndReport(ctx context.Context) {
	err := s.UpdateOnce(ctx)
	if err == nil || ctx.Err() != nil {
		return
	}
	logrus.WithError(err).Error("Applying the profile failed, keeping the daemon running")
	if nerr := s.notificationsService.NotifyError("applying the profile failed", err,
		s.serviceConfig.DryRun); nerr != nil {
		logrus.WithError(nerr).Error("swallowing notification error")
	}
}

func (s *Service) UpdateOnce(ctx context.Context) error {
	s.applyMu.Lock()
	defer s.applyMu.Unlock()

	s.stateMu.RLock()
	monitors := s.cachedMonitors
	powerState := s.cachedPowerState
	lidState := s.cachedLidState
	s.stateMu.RUnlock()

	// grab latest config and pass along for the same world-view
	cfg := s.config.Get()

	logrus.WithFields(logrus.Fields{
		"monitor_count": len(monitors),
		"power_state":   powerState.String(),
		"lid_state":     lidState.String(),
		"dry_run":       s.serviceConfig.DryRun,
	}).Debug("Updating configuration")

	found, matchedProfile, err := s.matcher.Match(cfg, monitors, powerState, lidState)
	if err != nil {
		return fmt.Errorf("failed to match a profile %w", err)
	}

	if !found {
		logrus.Info("No matching profile found")
		return nil
	}

	profileFields := logrus.Fields{
		"profile_name": matchedProfile.Profile.Name,
		"config_file":  matchedProfile.Profile.ConfigFile,
		"config_type":  matchedProfile.Profile.ConfigType.Value(),
	}

	if s.serviceConfig.DryRun {
		logrus.WithFields(utils.NewLogrusCustomFields(profileFields).WithLogID(utils.DryRunLogID)).
			Info("[DRY RUN] Using profile")
	}

	logrus.WithFields(profileFields).Info("Using profile")

	if _, err := s.applier.Apply(ctx, cfg, matchedProfile, monitors, powerState, lidState); err != nil {
		return fmt.Errorf("cant apply profile %s: %w", matchedProfile.Profile.Name, err)
	}

	return nil
}
