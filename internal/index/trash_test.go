package index

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

var trashedAt = time.Date(2026, 8, 20, 9, 30, 0, 0, time.UTC)

// trashPathOf はゴミ箱へ入れたあとのパスを組み立てる。
func trashPathOf(p string) string { return ".trash/" + p }

// trash はテスト対象の画像をゴミ箱へ入れる。
func trash(t *testing.T, db *DB, id int64, orig string) {
	t.Helper()
	if err := db.Trash(context.Background(), id, trashPathOf(orig), trashedAt); err != nil {
		t.Fatalf("Trash(%d) error = %v", id, err)
	}
}

func TestTrash_ゴミ箱へ入れた画像は検索に出てこない(t *testing.T) {
	// Given: 4 枚のうち 1 枚をゴミ箱へ入れる
	db := newFixtureDB(t)
	id := idOf(t, db, pathGirl)

	// When
	trash(t, db, id, pathGirl)

	// Then
	res, err := db.Search(context.Background(), Query{})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if res.Total != 3 {
		t.Errorf("Total = %d, want 3", res.Total)
	}
	for _, img := range res.Images {
		if img.ID == id {
			t.Errorf("ゴミ箱の画像 %s が一覧に出ている", img.Path)
		}
	}
}

func TestTrash_ゴミ箱の中身だけを取り出せる(t *testing.T) {
	// Given
	db := newFixtureDB(t)
	id := idOf(t, db, pathGirl)
	trash(t, db, id, pathGirl)

	// When
	res, err := db.Search(context.Background(), Query{Trashed: true})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}

	// Then
	if res.Total != 1 {
		t.Fatalf("Total = %d, want 1", res.Total)
	}
	got := res.Images[0]
	if got.ID != id {
		t.Errorf("ID = %d, want %d", got.ID, id)
	}
	if got.Path != trashPathOf(pathGirl) {
		t.Errorf("Path = %q, want %q", got.Path, trashPathOf(pathGirl))
	}
	if got.OrigPath != pathGirl {
		t.Errorf("OrigPath = %q, want %q", got.OrigPath, pathGirl)
	}
	if !got.TrashedAt.Equal(trashedAt) {
		t.Errorf("TrashedAt = %v, want %v", got.TrashedAt, trashedAt)
	}
}

func TestTrash_ゴミ箱の中身はルートで絞り込める(t *testing.T) {
	// Given: ルートの異なる 2 枚をゴミ箱へ入れる
	db := newFixtureDB(t)
	other := &Image{Root: "other", Path: "a/b.png", CreatedAt: fixtureBase}
	put(t, db, other)
	trash(t, db, idOf(t, db, pathGirl), pathGirl)
	trash(t, db, other.ID, other.Path)

	// When
	res, err := db.Search(context.Background(), Query{Trashed: true, Roots: []string{"other"}})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}

	// Then
	if res.Total != 1 {
		t.Fatalf("Total = %d, want 1", res.Total)
	}
	if res.Images[0].ID != other.ID {
		t.Errorf("ID = %d, want %d", res.Images[0].ID, other.ID)
	}
}

func TestTrash_ファセットとタグ補完はゴミ箱を数えない(t *testing.T) {
	// Given: modelA の 2 枚のうち 1 枚をゴミ箱へ入れる
	db := newFixtureDB(t)
	trash(t, db, idOf(t, db, pathGirl), pathGirl)
	ctx := context.Background()

	// When
	facets, err := db.Facets(ctx, Query{})
	if err != nil {
		t.Fatalf("Facets() error = %v", err)
	}

	// Then: modelA は残った 1 枚だけ数える
	if got := facetCount(facets.Models, "modelA"); got != 1 {
		t.Errorf("modelA の件数 = %d, want 1", got)
	}
	// Then: ゴミ箱の画像だけが持つ LoRA は候補から消える
	if got := facetCount(facets.Loras, "char_a"); got != 1 {
		t.Errorf("char_a の件数 = %d, want 1", got)
	}

	tags, err := db.TagSuggest(ctx, "smile", 10)
	if err != nil {
		t.Fatalf("TagSuggest() error = %v", err)
	}
	if len(tags) != 0 {
		t.Errorf("TagSuggest() = %v, ゴミ箱の画像のタグは候補に出さない", tags)
	}
}

func TestTrash_ゴミ箱の件数をルートごとに返す(t *testing.T) {
	// Given
	db := newFixtureDB(t)
	other := &Image{Root: "other", Path: "a/b.png", CreatedAt: fixtureBase}
	put(t, db, other)
	trash(t, db, idOf(t, db, pathGirl), pathGirl)
	trash(t, db, idOf(t, db, pathBoy), pathBoy)
	trash(t, db, other.ID, other.Path)

	// When
	counts, err := db.TrashCounts(context.Background())
	if err != nil {
		t.Fatalf("TrashCounts() error = %v", err)
	}

	// Then: 件数の多い順に並ぶ
	want := []TrashCount{{Root: "out", Count: 2}, {Root: "other", Count: 1}}
	if !slices.Equal(counts, want) {
		t.Errorf("TrashCounts() = %v, want %v", counts, want)
	}
}

func TestRestore_元のパスへ戻すと再び検索に出る(t *testing.T) {
	// Given
	db := newFixtureDB(t)
	id := idOf(t, db, pathGirl)
	trash(t, db, id, pathGirl)

	// When
	if err := db.Restore(context.Background(), id, pathGirl); err != nil {
		t.Fatalf("Restore() error = %v", err)
	}

	// Then
	img, err := db.Get(context.Background(), id)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if !img.TrashedAt.IsZero() {
		t.Errorf("TrashedAt = %v, want ゼロ値", img.TrashedAt)
	}
	if img.Path != pathGirl {
		t.Errorf("Path = %q, want %q", img.Path, pathGirl)
	}
	if img.OrigPath != "" {
		t.Errorf("OrigPath = %q, want 空", img.OrigPath)
	}
	if !slices.Contains(searchPaths(t, db, Query{}), pathGirl) {
		t.Error("戻した画像が一覧に出てこない")
	}
}

func TestRestore_戻し先が変わってもディレクトリと名前を付け替える(t *testing.T) {
	// Given
	db := newFixtureDB(t)
	id := idOf(t, db, pathGirl)
	trash(t, db, id, pathGirl)

	// When: 元のパスが埋まっていて別名で戻す
	const renamed = "txt2img/2026-08-13/00001 (2).png"
	if err := db.Restore(context.Background(), id, renamed); err != nil {
		t.Fatalf("Restore() error = %v", err)
	}

	// Then
	img, err := db.Get(context.Background(), id)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if img.Path != renamed {
		t.Errorf("Path = %q, want %q", img.Path, renamed)
	}
	if img.Dir != "txt2img/2026-08-13" {
		t.Errorf("Dir = %q, want %q", img.Dir, "txt2img/2026-08-13")
	}
	if img.Name != "00001 (2).png" {
		t.Errorf("Name = %q, want %q", img.Name, "00001 (2).png")
	}
}

func TestPurge_行ごとインデックスから取り除く(t *testing.T) {
	// Given
	db := newFixtureDB(t)
	id := idOf(t, db, pathGirl)
	trash(t, db, id, pathGirl)

	// When
	if err := db.Purge(context.Background(), id); err != nil {
		t.Fatalf("Purge() error = %v", err)
	}

	// Then
	if _, err := db.Get(context.Background(), id); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get() error = %v, want ErrNotFound", err)
	}
	res, err := db.Search(context.Background(), Query{Trashed: true})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if res.Total != 0 {
		t.Errorf("ゴミ箱の件数 = %d, want 0", res.Total)
	}
}

func TestPurge_ゴミ箱にない画像は消さない(t *testing.T) {
	// Given
	db := newFixtureDB(t)
	id := idOf(t, db, pathGirl)

	// When
	err := db.Purge(context.Background(), id)

	// Then
	if !errors.Is(err, ErrNotTrashed) {
		t.Fatalf("Purge() error = %v, want ErrNotTrashed", err)
	}
	if _, err := db.Get(context.Background(), id); err != nil {
		t.Errorf("Get() error = %v, 行が残っていてほしい", err)
	}
}

func TestRestore_ゴミ箱にない画像は戻せない(t *testing.T) {
	// Given
	db := newFixtureDB(t)
	id := idOf(t, db, pathGirl)

	// When
	err := db.Restore(context.Background(), id, pathGirl)

	// Then
	if !errors.Is(err, ErrNotTrashed) {
		t.Errorf("Restore() error = %v, want ErrNotTrashed", err)
	}
}

func TestTrash_すでにゴミ箱にある画像は入れ直さない(t *testing.T) {
	// Given
	db := newFixtureDB(t)
	id := idOf(t, db, pathGirl)
	trash(t, db, id, pathGirl)

	// When
	err := db.Trash(context.Background(), id, ".trash/other.png", trashedAt)

	// Then
	if !errors.Is(err, ErrTrashed) {
		t.Fatalf("Trash() error = %v, want ErrTrashed", err)
	}
	img, err := db.Get(context.Background(), id)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if img.OrigPath != pathGirl {
		t.Errorf("OrigPath = %q, 元のパスを上書きしてはいけない", img.OrigPath)
	}
}

func TestStates_ゴミ箱の画像は差分スキャンの対象から外す(t *testing.T) {
	// Given
	db := newFixtureDB(t)
	id := idOf(t, db, pathGirl)
	trash(t, db, id, pathGirl)
	ctx := context.Background()

	// When
	states, err := db.States(ctx, "out")
	if err != nil {
		t.Fatalf("States() error = %v", err)
	}

	// Then: 元のパスもゴミ箱のパスも現れない
	if _, ok := states[pathGirl]; ok {
		t.Error("States() にゴミ箱の画像の元パスが含まれている")
	}
	if _, ok := states[trashPathOf(pathGirl)]; ok {
		t.Error("States() にゴミ箱の中のパスが含まれている")
	}
	if _, ok, err := db.State(ctx, "out", trashPathOf(pathGirl)); err != nil || ok {
		t.Errorf("State() = ok %v, err %v, ゴミ箱の画像は見つけない", ok, err)
	}
}

func TestPut_ゴミ箱へ入れた跡地へ新しい画像を登録できる(t *testing.T) {
	// Given: 画像をゴミ箱へ入れる
	db := newFixtureDB(t)
	old := idOf(t, db, pathGirl)
	trash(t, db, old, pathGirl)

	// When: 同じパスへ別の画像が現れる
	fresh := &Image{
		Root: "out", Path: pathGirl, CreatedAt: fixtureBase, Size: 99,
		Prompt: "1cat", Model: "modelC",
	}
	put(t, db, fresh)

	// Then: 別の行として登録され、ゴミ箱の行は残る
	if fresh.ID == old {
		t.Errorf("ID = %d, ゴミ箱の行を上書きしている", fresh.ID)
	}
	if !slices.Contains(searchPaths(t, db, Query{}), pathGirl) {
		t.Error("新しい画像が一覧に出てこない")
	}
	trashed, err := db.Get(context.Background(), old)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if trashed.TrashedAt.IsZero() {
		t.Error("ゴミ箱の行が上書きされている")
	}
}

func TestDelete_ゴミ箱の画像は監視の削除では消えない(t *testing.T) {
	// Given
	db := newFixtureDB(t)
	id := idOf(t, db, pathGirl)
	trash(t, db, id, pathGirl)
	ctx := context.Background()

	// When: 監視が元のパスの消失を拾ったつもりで削除を掛ける
	if err := db.Delete(ctx, "out", pathGirl); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}
	if err := db.DeleteUnder(ctx, "out", ".trash"); err != nil {
		t.Fatalf("DeleteUnder() error = %v", err)
	}

	// Then
	if _, err := db.Get(ctx, id); err != nil {
		t.Errorf("Get() error = %v, ゴミ箱の行は残っていてほしい", err)
	}
}

// facetCount は指定した値のファセット件数を返す。見つからなければ 0。
func facetCount(values []FacetValue, want string) int {
	for _, v := range values {
		if v.Value == want {
			return v.Count
		}
	}
	return 0
}

// idOf は登録済みの画像のパスから ID を引く。
func idOf(t *testing.T, db *DB, p string) int64 {
	t.Helper()
	state, ok, err := db.State(context.Background(), "out", p)
	if err != nil || !ok {
		t.Fatalf("State(%s) = ok %v, err %v", p, ok, err)
	}
	return state.ID
}

func TestOpen_ゴミ箱の列がないインデックスを開ける(t *testing.T) {
	// Given: ゴミ箱の列を持たない、古い形のインデックス
	dbPath := filepath.Join(t.TempDir(), "index.db")
	old, err := sql.Open("sqlite", "file:"+dbPath)
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	legacy := strings.NewReplacer(
		"    trashed_at    INTEGER NOT NULL DEFAULT 0,\n", "",
		"    orig_path     TEXT    NOT NULL DEFAULT '',\n", "",
	).Replace(schema)
	if _, err := old.Exec(legacy); err != nil {
		t.Fatalf("古いスキーマの作成に失敗した: %v", err)
	}
	if _, err := old.Exec(`INSERT INTO images (root, path) VALUES ('out', 'a.png')`); err != nil {
		t.Fatalf("行の登録に失敗した: %v", err)
	}
	if err := old.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	// When
	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer db.Close()

	// Then: 既存の行は残り、ゴミ箱の外として扱われる
	if got := searchPaths(t, db, Query{}); !slices.Equal(got, []string{"a.png"}) {
		t.Errorf("searchPaths() = %v, want [a.png]", got)
	}
}
