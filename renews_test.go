package agent

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// fakeCore answers certificate requests and remembers what each one said.
type fakeCore struct {
	mu     sync.Mutex
	bodies []map[string]any
}

func (f *fakeCore) handler(t *testing.T) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/agent/certificates" {
			http.NotFound(w, r)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		f.mu.Lock()
		f.bodies = append(f.bodies, body)
		f.mu.Unlock()
		now := time.Now().UTC()
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
			"certificate_id":  "cert-1",
			"common_name":     "web-01.example.com",
			"sans":            []string{"web-01.example.com"},
			"certificate_pem": "-----BEGIN CERTIFICATE-----\nMA==\n-----END CERTIFICATE-----\n",
			"not_after":       now.Add(90 * 24 * time.Hour).Format(time.RFC3339),
			"renew_after":     now.Add(60 * 24 * time.Hour).Format(time.RFC3339),
		}})
	})
}

func (f *fakeCore) last() map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.bodies[len(f.bodies)-1]
}

func fakeRunner(t *testing.T, core *fakeCore) *Runner {
	t.Helper()
	srv := httptest.NewServer(core.handler(t))
	t.Cleanup(srv.Close)
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return NewRunner(NewClient(srv.URL, "agent-1", key), State{AgentID: "agent-1", Server: srv.URL}, t.TempDir())
}

// TestARenewalSaysWhichCertificateItReplaces.
//
// The core recorded every request as a new certificate, so the one a renewal
// replaced stayed ISSUED in the inventory and alerted as it neared expiry about
// a certificate nothing was serving. This host is the only thing that knows
// which certificate it is replacing, so it says so.
func TestARenewalSaysWhichCertificateItReplaces(t *testing.T) {
	core := &fakeCore{}
	r := fakeRunner(t, core)

	if _, err := r.Request(context.Background(), RequestOptions{Names: []string{"web-01.example.com"}}); err != nil {
		t.Fatalf("first request: %v", err)
	}
	if _, ok := core.last()["renews"]; ok {
		t.Fatalf("a first request claimed to renew something: %v", core.last())
	}

	// Due now.
	metaPath := filepath.Join(r.stateDir, certsDir, "web-01.example.com", metaFileName)
	raw, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatal(err)
	}
	var held Held
	if err := json.Unmarshal(raw, &held); err != nil {
		t.Fatal(err)
	}
	held.RenewAfter = time.Now().Add(-time.Minute)
	held.IssuedAt = time.Now().Add(-time.Hour)
	raw, _ = json.Marshal(held)
	if err := os.WriteFile(metaPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}

	if n := r.RenewDue(context.Background()); n != 1 {
		t.Fatalf("renewed %d certificates, want 1", n)
	}
	if got := core.last()["renews"]; got != "cert-1" {
		t.Fatalf("the renewal told the core it replaces %v, want cert-1", got)
	}
}
