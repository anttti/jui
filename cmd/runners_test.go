package cmd_test

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anttti/j/cmd"
	"github.com/anttti/j/internal/config"
)

// -----------------------------------------------------------------------------
// fake jira server factory
// -----------------------------------------------------------------------------

type fakeJira struct {
	searchCalls int
}

func (f *fakeJira) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/rest/api/3/myself", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"accountId":"acc-me","displayName":"Me","emailAddress":"me@example.com"}`))
	})
	mux.HandleFunc("/rest/api/3/search/jql", func(w http.ResponseWriter, r *http.Request) {
		f.searchCalls++
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"issues": [{
				"id":"10001","key":"ABC-1",
				"fields":{
					"summary":"test issue",
					"description":{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"body"}]}]},
					"issuetype":{"name":"Bug"},
					"status":{"name":"To Do","statusCategory":{"key":"new"}},
					"priority":{"name":"Medium"},
					"project":{"key":"ABC"},
					"labels":[],
					"created":"2026-04-20T10:00:00.000+0000",
					"updated":"2026-04-20T10:00:00.000+0000"
				}
			}],
			"isLast": true
		}`))
	})
	mux.HandleFunc("/rest/api/3/issue/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/comment") {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"comments":[]}`))
			return
		}
		http.NotFound(w, r)
	})
	return mux
}

func writeConfig(t *testing.T, site string) *config.Config {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	body := fmt.Sprintf(`
site = %q
email = "me@example.com"
api_token = "tok"
jql = "project = ABC"
sync_interval = "5m"
initial_lookback = "30d"
`, site)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.LoadFrom(path)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func withBaseURL(cfg *config.Config, url string) *config.Config {
	cfg.BaseURL = url
	return cfg
}

// -----------------------------------------------------------------------------
// tests
// -----------------------------------------------------------------------------

func TestSync_E2E_populatesStoreFromFakeJira(t *testing.T) {
	fj := &fakeJira{}
	srv := httptest.NewServer(fj.handler())
	defer srv.Close()

	cfg := writeConfig(t, strings.TrimPrefix(srv.URL, "http://"))
	// LoadFrom sets BaseURL from site with https prefix; override for tests.
	// We run through Jira package which honours any URL that starts with http.
	// Easiest: rebuild cfg with Site set to include scheme-less URL works
	// because BaseURL = "https://" + Site. We need BaseURL = srv.URL.
	// Approach: patch cfg after the fact via a small Override helper.
	cfg = withBaseURL(cfg, srv.URL)

	var buf bytes.Buffer
	if err := cmd.Sync(context.Background(), cfg, &buf); err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if fj.searchCalls == 0 {
		t.Fatalf("no search calls")
	}
	if !strings.Contains(buf.String(), "sync: ok") {
		t.Fatalf("unexpected stdout: %s", buf.String())
	}

	// Reopen the store to verify.
	st, err := cmd.OpenStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	iss, err := st.Get(context.Background(), "ABC-1")
	if err != nil || iss == nil {
		t.Fatalf("expected ABC-1 in store, got %v / %v", iss, err)
	}
}

func TestDoctor_OK_printsUserInfo(t *testing.T) {
	fj := &fakeJira{}
	srv := httptest.NewServer(fj.handler())
	defer srv.Close()

	cfg := withBaseURL(writeConfig(t, strings.TrimPrefix(srv.URL, "http://")), srv.URL)
	var buf bytes.Buffer
	if err := cmd.Doctor(context.Background(), cfg, &buf); err != nil {
		t.Fatalf("Doctor: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"jira:    ok", "authenticated as Me", "store:   ok"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in output:\n%s", want, out)
		}
	}
}

func TestDoctor_Unreachable_errors(t *testing.T) {
	cfg := withBaseURL(writeConfig(t, "unused"), "http://127.0.0.1:1")
	var buf bytes.Buffer
	err := cmd.Doctor(context.Background(), cfg, &buf)
	if err == nil {
		t.Fatalf("expected error on unreachable server")
	}
}
