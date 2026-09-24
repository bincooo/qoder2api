package patpool

import (
	"errors"
	"path/filepath"
	"testing"

	"qoder2api/internal/cosy"
	"qoder2api/internal/patstore"
)

// fakeBuilder 为每个 PAT 生成一个可识别的会话，方便断言轮换顺序。
// pat 值被编码进会话的 CosyKey，用它来断言返回的是哪个会话。
func fakeBuilder(pat string) (*cosy.SessionContext, error) {
	return &cosy.SessionContext{CosyKey: pat}, nil
}

// failingBuilder 对指定的 PAT 返回错误，其余成功。
func failingBuilder(failOn string) func(string) (*cosy.SessionContext, error) {
	return func(pat string) (*cosy.SessionContext, error) {
		if pat == failOn {
			return nil, errors.New("boom")
		}
		return &cosy.SessionContext{CosyKey: pat}, nil
	}
}

func newTestPool(t *testing.T, pats []string, build func(string) (*cosy.SessionContext, error)) (*Pool, *patstore.Store) {
	t.Helper()
	store, err := patstore.Open(filepath.Join(t.TempDir(), "pats.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if _, _, err := store.Import(pats); err != nil {
		t.Fatal(err)
	}
	pool, err := NewPool(store, build)
	if err != nil {
		t.Fatal(err)
	}
	return pool, store
}

// TestNewPoolBuildsAllEnabled 验证池为所有 enabled PAT 构建会话，构建失败的跳过。
// 破坏点：跳过了成功构建的 PAT，或把失败的也塞进池。
func TestNewPoolBuildsAllEnabled(t *testing.T) {
	pool, _ := newTestPool(t, []string{"a", "b", "c"}, failingBuilder("b"))
	if got := pool.Size(); got != 2 {
		t.Fatalf("Size=%d, want 2", got)
	}
}

// TestNextRoundRobins 验证按 id 升序依次返回，到头回绕。
// 破坏点：next 不递增、不回绕、或返回顺序错。
func TestNextRoundRobins(t *testing.T) {
	pool, _ := newTestPool(t, []string{"a", "b"}, fakeBuilder)

	id1, s1, ok1 := pool.Next()
	id2, s2, ok2 := pool.Next()
	id3, s3, ok3 := pool.Next()

	if !ok1 || !ok2 || !ok3 {
		t.Fatal("expected all Next calls ok")
	}
	if s1.CosyKey != "a" || s2.CosyKey != "b" || s3.CosyKey != "a" {
		t.Fatalf("round robin wrong: %s,%s,%s", s1.CosyKey, s2.CosyKey, s3.CosyKey)
	}
	if id1 == id2 {
		t.Fatal("consecutive Next should differ")
	}
	_ = id3
}

// TestNextEmptyPool 验证空池返回 ok=false。
// 破坏点：空池 Next 返回了零值会话且 ok=true。
func TestNextEmptyPool(t *testing.T) {
	pool, _ := newTestPool(t, []string{}, failingBuilder("x"))
	if _, _, ok := pool.Next(); ok {
		t.Fatal("empty pool Next should return ok=false")
	}
}

// TestMarkDeadDisablesAndRemoves 验证 MarkDead 后该 PAT 不再被返回，且库中已禁用。
// 破坏点：移除失败导致仍被轮换，或未调用 store.Disable。
func TestMarkDeadDisablesAndRemoves(t *testing.T) {
	pool, store := newTestPool(t, []string{"a", "b"}, fakeBuilder)

	idA, _, _ := pool.Next()
	pool.MarkDead(idA)

	if got := pool.Size(); got != 1 {
		t.Fatalf("Size after MarkDead=%d, want 1", got)
	}
	// 剩下唯一会话应是 "b"
	_, s, ok := pool.Next()
	if !ok || s.CosyKey != "b" {
		t.Fatalf("expected b, got ok=%v sess=%v", ok, s)
	}

	left, err := store.ListEnabled()
	if err != nil {
		t.Fatal(err)
	}
	if len(left) != 1 || left[0].PAT != "b" {
		t.Fatalf("store should disable a, got %+v", left)
	}
}

// TestRefreshPicksUpNewPats 验证 Refresh 后新导入的 PAT 会进入池。
// 破坏点：Refresh 未重新读取库，或未重建会话。
func TestRefreshPicksUpNewPats(t *testing.T) {
	pool, store := newTestPool(t, []string{"a"}, fakeBuilder)

	if pool.Size() != 1 {
		t.Fatalf("Size=%d, want 1", pool.Size())
	}
	if _, _, err := store.Import([]string{"b"}); err != nil {
		t.Fatal(err)
	}
	if err := pool.Refresh(); err != nil {
		t.Fatal(err)
	}
	if got := pool.Size(); got != 2 {
		t.Fatalf("Size after Refresh=%d, want 2", got)
	}
}
