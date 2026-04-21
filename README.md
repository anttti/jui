# jui

A terminal UI for Jira Cloud. Syncs your issues into a local SQLite cache and
browses them with vim-style keybindings — list view, detail view, live
filtering, clipboard yank, and open-in-browser.

Built in Go on [Bubble Tea](https://github.com/charmbracelet/bubbletea) and
[Cobra](https://github.com/spf13/cobra). SQLite via
[modernc.org/sqlite](https://pkg.go.dev/modernc.org/sqlite) (pure Go, no CGO).

## Install

Requires Go 1.24+.

```sh
go build -o jira .
# or
task build
```

The binary is called `jira`.

## Configure

On first run a default config is written to `~/.config/jui/jui.toml`:

```toml
site = "acme.atlassian.net"
email = "you@example.com"
api_token = ""                 # or set JIRA_API_TOKEN
jql = "assignee = currentUser() AND resolution = Unresolved"
sync_interval = "5m"
initial_lookback = "90d"

[ui]
theme = "dark"
```

Create an API token at
<https://id.atlassian.com/manage-profile/security/api-tokens>. You can put it
in `api_token`, or prefer `JIRA_API_TOKEN` in your shell env.

The local cache lives next to the config at `~/.config/jui/jira.db`.

## Commands

```
jira                   # launch the TUI
jira sync              # run one sync cycle and exit
jira daemon            # long-running sync loop (used by launchd)
jira doctor            # validate config, DB, Jira auth
jira agent install     # install the macOS LaunchAgent
jira agent uninstall   # remove the LaunchAgent
```

`--config <path>` overrides the config location on any subcommand.

## Background sync (macOS)

`jira agent install` writes a plist to
`~/Library/LaunchAgents/` and bootstraps the daemon under `launchd`. Logs go
to `~/Library/Logs/jira-tui/`. Remove it with `jira agent uninstall`.

## Keybindings

**List view**

| Key         | Action                          |
|-------------|---------------------------------|
| `j` / `k`   | cursor down / up                |
| `gg` / `G`  | jump to top / bottom            |
| `Ctrl-d/u`  | half-page down / up             |
| `Ctrl-f/b`  | page down / up                  |
| `Enter` / `l` | open selected issue           |
| `o`         | open issue URL in browser       |
| `yy`        | yank issue key to clipboard     |
| `/`         | search (incremental)            |
| `:`         | command; type an issue key to jump |
| `t` / `s` / `a` | focus type / status / assignee chip |
| `Esc`       | dismiss search / chip / command |

In a chip: `j/k` to move, `Space` to toggle (type, status) or `Enter` to
commit, `Esc` to cancel.

**Detail view**

| Key       | Action                        |
|-----------|-------------------------------|
| `j` / `k` | scroll down / up              |
| `gg` / `G`| top / bottom                  |
| `Ctrl-d/u`| half-page scroll              |
| `]` / `[` | next / previous issue         |
| `o`       | open in browser               |
| `y`       | yank issue key                |
| `h` / `q` / `Esc` | back to the list      |

`Ctrl-C` quits from anywhere.

## Development

```sh
task test       # go test ./...
task vet        # go vet ./...
task run -- doctor
```

Layout:

```
cmd/              cobra wiring and runner entrypoints
internal/config   TOML config loader
internal/jira     Jira REST client + ADF rendering
internal/model    core domain types
internal/store    store interface + sqlite/memstore/test kits
internal/sync     sync engine (incremental via updated > watermark)
internal/tui      Bubble Tea root, list, and detail views
internal/launchd  macOS LaunchAgent install / uninstall
internal/platform OS integrations (clipboard, URL opener)
```
