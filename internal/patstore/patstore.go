// Package patstore 用 SQLite 持久化 Qoder 个人令牌（PAT），支持启用/禁用
// 与按 id 排序的轮换读取。
package patstore

import (
	"database/sql"
	"strings"
	"time"

	_ "modernc.org/sqlite" // 纯 Go SQLite 驱动，驱动名 "sqlite"
)

// Pat 是 pats 表中的一行。
type Pat struct {
	ID        int64
	PAT       string
	CallCount int
}

// Store 封装了 PAT 表的一个 SQLite 连接。
type Store struct {
	db *sql.DB
}

// Open 打开（或创建）指定路径的 SQLite 库，并确保 pats 表存在；老库自动
// 迁移补上 call_count 列。
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	// SQLite 单写者；限制连接数避免并发写锁竞争。
	db.SetMaxOpenConns(1)

	const schema = `
CREATE TABLE IF NOT EXISTS pats (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    pat           TEXT NOT NULL UNIQUE,
    enabled       INTEGER NOT NULL DEFAULT 1,
    create_time   INTEGER NOT NULL,
    disabled_time INTEGER,
    call_count    INTEGER NOT NULL DEFAULT 0
);
`
	if _, err := db.Exec(schema); err != nil {
		_ = db.Close()
		return nil, err
	}
	// 迁移：老库没有 call_count 列时补上（SQLite 无 IF NOT EXISTS 加列）。
	if _, err := db.Exec("ALTER TABLE pats ADD COLUMN call_count INTEGER NOT NULL DEFAULT 0"); err != nil {
		// duplicate column name 说明列已存在，忽略；其余错误上报。
		if !strings.Contains(err.Error(), "duplicate column") {
			_ = db.Close()
			return nil, err
		}
	}
	return &Store{db: db}, nil
}

// Close 关闭底层连接。
func (s *Store) Close() error { return s.db.Close() }

// Import 将给定 PAT 数组写入表，重复的（按 pat UNIQUE）跳过。
// 返回 (新导入数量, 已存在跳过数量)。
func (s *Store) Import(pats []string) (imported, duplicated int, err error) {
	now := time.Now().Unix()
	for _, p := range pats {
		res, err := s.db.Exec(
			"INSERT OR IGNORE INTO pats (pat, enabled, create_time) VALUES (?, 1, ?)",
			p, now,
		)
		if err != nil {
			return imported, duplicated, err
		}
		if n, _ := res.RowsAffected(); n == 1 {
			imported++
		} else {
			duplicated++
		}
	}
	return imported, duplicated, nil
}

// ListEnabled 返回所有 enabled=1 的 PAT，按 id 升序。
func (s *Store) ListEnabled() ([]Pat, error) {
	rows, err := s.db.Query("SELECT id, pat, call_count FROM pats WHERE enabled=1 ORDER BY id ASC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Pat
	for rows.Next() {
		var p Pat
		if err := rows.Scan(&p.ID, &p.PAT, &p.CallCount); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// IncCall 将指定 id 的 PAT 调用次数 +1。
func (s *Store) IncCall(id int64) error {
	_, err := s.db.Exec("UPDATE pats SET call_count = call_count + 1 WHERE id=?", id)
	return err
}

// Disable 将指定 id 的 PAT 置为 enabled=0 并记录 disabled_time。
func (s *Store) Disable(id int64) error {
	_, err := s.db.Exec(
		"UPDATE pats SET enabled=0, disabled_time=? WHERE id=?",
		time.Now().Unix(), id,
	)
	return err
}
