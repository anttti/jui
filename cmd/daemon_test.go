package cmd_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/anttti/j/cmd"
)

// TestDaemon_RunsSyncLoopUntilContextCancelled verifies the daemon loop
// invokes Sync repeatedly and exits cleanly on context cancellation.
func TestDaemon_RunsSyncLoopUntilContextCancelled(t *testing.T) {
	fj := &fakeJira{}
	srv := httptest.NewServer(fj.handler())
	defer srv.Close()
	cfg := withBaseURL(writeConfig(t, strings.TrimPrefix(srv.URL, "http://")), srv.URL)
	cfg.SyncInterval = 10 * time.Millisecond // tight loop, but bounded

	ctx, cancel := context.WithCancel(context.Background())
	var buf bytes.Buffer
	var wg sync.WaitGroup
	wg.Add(1)
	var err error
	go func() {
		defer wg.Done()
		err = cmd.Daemon(ctx, cfg, &buf)
	}()
	// Let it run a couple of cycles, then cancel.
	time.Sleep(60 * time.Millisecond)
	cancel()
	wg.Wait()
	if err != nil && err != context.Canceled {
		t.Fatalf("Daemon: %v", err)
	}
	// At least one sync should have completed.
	if !strings.Contains(buf.String(), "sync: ok") {
		t.Fatalf("expected at least one 'sync: ok' line:\n%s", buf.String())
	}
}

func TestDaemon_PrintsErrorOnSyncFailure(t *testing.T) {
	// Server that always 500s on /search/jql so Sync returns an error.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	cfg := withBaseURL(writeConfig(t, "x"), srv.URL)
	cfg.SyncInterval = 5 * time.Millisecond

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	var buf bytes.Buffer
	_ = cmd.Daemon(ctx, cfg, &buf)
	if !strings.Contains(buf.String(), "sync error:") {
		t.Fatalf("expected 'sync error:' line:\n%s", buf.String())
	}
}
