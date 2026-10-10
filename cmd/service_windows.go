//go:build windows

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	agent "github.com/certpilot/certpilot-agent"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/eventlog"
	"golang.org/x/sys/windows/svc/mgr"
)

func serviceAvailable() error { return nil }

// runningAsService reports whether the service manager started this process.
func runningAsService() bool {
	ok, err := svc.IsWindowsService()
	return err == nil && ok
}

func installService(stateDir, specPath string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	exe, err = filepath.Abs(exe)
	if err != nil {
		return err
	}

	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("could not reach the service manager; run this as Administrator: %w", err)
	}
	defer m.Disconnect()

	if s, err := m.OpenService(serviceName); err == nil {
		s.Close()
		return fmt.Errorf("a %s service is already installed; run `certpilot-agent service uninstall` first", serviceName)
	}

	args := []string{"run", "--state-dir", stateDir}
	if specPath != "" {
		args = append(args, "--installs", specPath)
	}
	s, err := m.CreateService(serviceName, exe, mgr.Config{
		DisplayName: "CertPilot agent",
		Description: "Requests certificates for this host and installs them where its services read them.",
		StartType:   mgr.StartAutomatic,
	}, args...)
	if err != nil {
		return fmt.Errorf("could not create the service: %w", err)
	}
	defer s.Close()

	// Restart after 30 seconds, as the systemd unit does. On a non-zero exit
	// too, not only a crash: that is how the agent says something went wrong.
	restart := mgr.RecoveryAction{Type: mgr.ServiceRestart, Delay: 30 * time.Second}
	if err := s.SetRecoveryActions([]mgr.RecoveryAction{restart, restart, restart}, 24*60*60); err != nil {
		return fmt.Errorf("could not set the service to restart: %w", err)
	}
	if err := s.SetRecoveryActionsOnNonCrashFailures(true); err != nil {
		return fmt.Errorf("could not set the service to restart: %w", err)
	}

	// Where the agent's log goes when there is no console. Already there after
	// an earlier install, which is fine.
	_ = eventlog.InstallAsEventCreate(serviceName, eventlog.Error|eventlog.Warning|eventlog.Info)

	fmt.Printf("installed the %s service for %s\nstart it with: Start-Service %s\n", serviceName, stateDir, serviceName)
	return nil
}

func uninstallService() error {
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("could not reach the service manager; run this as Administrator: %w", err)
	}
	defer m.Disconnect()

	s, err := m.OpenService(serviceName)
	if err != nil {
		return fmt.Errorf("no %s service is installed", serviceName)
	}
	defer s.Close()

	if status, err := s.Control(svc.Stop); err == nil {
		deadline := time.Now().Add(30 * time.Second)
		for status.State != svc.Stopped && time.Now().Before(deadline) {
			time.Sleep(300 * time.Millisecond)
			if status, err = s.Query(); err != nil {
				break
			}
		}
	}
	if err := s.Delete(); err != nil {
		return fmt.Errorf("could not remove the service: %w", err)
	}
	_ = eventlog.Remove(serviceName)
	fmt.Printf("removed the %s service; its identity is left where it was\n", serviceName)
	return nil
}

// runAsService hands this process to the service manager and runs the agent
// until it is told to stop.
func runAsService(run func(context.Context) error) error {
	if elog, err := eventlog.Open(serviceName); err == nil {
		defer elog.Close()
		slog.SetDefault(slog.New(&eventLogHandler{log: elog}))
	}
	return svc.Run(serviceName, &serviceHandler{run: run})
}

type serviceHandler struct {
	run func(context.Context) error
}

func (h *serviceHandler) Execute(_ []string, requests <-chan svc.ChangeRequest, status chan<- svc.Status) (bool, uint32) {
	status <- svc.Status{State: svc.StartPending}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- h.run(ctx) }()

	status <- svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}
	for {
		select {
		case err := <-done:
			// A revoked credential stops cleanly. Exiting with an error would
			// have the service manager restart it every 30 seconds, which is a
			// revoked credential still knocking on the door.
			if err == nil || errors.Is(err, agent.ErrRevoked) {
				return false, 0
			}
			slog.Error("the agent stopped", "error", err)
			return false, 1

		case req := <-requests:
			switch req.Cmd {
			case svc.Interrogate:
				status <- req.CurrentStatus
			case svc.Stop, svc.Shutdown:
				status <- svc.Status{State: svc.StopPending}
				cancel()
				select {
				case <-done:
				case <-time.After(20 * time.Second):
				}
				return false, 0
			}
		}
	}
}

// eventLogHandler writes the agent's log to the Windows Event Log, the place
// an administrator looks for a service's messages.
type eventLogHandler struct {
	log *eventlog.Log
	// with replays WithAttrs and WithGroup onto the text handler that formats
	// each record, in the order they were called.
	with []func(slog.Handler) slog.Handler
}

func (h *eventLogHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= slog.LevelInfo
}

func (h *eventLogHandler) Handle(ctx context.Context, r slog.Record) error {
	var b bytes.Buffer
	var inner slog.Handler = slog.NewTextHandler(&b, &slog.HandlerOptions{
		// The Event Log stamps its own time.
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if len(groups) == 0 && a.Key == slog.TimeKey {
				return slog.Attr{}
			}
			return a
		},
	})
	for _, w := range h.with {
		inner = w(inner)
	}
	if err := inner.Handle(ctx, r); err != nil {
		return err
	}
	msg := b.String()
	switch {
	case r.Level >= slog.LevelError:
		return h.log.Error(1, msg)
	case r.Level >= slog.LevelWarn:
		return h.log.Warning(1, msg)
	default:
		return h.log.Info(1, msg)
	}
}

func (h *eventLogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return h.extend(func(inner slog.Handler) slog.Handler { return inner.WithAttrs(attrs) })
}

func (h *eventLogHandler) WithGroup(name string) slog.Handler {
	return h.extend(func(inner slog.Handler) slog.Handler { return inner.WithGroup(name) })
}

func (h *eventLogHandler) extend(w func(slog.Handler) slog.Handler) slog.Handler {
	with := append(append([]func(slog.Handler) slog.Handler{}, h.with...), w)
	return &eventLogHandler{log: h.log, with: with}
}
