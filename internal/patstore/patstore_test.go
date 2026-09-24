package patstore

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

// openTemp 在临时目录打开一个独立的 SQLite 库，测试结束自动清理。
func openTemp(t *testing.T) *Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "pats.db")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// TestOpenCreatesTable 验证打开库后 pats 表存在且可查询。
// 破坏点：建表语句被删或表名写错。
func TestOpenCreatesTable(t *testing.T) {
	s := openTemp(t)
	pats, err := s.ListEnabled()
	if err != nil {
		t.Fatalf("ListEnabled on fresh db: %v", err)
	}
	if len(pats) != 0 {
		t.Fatalf("fresh db should be empty, got %d", len(pats))
	}
}

// TestImportCountsAndDedup 验证导入计数：新 PAT 计入 imported，重复计入 duplicated。
// 破坏点：INSERT OR IGNORE 失效导致重复插入报错或计数错。
func TestImportCountsAndDedup(t *testing.T) {
	s := openTemp(t)

	imported, duplicated, err := s.Import([]string{"pat-a", "pat-b", "pat-a"})
	if err != nil {
		t.Fatal(err)
	}
	if imported != 2 || duplicated != 1 {
		t.Fatalf("first import: imported=%d duplicated=%d, want 2/1", imported, duplicated)
	}

	imported, duplicated, err = s.Import([]string{"pat-a", "pat-c"})
	if err != nil {
		t.Fatal(err)
	}
	if imported != 1 || duplicated != 1 {
		t.Fatalf("second import: imported=%d duplicated=%d, want 1/1", imported, duplicated)
	}
}

// TestListEnabledOrdersByID 验证只返回 enabled=1 的 PAT，且按 id 升序。
// 破坏点：WHERE 条件缺失返回已禁用行，或排序反了。
func TestListEnabledOrdersByID(t *testing.T) {
	s := openTemp(t)
	if _, _, err := s.Import([]string{"pat-1", "pat-2", "pat-3"}); err != nil {
		t.Fatal(err)
	}

	all, err := s.ListEnabled()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Fatalf("got %d pats, want 3", len(all))
	}
	for i, p := range all {
		if p.PAT != []string{"pat-1", "pat-2", "pat-3"}[i] {
			t.Fatalf("order[%d]=%s, want pat-%d", i, p.PAT, i+1)
		}
	}

	if err := s.Disable(all[1].ID); err != nil {
		t.Fatal(err)
	}
	left, err := s.ListEnabled()
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 2 || left[0].PAT != "pat-1" || left[1].PAT != "pat-3" {
		t.Fatalf("after disable got %+v", left)
	}
}

// TestDisableSetsDisabledTime 验证禁用后 enabled=0 且 disabled_time 被写入。
// 破坏点：UPDATE 漏写 disabled_time 列。
func TestDisableSetsDisabledTime(t *testing.T) {
	s := openTemp(t)
	if _, _, err := s.Import([]string{"pat-x"}); err != nil {
		t.Fatal(err)
	}
	pats, err := s.ListEnabled()
	if err != nil {
		t.Fatal(err)
	}

	if err := s.Disable(pats[0].ID); err != nil {
		t.Fatal(err)
	}

	var enabled int
	var disabledTime *int64
	row := s.db.QueryRow("SELECT enabled, disabled_time FROM pats WHERE id=?", pats[0].ID)
	if err := row.Scan(&enabled, &disabledTime); err != nil {
		t.Fatal(err)
	}
	if enabled != 0 {
		t.Fatalf("enabled=%d, want 0", enabled)
	}
	if disabledTime == nil || *disabledTime == 0 {
		t.Fatalf("disabled_time not set: %v", disabledTime)
	}
}

// TestImportEmpty 验证空数组导入不报错且计数为零。
func TestImportEmpty(t *testing.T) {
	s := openTemp(t)
	imported, duplicated, err := s.Import(nil)
	if err != nil {
		t.Fatal(err)
	}
	if imported != 0 || duplicated != 0 {
		t.Fatalf("imported=%d duplicated=%d, want 0/0", imported, duplicated)
	}
}

// TestIncCallCount 验证调用次数可累加。
// 破坏点：IncCall 的 UPDATE 未累加（写成赋值）、或列名错。
func TestIncCallCount(t *testing.T) {
	s := openTemp(t)
	if _, _, err := s.Import([]string{"pat-a"}); err != nil {
		t.Fatal(err)
	}
	pats, err := s.ListEnabled()
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 3; i++ {
		if err := s.IncCall(pats[0].ID); err != nil {
			t.Fatal(err)
		}
	}

	var n int
	if err := s.db.QueryRow("SELECT call_count FROM pats WHERE id=?", pats[0].ID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Fatalf("call_count=%d, want 3", n)
	}
}

// TestOpenMigratesCallCount 验证旧版（无 call_count 列）数据库打开时自动补列。
// 破坏点：Open 未执行 ALTER TABLE 迁移，老库 ListEnabled 报列不存在。
func TestOpenMigratesCallCount(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	old, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := old.Exec(`CREATE TABLE pats (
		id            INTEGER PRIMARY KEY AUTOINCREMENT,
		pat           TEXT NOT NULL UNIQUE,
		enabled       INTEGER NOT NULL DEFAULT 1,
		create_time   INTEGER NOT NULL,
		disabled_time INTEGER
	);`); err != nil {
		t.Fatal(err)
	}
	if _, err := old.Exec(`INSERT INTO pats (pat, enabled, create_time) VALUES ('legacy-pat', 1, 1)`); err != nil {
		t.Fatal(err)
	}
	if err := old.Close(); err != nil {
		t.Fatal(err)
	}

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	pats, err := s.ListEnabled()
	if err != nil {
		t.Fatal(err)
	}
	if len(pats) != 1 || pats[0].PAT != "legacy-pat" || pats[0].CallCount != 0 {
		t.Fatalf("migrated row wrong: %+v", pats)
	}
	if err := s.IncCall(pats[0].ID); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := s.db.QueryRow("SELECT call_count FROM pats WHERE id=?", pats[0].ID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("call_count after migrate+inc=%d, want 1", n)
	}
}

// TestOpenMissingDir 验证父目录不存在时 Open 报错而非 panic。
func TestOpenMissingDir(t *testing.T) {
	path := filepath.Join(os.TempDir(), "definitely-missing-dir-xyz", "pats.db")
	if _, err := Open(path); err == nil {
		t.Fatal("Open in missing dir should fail")
	}
}
