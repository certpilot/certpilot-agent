package main

import (
	"errors"
	"flag"
	"fmt"
	"path/filepath"

	agent "github.com/certpilot/certpilot-agent"
)

// serviceName is what the service manager knows the agent as.
const serviceName = "certpilot-agent"

func runServiceCommand(args []string) error {
	if err := serviceAvailable(); err != nil {
		return err
	}
	if len(args) == 0 {
		return errors.New("usage: certpilot-agent service install [--state-dir=DIR] [--installs=FILE] | uninstall")
	}
	switch args[0] {
	case "install":
		fs := flag.NewFlagSet("service install", flag.ExitOnError)
		stateDir := fs.String("state-dir", agent.DefaultStateDir(), "the enrolled identity the service runs as")
		specPath := fs.String("installs", "", "where the destinations are declared (defaults to /etc/certpilot/installs.json)")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		dir, err := filepath.Abs(*stateDir)
		if err != nil {
			return err
		}
		// A service started without an identity fails at once and then again
		// on every restart, and the reason ends up in an event log nobody is
		// looking at yet. Refusing here says it to the person installing.
		if !agent.Enrolled(dir) {
			return fmt.Errorf("no identity in %s: run `certpilot-agent enrol` before installing the service", dir)
		}
		return installService(dir, *specPath)
	case "uninstall":
		return uninstallService()
	default:
		return fmt.Errorf("unknown service command %q: install or uninstall", args[0])
	}
}
