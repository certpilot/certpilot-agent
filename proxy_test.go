package agent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"sync"
	"testing"
	"time"

	"github.com/certpilot/certpilot-agent-sdk/agentauth"
)

// A host that reaches the core only through a proxy is the commonest reason an
// agent "does not work" in a corporate network, so it is pinned here rather
// than left to be true of the standard library.
//
// Go reads the proxy variables once per process, and never for a loopback
// destination, so the test re-runs itself as a child with the variables set
// and asks it to reach a name that only a proxy could answer.
const proxyChildEnv = "CERTPILOT_PROXY_TEST_CHILD"

func TestAgentTrafficGoesThroughTheProxyTheEnvironmentNames(t *testing.T) {
	if os.Getenv(proxyChildEnv) != "" {
		t.Skip("running as the child")
	}

	var mu sync.Mutex
	seen := map[string]bool{}
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen[r.URL.Path] = true
		mu.Unlock()
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer proxy.Close()

	cmd := exec.Command(os.Args[0], "-test.run=^TestProxyChild$")
	cmd.Env = append(os.Environ(),
		proxyChildEnv+"=1",
		"HTTP_PROXY="+proxy.URL, "http_proxy="+proxy.URL,
		"NO_PROXY=", "no_proxy=",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("child failed: %v\n%s", err, out)
	}

	mu.Lock()
	defer mu.Unlock()
	for _, path := range []string{"/api/v1/agent/enrol", "/api/v1/agent/heartbeat"} {
		if !seen[path] {
			t.Errorf("%s never reached the proxy; it went direct (saw %v)", path, seen)
		}
	}
}

// TestProxyChild is the other half of the test above, and does nothing on its
// own.
func TestProxyChild(t *testing.T) {
	if os.Getenv(proxyChildEnv) == "" {
		t.Skip("only runs as the child of the proxy test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Not a loopback name: Go never proxies those.
	const core = "http://core.invalid"

	// Both fail, because the proxy answers 503. What matters is where they went.
	_, _ = Enrol(ctx, EnrolOptions{Server: core, Token: "t", StateDir: t.TempDir()})

	_, key, err := agentauth.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	_, _ = NewRunner(NewClient(core, "agent-1", key), State{}, "").Heartbeat(ctx)
}
