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

var favedAt = time.Date(2026, 8, 21, 10, 0, 0, 0, time.UTC)

// fav はテスト対象の画像を Fav に入れる。
func fav(t *testing.T, db *DB, ids ...int64) {
	t.Helper()
	for _, id := range ids {
		if err := db.SetFav(context.Background(), id, true, favedAt); err != nil {
			t.Fatalf("SetFav(%d, true) error = %v", id, err)
		}
	}
}

func TestSetFav_Favにした画像だけを取り出せる(t *testing.T) {
	// Given
	db := newFixtureDB(t)

	// When
	fav(t, db, idOf(t, db, pathBoy), idOf(t, db, pathGirl))

	// Then: 並び順は通常の検索と同じく生成日時の新しい順
	got := searchPaths(t, db, Query{Fav: true})
	if want := []string{pathGirl, pathBoy}; !slices.Equal(got, want) {
		t.Errorf("searchPaths(Fav) = %v, want %v", got, want)
	}
}

func TestSetFav_Favの日時が一覧と詳細に入る(t *testing.T) {
	// Given
	db := newFixtureDB(t)
	id := idOf(t, db, pathGirl)

	// When
	fav(t, db, id)

	// Then
	img, err := db.Get(context.Background(), id)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if !img.FavAt.Equal(favedAt) {
		t.Errorf("Get().FavAt = %v, want %v", img.FavAt, favedAt)
	}
	res, err := db.Search(context.Background(), Query{})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	for _, got := range res.Images {
		if got.ID == id && !got.FavAt.Equal(favedAt) {
			t.Errorf("Search() の FavAt = %v, want %v", got.FavAt, favedAt)
		}
		if got.ID != id && !got.FavAt.IsZero() {
			t.Errorf("%s の FavAt = %v, Fav にしていない画像はゼロ値", got.Path, got.FavAt)
		}
	}
}

func TestSetFav_解除するとFavの一覧から消える(t *testing.T) {
	// Given
	db := newFixtureDB(t)
	id := idOf(t, db, pathGirl)
	fav(t, db, id)

	// When
	if err := db.SetFav(context.Background(), id, false, favedAt); err != nil {
		t.Fatalf("SetFav(false) error = %v", err)
	}

	// Then
	if got := searchPaths(t, db, Query{Fav: true}); len(got) != 0 {
		t.Errorf("searchPaths(Fav) = %v, want 空", got)
	}
	img, err := db.Get(context.Background(), id)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if !img.FavAt.IsZero() {
		t.Errorf("FavAt = %v, want ゼロ値", img.FavAt)
	}
}

func TestSetFav_すでにFavなら最初の日時を保つ(t *testing.T) {
	// Given
	db := newFixtureDB(t)
	id := idOf(t, db, pathGirl)
	fav(t, db, id)

	// When: 別の日時でもう一度 Fav にする
	if err := db.SetFav(context.Background(), id, true, favedAt.Add(time.Hour)); err != nil {
		t.Fatalf("SetFav() error = %v", err)
	}

	// Then
	img, err := db.Get(context.Background(), id)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if !img.FavAt.Equal(favedAt) {
		t.Errorf("FavAt = %v, want %v", img.FavAt, favedAt)
	}
}

func TestSetFav_知らない画像はErrNotFound(t *testing.T) {
	// Given
	db := newFixtureDB(t)

	// When
	err := db.SetFav(context.Background(), 9999, true, favedAt)

	// Then
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("SetFav() error = %v, want ErrNotFound", err)
	}
}

func TestSetFav_ファセットはFavの絞り込みと件数が一致する(t *testing.T) {
	// Given: modelA の 2 枚のうち 1 枚と、modelB の 1 枚を Fav にする
	db := newFixtureDB(t)
	fav(t, db, idOf(t, db, pathGirl), idOf(t, db, pathBoy))
	ctx := context.Background()

	// When
	facets, err := db.Facets(ctx, Query{Fav: true})
	if err != nil {
		t.Fatalf("Facets() error = %v", err)
	}

	// Then: 候補ごとの件数が、その候補で絞り込んだ Fav の件数と食い違わない
	for _, tt := range []struct {
		name  string
		value string
		count int
		query Query
	}{
		{"モデル", "modelA", facetCount(facets.Models, "modelA"), Query{Fav: true, Models: []string{"modelA"}}},
		{"モデル", "modelB", facetCount(facets.Models, "modelB"), Query{Fav: true, Models: []string{"modelB"}}},
		{"LoRA", "char_a", facetCount(facets.Loras, "char_a"), Query{Fav: true, Loras: []string{"char_a"}}},
		{"LoRA", "style_v3", facetCount(facets.Loras, "style_v3"), Query{Fav: true, Loras: []string{"style_v3"}}},
		{"ルート", "out", facetCount(facets.Roots, "out"), Query{Fav: true, Roots: []string{"out"}}},
	} {
		res, err := db.Search(ctx, tt.query)
		if err != nil {
			t.Fatalf("Search() error = %v", err)
		}
		if tt.count != res.Total {
			t.Errorf("%s %s: ファセットの件数 = %d, 絞り込みの件数 = %d", tt.name, tt.value, tt.count, res.Total)
		}
	}
	if got := facetCount(facets.Models, "modelA"); got != 1 {
		t.Errorf("modelA の件数 = %d, want 1（Fav でない 1 枚は数えない）", got)
	}
	// Fav の画像だけが持たない LoRA は候補から消える。
	if got := facetCount(facets.Loras, "char_a"); got != 1 {
		t.Errorf("char_a の件数 = %d, want 1", got)
	}
}

func TestSetFav_ルートとFavの絞り込みを併用できる(t *testing.T) {
	// Given: 2 つのルートにそれぞれ Fav の画像がある
	db := newFixtureDB(t)
	other := &Image{Root: "other", Path: "a/b.png", CreatedAt: fixtureBase}
	put(t, db, other)
	fav(t, db, idOf(t, db, pathGirl), other.ID)

	// When
	got := searchPaths(t, db, Query{Fav: true, Roots: []string{"other"}})

	// Then
	if want := []string{"a/b.png"}; !slices.Equal(got, want) {
		t.Errorf("searchPaths(Fav, other) = %v, want %v", got, want)
	}
}

func TestPut_再スキャンしてもFavは残る(t *testing.T) {
	// Given
	db := newFixtureDB(t)
	id := idOf(t, db, pathGirl)
	fav(t, db, id)

	// When: 同じファイルを内容を変えて登録し直す
	again := fixtures()[0]
	again.Size = 9999
	again.Prompt = "1girl, smile, short hair"
	put(t, db, again)

	// Then
	if again.ID != id {
		t.Fatalf("ID = %d, want %d", again.ID, id)
	}
	if got := searchPaths(t, db, Query{Fav: true}); !slices.Equal(got, []string{pathGirl}) {
		t.Errorf("searchPaths(Fav) = %v, want [%s]", got, pathGirl)
	}
}

func TestMove_移動してもFavは残る(t *testing.T) {
	// Given
	db := newFixtureDB(t)
	id := idOf(t, db, pathGirl)
	fav(t, db, id)

	// When: 監視がファイルの移動を拾う
	const moved = "favorites/00001.png"
	if err := db.Move(context.Background(), "out", pathGirl, moved); err != nil {
		t.Fatalf("Move() error = %v", err)
	}

	// Then
	if got := searchPaths(t, db, Query{Fav: true}); !slices.Equal(got, []string{moved}) {
		t.Errorf("searchPaths(Fav) = %v, want [%s]", got, moved)
	}
}

func TestSetFav_ゴミ箱の画像はFavの一覧に出さず戻せばFavが残る(t *testing.T) {
	// Given
	db := newFixtureDB(t)
	id := idOf(t, db, pathGirl)
	fav(t, db, id)

	// When: ゴミ箱へ入れる
	trash(t, db, id, pathGirl)

	// Then: Fav の一覧にも Fav のファセットにも出ない
	if got := searchPaths(t, db, Query{Fav: true}); len(got) != 0 {
		t.Errorf("searchPaths(Fav) = %v, ゴミ箱の画像は出さない", got)
	}
	facets, err := db.Facets(context.Background(), Query{Fav: true})
	if err != nil {
		t.Fatalf("Facets() error = %v", err)
	}
	if got := facetCount(facets.Models, "modelA"); got != 0 {
		t.Errorf("modelA の件数 = %d, want 0", got)
	}

	// When: 元に戻す
	if err := db.Restore(context.Background(), id, pathGirl); err != nil {
		t.Fatalf("Restore() error = %v", err)
	}

	// Then: Fav のまま一覧へ戻る
	if got := searchPaths(t, db, Query{Fav: true}); !slices.Equal(got, []string{pathGirl}) {
		t.Errorf("searchPaths(Fav) = %v, want [%s]", got, pathGirl)
	}
}

func TestOpen_Favの列がないインデックスを開ける(t *testing.T) {
	// Given: Fav の列を持たない、古い形のインデックス
	dbPath := filepath.Join(t.TempDir(), "index.db")
	old, err := sql.Open("sqlite", "file:"+dbPath)
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	legacy := strings.NewReplacer(
		"    fav_at        INTEGER NOT NULL DEFAULT 0,\n", "",
	).Replace(schema)
	if legacy == schema {
		t.Fatal("スキーマから fav_at の行を取り除けなかった")
	}
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

	// Then: 既存の行は Fav でないものとして扱われ、Fav にもできる
	if got := searchPaths(t, db, Query{Fav: true}); len(got) != 0 {
		t.Errorf("searchPaths(Fav) = %v, want 空", got)
	}
	state, ok, err := db.State(context.Background(), "out", "a.png")
	if err != nil || !ok {
		t.Fatalf("State() = ok %v, err %v", ok, err)
	}
	fav(t, db, state.ID)
	if got := searchPaths(t, db, Query{Fav: true}); !slices.Equal(got, []string{"a.png"}) {
		t.Errorf("searchPaths(Fav) = %v, want [a.png]", got)
	}
}
