package cmd

import (
	"database/sql"
	"fmt"
	"os"
	"os/user"
	"path/filepath"

	"github.com/Lumos-Labs-HQ/kmax/internal/config"
	"github.com/Lumos-Labs-HQ/kmax/internal/ui"
	_ "modernc.org/sqlite"
)

const (
	flagCac = "cac"
	flagUbu = "ubu"
)

func realHome() string {
	if u, err := user.Current(); err == nil {
		return u.HomeDir
	}
	home, _ := os.UserHomeDir()
	return home
}

func cachyosDataDB() string {
	return filepath.Join(realHome(), ".local", "share", "kiro-cli", "data.sqlite3")
}

func ubuntuDataDB() string {
	return filepath.Join(realHome(), ".kiro-ubuntu-account", ".local", "share", "kiro-cli", "data.sqlite3")
}

func Sync(from string) {
	var srcDB, dstDB string

	switch from {
	case flagCac:
		srcDB = cachyosDataDB()
		dstDB = ubuntuDataDB()
		ui.Info("Syncing conversations: CachyOS → Ubuntu")
	case flagUbu:
		srcDB = ubuntuDataDB()
		dstDB = cachyosDataDB()
		ui.Info("Syncing conversations: Ubuntu → CachyOS")
	default:
		config.Die("usage: kmax sync -f cac|ubu")
	}

	if _, err := os.Stat(srcDB); err != nil {
		config.Die("source database not found:", srcDB)
	}
	if _, err := os.Stat(dstDB); err != nil {
		config.Die("destination database not found:", dstDB)
	}

	src, err := sql.Open("sqlite", srcDB+"?mode=ro")
	if err != nil {
		config.Die("failed to open source db:", err)
	}
	defer src.Close()

	dst, err := sql.Open("sqlite", dstDB)
	if err != nil {
		config.Die("failed to open destination db:", err)
	}
	defer dst.Close()

	convCount := 0
	if tableExists(src, "conversations") && tableExists(dst, "conversations") {
		n, err := syncConversations(src, dst)
		if err != nil {
			config.Die("error syncing conversations:", err)
		}
		convCount = n
	}

	conv2Count := 0
	if tableExists(src, "conversations_v2") && tableExists(dst, "conversations_v2") {
		n, err := syncConversationsV2(src, dst)
		if err != nil {
			config.Die("error syncing conversations_v2:", err)
		}
		conv2Count = n
	}

	fmt.Println()
	ui.Success(fmt.Sprintf("Synced %d conversation(s), %d conversation_v2 row(s)", convCount, conv2Count))
	fmt.Println(ui.Dim("  Restart kiro-cli to see synced conversations"))
	fmt.Println()
}

func tableExists(db *sql.DB, name string) bool {
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?", name).Scan(&count)
	return err == nil && count > 0
}

func syncConversations(src, dst *sql.DB) (int, error) {
	rows, err := src.Query("SELECT key, value FROM conversations")
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	tx, err := dst.Begin()
	if err != nil {
		return 0, err
	}

	count := 0
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			tx.Rollback()
			return 0, err
		}
		_, err := tx.Exec(`INSERT INTO conversations (key, value) VALUES (?, ?)
			ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value)
		if err != nil {
			tx.Rollback()
			return 0, err
		}
		count++
	}

	return count, tx.Commit()
}

func syncConversationsV2(src, dst *sql.DB) (int, error) {
	rows, err := src.Query("SELECT key, conversation_id, value, created_at, updated_at FROM conversations_v2")
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	tx, err := dst.Begin()
	if err != nil {
		return 0, err
	}

	count := 0
	for rows.Next() {
		var key, convID, value string
		var createdAt, updatedAt int64
		if err := rows.Scan(&key, &convID, &value, &createdAt, &updatedAt); err != nil {
			tx.Rollback()
			return 0, err
		}
		_, err := tx.Exec(`INSERT INTO conversations_v2 (key, conversation_id, value, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?)
			ON CONFLICT(key, conversation_id) DO UPDATE SET
				value = excluded.value,
				updated_at = excluded.updated_at`,
			key, convID, value, createdAt, updatedAt)
		if err != nil {
			tx.Rollback()
			return 0, err
		}
		count++
	}

	return count, tx.Commit()
}
