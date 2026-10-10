//go:build windows

package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	agent "github.com/certpilot/certpilot-agent"
	"github.com/certpilot/certpilot-agent-sdk/agentauth"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"
)

// The whole point of a Windows service is what the service manager does with
// it, so this installs the real binary as a real service, starts it, and
// watches it heartbeat to a core. It changes the machine it runs on, which is
// why it needs CERTPILOT_SERVICE_TEST=1 and is only set in CI.
func TestTheAgentRunsAsAWindowsService(t *testing.T) {
	if os.Getenv("CERTPILOT_SERVICE_TEST") != "1" {
		t.Skip("installs a Windows service; set CERTPILOT_SERVICE_TEST=1 to run")
	}

	heartbeats := make(chan struct{}, 16)
	core := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/agent/heartbeat" {
			select {
			case heartbeats <- struct{}{}:
			default:
			}
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer core.Close()

	dir := t.TempDir()
	bin := filepath.Join(dir, "certpilot-agent.exe")
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}

	stateDir := filepath.Join(dir, "state")
	_, key, err := agentauth.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	if err := agent.SaveIdentity(stateDir, key, agent.State{
		AgentID: "agent-1", Server: core.URL, Name: "service-test",
		HeartbeatIntervalSeconds: 30, EnrolledAt: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}

	cli := func(args ...string) {
		t.Helper()
		if out, err := exec.Command(bin, args...).CombinedOutput(); err != nil {
			t.Fatalf("certpilot-agent %v: %v\n%s", args, err, out)
		}
	}
	cli("service", "install", "--state-dir", stateDir)
	t.Cleanup(func() { _ = exec.Command(bin, "service", "uninstall").Run() })

	m, err := mgr.Connect()
	if err != nil {
		t.Fatal(err)
	}
	defer m.Disconnect()
	s, err := m.OpenService("certpilot-agent")
	if err != nil {
		t.Fatalf("installed, but the service manager has no certpilot-agent: %v", err)
	}
	cfg, err := s.Config()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.StartType != mgr.StartAutomatic {
		t.Errorf("start type %d, want automatic: the agent has to come back after a reboot", cfg.StartType)
	}

	if err := s.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	waitFor(t, s, svc.Running)

	select {
	case <-heartbeats:
	case <-time.After(30 * time.Second):
		t.Fatal("the service is running but never sent a heartbeat")
	}

	if _, err := s.Control(svc.Stop); err != nil {
		t.Fatalf("stop: %v", err)
	}
	waitFor(t, s, svc.Stopped)
	s.Close()

	cli("service", "uninstall")
	if s, err := m.OpenService("certpilot-agent"); err == nil {
		s.Close()
		t.Fatal("uninstall left the service registered")
	}
}

func waitFor(t *testing.T, s *mgr.Service, want svc.State) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		status, err := s.Query()
		if err != nil {
			t.Fatal(err)
		}
		if status.State == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("service state %d, want %d", status.State, want)
		}
		time.Sleep(300 * time.Millisecond)
	}
}
