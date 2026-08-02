package session

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Lumos-Labs-HQ/kmax/internal/db"
	"github.com/google/uuid"
)

type Session struct {
	ID             int
	UUID           string
	File           string
	FileName       string
	Active         bool
	Ended          bool
	Token          string
	TokenExpiresAt time.Time
	UsedAt         time.Time
}

func Load(name, kiroDataDir string) (Session, error) {
	path := filepath.Join(kiroDataDir, name+".sqlite3")
	d, err := db.Open(path)
	if err != nil {
		return Session{}, err
	}
	defer d.Close()

	uid := db.GetMeta(d, "uuid")
	if uid == "" {
		uid = uuid.New().String()
		db.SetMeta(d, "uuid", uid)
	}
	ended := db.GetMeta(d, "ended") == "true"
	var usedAt time.Time
	if s := db.GetMeta(d, "used_at"); s != "" {
		usedAt, _ = time.Parse(time.RFC3339, s)
	}
	token, tokenExpiresAt := db.ReadToken(d)

	return Session{
		UUID: uid, File: path, FileName: name,
		Ended: ended, Token: token, TokenExpiresAt: tokenExpiresAt, UsedAt: usedAt,
	}, nil
}

func List(kiroDataDir, dataDB string) ([]Session, error) {
	entries, err := os.ReadDir(kiroDataDir)
	if err != nil {
		return nil, err
	}

	type indexedEntry struct {
		idx  int
		name string
	}
	var validEntries []indexedEntry
	for i, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sqlite3") {
			validEntries = append(validEntries, indexedEntry{i, strings.TrimSuffix(e.Name(), ".sqlite3")})
		}
	}

	activeUUID := LiveActiveUUID(dataDB)

	sessions := make([]Session, len(validEntries))
	var wg sync.WaitGroup
	for i, entry := range validEntries {
		wg.Add(1)
		go func(slot int, name string) {
			defer wg.Done()
			s, err := Load(name, kiroDataDir)
			if err != nil {
				return
			}
			s.Active = !s.Ended && activeUUID != "" && s.UUID == activeUUID
			sessions[slot] = s
		}(i, entry.name)
	}
	wg.Wait()

	var result []Session
	for _, s := range sessions {
		if s.FileName != "" {
			result = append(result, s)
		}
	}

	statusRank := func(s Session) int {
		if s.Ended {
			return 0
		}
		if s.Active {
			return 1
		}
		return 2
	}
	sort.SliceStable(result, func(i, j int) bool {
		ri, rj := statusRank(result[i]), statusRank(result[j])
		if ri != rj {
			return ri < rj
		}
		a, aerr := strconv.Atoi(result[i].FileName)
		b, berr := strconv.Atoi(result[j].FileName)
		if aerr == nil && berr == nil {
			return a < b
		}
		return result[i].FileName < result[j].FileName
	})
	for i := range result {
		result[i].ID = i + 1
	}
	return result, nil
}

func LiveActiveUUID(dataDB string) string {
	d, err := db.Open(dataDB)
	if err != nil {
		return ""
	}
	defer d.Close()
	return db.GetMeta(d, "active_uuid")
}

func UsedThisMonth(s Session) bool {
	if s.UsedAt.IsZero() {
		return false
	}
	now := time.Now()
	return s.UsedAt.Year() == now.Year() && s.UsedAt.Month() == now.Month()
}

func syncMigrations(srcPath, dstPath string) error {
	src, err := db.Open(srcPath)
	if err != nil {
		return err
	}
	defer src.Close()

	dst, err := db.Open(dstPath)
	if err != nil {
		return err
	}
	defer dst.Close()

	rows, err := src.Query(`SELECT id, version, migration_time FROM migrations`)
	if err != nil {
		return nil
	}
	defer rows.Close()

	for rows.Next() {
		var id, version, migTime int64
		if rows.Scan(&id, &version, &migTime) != nil {
			continue
		}
		dst.Exec(`INSERT OR IGNORE INTO migrations(id, version, migration_time) VALUES(?,?,?)`,
			id, version, migTime)
	}
	return nil
}

func mergeConversations(srcPath, dstPath string) error {
	src, err := db.Open(srcPath)
	if err != nil {
		return err
	}
	defer src.Close()

	dst, err := db.Open(dstPath)
	if err != nil {
		return err
	}
	defer dst.Close()

	var srcHas int
	src.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name='conversations_v2'`).Scan(&srcHas)
	if srcHas == 0 {
		return nil
	}

	var rowCount int
	src.QueryRow(`SELECT count(*) FROM conversations_v2`).Scan(&rowCount)
	if rowCount == 0 {
		return nil
	}

	var tblExists int
	dst.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name='conversations_v2'`).Scan(&tblExists)
	if tblExists == 0 {
		dst.Exec(`CREATE TABLE IF NOT EXISTS conversations_v2 (
			key TEXT NOT NULL,
			conversation_id TEXT NOT NULL,
			value TEXT NOT NULL,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL,
			PRIMARY KEY (key, conversation_id)
		)`)
		dst.Exec(`CREATE INDEX IF NOT EXISTS idx_conversations_v2_key_updated ON conversations_v2(key, updated_at DESC)`)
		dst.Exec(`CREATE INDEX IF NOT EXISTS idx_conversations_v2_updated_at ON conversations_v2(updated_at DESC)`)
		var migID, migVersion int64
		if src.QueryRow(`SELECT id, version FROM migrations WHERE version=7`).Scan(&migID, &migVersion) == nil {
			now := time.Now().UnixMilli()
			dst.Exec(`INSERT OR IGNORE INTO migrations(id, version, migration_time) VALUES(?,?,?)`, migID, migVersion, now)
		}
	} else {
		var dstLatest int64
		dst.QueryRow(`SELECT COALESCE(max(updated_at),0) FROM conversations_v2`).Scan(&dstLatest)
		var srcNewer int
		src.QueryRow(`SELECT count(*) FROM conversations_v2 WHERE updated_at > ?`, dstLatest).Scan(&srcNewer)
		if srcNewer == 0 {
			return nil
		}
	}

	rows, err := src.Query(`SELECT key, conversation_id, value, created_at, updated_at FROM conversations_v2`)
	if err != nil {
		return nil
	}
	defer rows.Close()

	tx, _ := dst.Begin()
	for rows.Next() {
		var key, convID, value string
		var createdAt, updatedAt int64
		if rows.Scan(&key, &convID, &value, &createdAt, &updatedAt) != nil {
			continue
		}
		tx.Exec(`INSERT INTO conversations_v2(key,conversation_id,value,created_at,updated_at)
			VALUES(?,?,?,?,?)
			ON CONFLICT(key,conversation_id) DO UPDATE SET
				value=excluded.value,
				updated_at=excluded.updated_at
			WHERE excluded.updated_at > conversations_v2.updated_at`,
			key, convID, value, createdAt, updatedAt)
	}
	return tx.Commit()
}

func readTokenFromDB(dataDB string) (token string, expiresAt time.Time) {
	d, err := db.Open(dataDB)
	if err != nil {
		return
	}
	defer d.Close()
	return db.ReadToken(d)
}

func readAuthKV(dataDB string) string {
	d, err := db.Open(dataDB)
	if err != nil {
		return ""
	}
	defer d.Close()
	var raw string
	d.QueryRow(`SELECT value FROM auth_kv WHERE key='kirocli:social:token'`).Scan(&raw)
	return raw
}

func writeAuthKV(dataDB, raw string) {
	d, err := db.Open(dataDB)
	if err != nil {
		return
	}
	defer d.Close()
	d.Exec(`INSERT OR REPLACE INTO auth_kv(key,value) VALUES('kirocli:social:token',?)`, raw)
}

func repairStateTable(dbPath string) {
	d, err := db.Open(dbPath)
	if err != nil {
		return
	}
	defer d.Close()

	var tblSQL string
	if d.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name='state'`).Scan(&tblSQL) != nil {
		return
	}
	if !strings.Contains(tblSQL, "value BLOB") {
		d.Exec(`DELETE FROM state WHERE key='welcomeAnnouncement.showCount'`)
		d.Exec(`BEGIN`)
		d.Exec(`ALTER TABLE state RENAME TO state_old`)
		d.Exec(`CREATE TABLE state (key TEXT PRIMARY KEY, value BLOB)`)
		d.Exec(`INSERT INTO state SELECT key, value FROM state_old`)
		d.Exec(`DROP TABLE state_old`)
		d.Exec(`COMMIT`)
	} else {
		d.Exec(`DELETE FROM state WHERE key='welcomeAnnouncement.showCount'`)
	}
}

func repairConversationsTable(dbPath string) {
	d, err := db.Open(dbPath)
	if err != nil {
		return
	}
	defer d.Close()

	var tblSQL string
	if d.QueryRow(`SELECT sql FROM sqlite_master WHERE type='table' AND name='conversations_v2'`).Scan(&tblSQL) != nil {
		return
	}
	if strings.Contains(tblSQL, "milliseconds") {
		return
	}

	d.Exec(`BEGIN`)
	d.Exec(`ALTER TABLE conversations_v2 RENAME TO conversations_v2_old`)
	d.Exec(`CREATE TABLE conversations_v2 (
		key TEXT NOT NULL,
		conversation_id TEXT NOT NULL,
		value TEXT NOT NULL,
		created_at INTEGER NOT NULL,
		updated_at INTEGER NOT NULL,
		PRIMARY KEY (key, conversation_id)
	)`)
	d.Exec(`INSERT INTO conversations_v2 SELECT * FROM conversations_v2_old`)
	d.Exec(`DROP TABLE conversations_v2_old`)
	d.Exec(`CREATE INDEX IF NOT EXISTS idx_conversations_v2_key_updated ON conversations_v2(key, updated_at DESC)`)
	d.Exec(`CREATE INDEX IF NOT EXISTS idx_conversations_v2_updated_at ON conversations_v2(updated_at DESC)`)
	d.Exec(`COMMIT`)
}

func ensureConversationsTable(dbPath string) {
	d, err := db.Open(dbPath)
	if err != nil {
		return
	}
	defer d.Close()
	d.Exec(`CREATE TABLE IF NOT EXISTS conversations (key TEXT PRIMARY KEY, value TEXT)`)
}

func SyncActiveBack(dataDB, kiroDataDir string) error {
	activeUUID := LiveActiveUUID(dataDB)
	if activeUUID == "" {
		return nil
	}
	sessions, err := List(kiroDataDir, dataDB)
	if err != nil {
		return err
	}
	for _, s := range sessions {
		if s.UUID == activeUUID {
			data, err := os.ReadFile(dataDB)
			if err != nil {
				return err
			}
			return os.WriteFile(s.File, data, 0600)
		}
	}
	return nil
}

func SwapTo(s Session, dataDB, kiroDataDir string) error {
	if err := SyncActiveBack(dataDB, kiroDataDir); err != nil {
		return fmt.Errorf("failed to sync active session back: %w", err)
	}

	oldToken, oldExpiry := readTokenFromDB(dataDB)
	oldRaw := readAuthKV(dataDB)

	entries, _ := os.ReadDir(kiroDataDir)
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sqlite3") {
			continue
		}
		srcPath := filepath.Join(kiroDataDir, e.Name())
		if srcPath == s.File {
			continue
		}
		_ = mergeConversations(srcPath, s.File)
	}

	_ = syncMigrations(dataDB, s.File)

	data, err := os.ReadFile(s.File)
	if err != nil {
		return err
	}
	if err := os.WriteFile(dataDB, data, 0600); err != nil {
		return err
	}

	repairStateTable(dataDB)
	repairStateTable(s.File)
	repairConversationsTable(dataDB)
	repairConversationsTable(s.File)
	ensureConversationsTable(dataDB)
	ensureConversationsTable(s.File)

	if oldToken != "" && !time.Now().After(oldExpiry) {
		newToken, newExpiry := readTokenFromDB(dataDB)
		if newToken == "" || (!newExpiry.IsZero() && time.Now().After(newExpiry)) {
			writeAuthKV(dataDB, oldRaw)
		}
	}

	now := time.Now().Format(time.RFC3339)
	for _, path := range []string{s.File, dataDB} {
		d, err := db.Open(path)
		if err != nil {
			continue
		}
		db.SetMeta(d, "active_uuid", s.UUID)
		db.SetMeta(d, "used_at", now)
		d.Close()
	}
	return nil
}

func Resolve(arg, kiroDataDir, dataDB string) (Session, error) {
	sessions, err := List(kiroDataDir, dataDB)
	if err != nil {
		return Session{}, err
	}
	if n, err2 := strconv.Atoi(arg); err2 == nil {
		for _, s := range sessions {
			if s.ID == n {
				return s, nil
			}
		}
		return Session{}, fmt.Errorf("no session with id %s", arg)
	}
	for _, s := range sessions {
		if s.FileName == arg {
			return s, nil
		}
	}
	return Session{}, fmt.Errorf("session %q not found", arg)
}
