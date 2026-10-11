//go:build !windows

package main

import (
	"context"
	"errors"
)

var errNotWindows = errors.New("`service` is for Windows. On Linux, the .deb and .rpm install a systemd unit and a timer; see docs/agent.md")

func serviceAvailable() error { return errNotWindows }

func runningAsService() bool { return false }

func runAsService(func(context.Context) error) error { return errNotWindows }

func installService(string, string) error { return errNotWindows }

func uninstallService() error { return errNotWindows }
