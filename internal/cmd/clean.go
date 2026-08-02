package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/Lumos-Labs-HQ/kmax/internal/config"
	"github.com/Lumos-Labs-HQ/kmax/internal/db"
	"github.com/Lumos-Labs-HQ/kmax/internal/session"
	"github.com/Lumos-Labs-HQ/kmax/internal/ui"
)

// left completely intact (auth, state, migrations, kmax_meta are untouched).
func clearHistory(dbPath string) error {
	d, err := db.Open(dbPath)
	if err != nil {
		return fmt.Errorf("open %s: %w", dbPath, err)
	}
	defer d.Close()

	// conversations table (always present in kiro-cli DBs)
	if _, err := d.Exec(`DELETE FROM conversations`); err != nil {
		// Table may not exist in very old session files — that's fine.
		if !strings.Contains(err.Error(), "no such table") {
			return fmt.Errorf("clear conversations in %s: %w", dbPath, err)
		}
	}

	// conversations_v2 — only present after kiro-cli migration 7
	var hasV2 int
	d.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name='conversations_v2'`).Scan(&hasV2)
	if hasV2 > 0 {
		if _, err := d.Exec(`DELETE FROM conversations_v2`); err != nil {
			return fmt.Errorf("clear conversations_v2 in %s: %w", dbPath, err)
		}
	}

	// Reclaim freed pages so the file actually shrinks on disk.
	if _, err := d.Exec(`VACUUM`); err != nil {
		return fmt.Errorf("vacuum %s: %w", dbPath, err)
	}

	return nil
}

// Clean wipes conversation history from all sessions except the currently active one.
// Pass force=true to instead wipe only the active (current) session's history.
func Clean(force bool) {
	config.EnsureDataDir()

	sessions, err := session.List(config.KiroDataDir, config.DataDB)
	if err != nil {
		config.Die("error:", err)
	}

	// Identify active session.
	var activeSess *session.Session
	for i := range sessions {
		if sessions[i].Active {
			activeSess = &sessions[i]
			break
		}
	}

	if force {
		// -f: clean the current (active) session only.
		if activeSess == nil {
			config.Die("no active session found")
		}
		fmt.Printf("\n  %s %s\n\n",
			ui.Bold("Cleaning history for active session:"),
			ui.Cyan(activeSess.FileName))

		// Clear the live data.sqlite3 (what kiro-cli is reading).
		if err := clearHistory(config.DataDB); err != nil {
			config.Die("error:", err)
		}

		// Sync the cleared state back to the session file so it matches.
		data, err := os.ReadFile(config.DataDB)
		if err != nil {
			config.Die("error reading data.sqlite3:", err)
		}
		if err := os.WriteFile(activeSess.File, data, 0600); err != nil {
			config.Die("error writing session file:", err)
		}

		ui.Success(fmt.Sprintf("History cleared for session %s — restart kiro-cli to apply",
			ui.Bold(activeSess.FileName)))
		fmt.Println()
		return
	}

	// Default: clean all sessions except the active one.
	// Collect the file paths to clean.
	var targets []session.Session
	for _, s := range sessions {
		if !s.Active {
			targets = append(targets, s)
		}
	}

	if len(targets) == 0 {
		fmt.Println(ui.Dim("  No inactive sessions to clean."))
		fmt.Println()
		return
	}

	activeLabel := "(none)"
	if activeSess != nil {
		activeLabel = activeSess.FileName
	}
	fmt.Printf("\n  %s %s\n\n",
		ui.Bold("Cleaning history for all sessions except:"),
		ui.Cyan(activeLabel))

	cleaned := 0
	// Each target file is independent — safe to clear in parallel.
	type result struct {
		name string
		err  error
	}
	results := make([]result, len(targets))
	var wg sync.WaitGroup
	for i, s := range targets {
		wg.Add(1)
		go func(idx int, sess session.Session) {
			defer wg.Done()
			filePath := filepath.Join(config.KiroDataDir, sess.FileName+".sqlite3")
			results[idx] = result{name: sess.FileName, err: clearHistory(filePath)}
		}(i, s)
	}
	wg.Wait()

	for _, r := range results {
		if r.err != nil {
			ui.Fail(fmt.Sprintf("%-20s %s", r.name, r.err.Error()))
			continue
		}
		ui.Success(fmt.Sprintf("%-20s history cleared", ui.Bold(r.name)))
		cleaned++
	}

	fmt.Println()
	ui.Success(fmt.Sprintf("Cleaned %d / %d inactive session(s)", cleaned, len(targets)))
	fmt.Println()
}
