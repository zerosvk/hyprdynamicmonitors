// Package reloader provides a service that listens to file change notifications
// and issues an application-wide reload
package reloader

import (
	"context"
	"errors"
	"fmt"

	"github.com/fiffeek/hyprdynamicmonitors/internal/config"
	"github.com/sirupsen/logrus"
	"golang.org/x/sync/errgroup"
)

type IPowerDetector interface {
	Reload(context.Context) error
}

type ILidDetector interface {
	Reload(context.Context) error
}

type IService interface {
	UpdateOnce(context.Context) error
}

type IFilewatcher interface {
	Update() error
	Listen() <-chan interface{}
}

type IGenerators interface {
	ValidateTemplates() error
}

type INotifier interface {
	NotifyError(summary string, cause error, dryRun bool) error
}

type Service struct {
	cfg                  *config.Config
	filewatcher          IFilewatcher
	powerDetector        IPowerDetector
	lidDetector          ILidDetector
	service              IService
	gen                  IGenerators
	disableAutoHotReload *bool
	notifier             INotifier
	dryRun               bool
}

// NewService creates the reloader, notifier can be nil
func NewService(cfg *config.Config, filewatcher IFilewatcher, powerDetector IPowerDetector,
	service IService, disableAutoHotReload bool, lidDetector ILidDetector, gen IGenerators,
	notifier INotifier, dryRun bool,
) *Service {
	return &Service{
		cfg:                  cfg,
		filewatcher:          filewatcher,
		powerDetector:        powerDetector,
		lidDetector:          lidDetector,
		service:              service,
		gen:                  gen,
		disableAutoHotReload: &disableAutoHotReload,
		notifier:             notifier,
		dryRun:               dryRun,
	}
}

// Handle reloads on SIGHUP, a failed reload is reported but does not stop the daemon
func (s *Service) Handle(ctx context.Context) error {
	s.reloadAndReport(ctx)
	return nil
}

// reloadAndReport runs a reload and reports a failure instead of returning it,
// a broken config while the user is editing it should not stop the daemon,
// the previously loaded config stays in use when the new one cant be parsed
func (s *Service) reloadAndReport(ctx context.Context) {
	err := s.Reload(ctx)
	if err == nil || ctx.Err() != nil {
		return
	}
	logrus.WithError(err).Error("Reload failed, keeping the daemon running")
	if s.notifier == nil {
		return
	}
	if nerr := s.notifier.NotifyError("reload failed", err, s.dryRun); nerr != nil {
		logrus.WithError(nerr).Error("swallowing notification error")
	}
}

func (s *Service) Reload(ctx context.Context) error {
	updates := []struct {
		Fun  func() error
		Name string
		Err  string
	}{
		{Fun: s.cfg.Reload, Name: "config reload", Err: "cant reload configuration"},
		{Fun: s.gen.ValidateTemplates, Name: "generators template validation", Err: "cant validate new templates"},
		{Fun: s.filewatcher.Update, Name: "update filewatcher", Err: "cant update filewatcher"},
		{Fun: func() error { return s.powerDetector.Reload(ctx) }, Name: "power detector reload", Err: "cant reload powerDetector"},
		{Fun: func() error { return s.lidDetector.Reload(ctx) }, Name: "lid detector reload", Err: "cant reload lidDetector"},
		{
			Fun:  func() error { return s.service.UpdateOnce(ctx) },
			Name: "updating user configuration", Err: "cant update user service",
		},
	}

	for _, update := range updates {
		logrus.Debug("Executing " + update.Name)
		if err := update.Fun(); err != nil {
			return fmt.Errorf("%s: %w", update.Err, err)
		}
	}

	return nil
}

func (s *Service) Run(ctx context.Context) error {
	eg, ctx := errgroup.WithContext(ctx)
	eg.Go(func() error {
		<-ctx.Done()
		logrus.Debug("Context cancelled for reloader, shutting down")
		return context.Cause(ctx)
	})

	if s.disableAutoHotReload != nil && *s.disableAutoHotReload {
		logrus.Info("Disabling reloader, no files will be watched")
		return eg.Wait()
	}

	watcherEventsChannel := s.filewatcher.Listen()

	eg.Go(func() error {
		logrus.Debug("Reloader event processor starting")
		for {
			select {
			case _, ok := <-watcherEventsChannel:
				if !ok {
					return errors.New("watcher event channel closed")
				}
				logrus.Debug("Watcher event received")
				s.reloadAndReport(ctx)

			case <-ctx.Done():
				logrus.Debug("Reloader event processor context cancelled, shutting down")
				return context.Cause(ctx)

			}
		}
	})

	return eg.Wait()
}
