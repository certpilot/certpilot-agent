package agent

import (
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"testing"
	"time"

	"github.com/certpilot/certpilot-agent-sdk/agentauth"
)

// TestAnIdentitySurvivesARestart — which is every restart of every host the
// agent is installed on.
func TestAnIdentitySurvivesARestart(t *testing.T) {
	dir := t.TempDir()
	_, priv, err := agentauth.GenerateKey()
	if err != nil {
		t.Fatalf("keygen: %v", err)
	}

	state := State{
		AgentID: "abc", Server: "https://core:8080", Name: "web-01",
		KeyID: "deadbeef", HeartbeatIntervalSeconds: 300, EnrolledAt: time.Now(),
	}
	if err := SaveIdentity(dir, priv, state); err != nil {
		t.Fatalf("save: %v", err)
	}

	loadedKey, loadedState, err := LoadIdentity(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if !loadedKey.Equal(priv) {
		t.Fatal("the key did not come back")
	}
	if loadedState.AgentID != state.AgentID || loadedState.Server != state.Server {
		t.Fatalf("the state did not come back: %#v", loadedState)
	}
}

// TestTheDirectoryAndKeyAreOwnerOnly. The directory matters as much as the
// file: a world-readable directory holding a 0600 key is not a leak today and
// becomes one the moment a second file is written into it without thinking.
func TestTheDirectoryAndKeyAreOwnerOnly(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "state")
	_, priv, _ := agentauth.GenerateKey()
	if err := SaveIdentity(dir, priv, State{AgentID: "a", Server: "s"}); err != nil {
		t.Fatalf("save: %v", err)
	}

	// Asked in the terms the platform actually enforces. A mode is the answer
	// on Unix; on Windows every file reports 0666 whatever its ACL says, so a
	// mode assertion here would pass on a world-readable key and fail on a
	// correct one.
	for _, name := range []string{keyFile, stateFile} {
		path := filepath.Join(dir, name)
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat %s: %v", path, err)
		}
		if err := keyIsPrivate(path, info); err != nil {
			t.Errorf("%s is not private: %v", name, err)
		}
	}

	if runtime.GOOS == "windows" {
		// Windows directories have no mode either, and the ACL on this one is
		// asserted by TestAPrivateKeyIsNotReadableByOtherAccounts in
		// platform_windows_test.go, which runs on a real Windows kernel.
		return
	}

	for path, want := range map[string]os.FileMode{
		dir:                           0o700,
		filepath.Join(dir, keyFile):   0o600,
		filepath.Join(dir, stateFile): 0o600,
	} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat %s: %v", path, err)
		}
		if got := info.Mode().Perm(); got != want {
			t.Fatalf("%s is mode %04o, want %04o", path, got, want)
		}
	}
}

// TestAReadableKeyIsRefusedRatherThanWarnedAbout.
//
// A private key other accounts on the host can read is not a slightly less
// secure agent — every one of those accounts can now speak to CertPilot as this
// machine. An agent that carried on after printing a warning would be an agent
// whose warning nobody reads.
func TestAReadableKeyIsRefusedRatherThanWarnedAbout(t *testing.T) {
	if runtime.GOOS == "windows" {
		// os.Chmod cannot make a file readable on Windows, so this test cannot
		// create the condition it is about. The equivalent — an identity key
		// whose ACL grants Everyone — is
		// TestAnIdentityKeyGrantingEveryoneIsRefused in
		// platform_windows_test.go.
		t.Skip("Unix file modes; the Windows equivalent is in platform_windows_test.go")
	}
	dir := t.TempDir()
	_, priv, _ := agentauth.GenerateKey()
	if err := SaveIdentity(dir, priv, State{AgentID: "a", Server: "s"}); err != nil {
		t.Fatalf("save: %v", err)
	}
	if err := os.Chmod(filepath.Join(dir, keyFile), 0o644); err != nil {
		t.Fatalf("chmod: %v", err)
	}

	_, _, err := LoadIdentity(dir)
	if err == nil {
		t.Fatal("a world-readable identity key must be refused")
	}
	// The message has to contain the fix, because whoever hits this is trying
	// to get an agent running and not to read about file modes.
	if !strings.Contains(err.Error(), "chmod 600") {
		t.Fatalf("the error should say how to fix it, got: %v", err)
	}
}

// TestAHostWithNoIdentitySaysWhatToRun.
func TestAHostWithNoIdentitySaysWhatToRun(t *testing.T) {
	_, _, err := LoadIdentity(t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "enrol") {
		t.Fatalf("the error should point at enrolment, got: %v", err)
	}
	if Enrolled(t.TempDir()) {
		t.Fatal("an empty directory is not an enrolled host")
	}
}

// What an agent reports about itself is what the fleet view answers "which of
// these hosts is running something old" with, and what the core's compatibility
// matrix records for each released agent it measures. These cover the four ways
// a binary can arrive at a host.
func TestWhatTheAgentSaysItsVersionIs(t *testing.T) {
	buildInfo := func(version string) func() (*debug.BuildInfo, bool) {
		return func() (*debug.BuildInfo, bool) {
			return &debug.BuildInfo{Main: debug.Module{Version: version}}, true
		}
	}

	for _, tc := range []struct {
		name    string
		stamped string
		read    func() (*debug.BuildInfo, bool)
		want    string
	}{
		{
			// The container build. An explicit statement beats an inference.
			name:    "a stamped build keeps its stamp",
			stamped: "0.9.1",
			read:    buildInfo("v0.2.0"),
			want:    "0.9.1",
		},
		{
			// `go install github.com/certpilot/certpilot-agent/cmd@v0.2.0`.
			// This reported 0.1.0-dev before, and the core believed it.
			name:    "go install takes the module version",
			stamped: devVersion,
			read:    buildInfo("v0.2.0"),
			want:    "0.2.0",
		},
		{
			// A modified working tree. Go appends +dirty itself, so this does
			// not claim to be the release it was branched from — which is the
			// whole reason the default carried a -dev suffix.
			name:    "a modified working tree says so",
			stamped: devVersion,
			read:    buildInfo("v0.2.0+dirty"),
			want:    "0.2.0+dirty",
		},
		{
			// Between tags. The pseudo-version names the commit, which is more
			// than a hand-maintained constant could ever say.
			name:    "a commit between tags reports the commit",
			stamped: devVersion,
			read:    buildInfo("v0.2.1-0.20260914130547-2043ed16411a"),
			want:    "0.2.1-0.20260914130547-2043ed16411a",
		},
		{
			// -buildvcs=false, or a build from outside a repository. Nothing is
			// known, and the default is the honest answer.
			name:    "no version to derive stays a development build",
			stamped: devVersion,
			read:    buildInfo("(devel)"),
			want:    devVersion,
		},
		{
			name:    "no build info at all is a development build",
			stamped: devVersion,
			read:    func() (*debug.BuildInfo, bool) { return nil, false },
			want:    devVersion,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolveVersion(tc.stamped, tc.read); got != tc.want {
				t.Errorf("reported %q, want %q", got, tc.want)
			}
		})
	}
}
