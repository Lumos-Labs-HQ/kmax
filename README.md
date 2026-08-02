# kmax

Manage multiple kiro-cli accounts on a single machine. Swap between sessions, track monthly usage, and keep a unified conversation history across all accounts.

## How it works

Each account is stored as a `.sqlite3` file in `~/.local/share/kiro-cli/kiro_data/`. The active account is `~/.local/share/kiro-cli/data.sqlite3`.

On every swap kmax:

1. Saves the current `data.sqlite3` back to the active session file.
2. Merges conversation history from all sessions into the target session.
3. Copies the target session file to `data.sqlite3`.

## Setup

```bash
mkdir -p ~/.local/share/kiro-cli/kiro_data
# copy or move existing data.sqlite3 files there, named 1.sqlite3, 2.sqlite3, etc.
```

## Build & Install

```bash
task build      # builds ./kmax binary
task install    # installs to $GOPATH/bin
```

Or without Task:

```bash
go build -o kmax .
go install .
```

## Commands

```
kmax list              List all sessions with status
kmax swap              Mark current session as ended, switch to next unused this month
kmax use <id>          Switch to a specific session by ID or name
kmax end <id>          Mark a session as ended (skipped by swap)
kmax reset [<id>]      Unend all sessions (or one), clearing used_at
kmax credits [<id>]    Show live credit usage (defaults to active session)
kmax login [-a]        Log in to a new account and save it as a session (-a for device flow)
kmax sync -f cac       Sync conversations from CachyOS → Ubuntu
kmax sync -f ubu       Sync conversations from Ubuntu → CachyOS
kmax clean             Clear history of all sessions except the active one
kmax clean -f          Clear history of the current active session only
kmax continue          Open the conversation picker to resume any previous chat
kmax c                 Alias for continue
```

## Session lifecycle

- `swap` picks the next session that is not ended and was not used this calendar month.
- `reset` clears both the ended flag and used_at, making sessions available again.
- Sessions are identified by numeric ID (position in sorted file list) or filename.

## clean

`kmax clean` deletes all conversation rows from every inactive session file and runs `VACUUM` to shrink the file on disk. The active session is untouched.

`kmax clean -f` deletes conversation rows from the current active session only, then syncs the result back to the session file. Restart kiro-cli after running this.

Auth tokens, state, migrations, and all other tables are never touched by clean.

## Notes

- Requires `kiro-cli` to be on PATH.
- Session files must be readable and writable by the user running kmax.
- The `credits` command reads the OAuth token stored in the session DB. For the active session it always reads from the live `data.sqlite3` in case kiro-cli has refreshed the token.
- Respects `XDG_DATA_HOME` if set (useful for isolated accounts via wrapper scripts).
