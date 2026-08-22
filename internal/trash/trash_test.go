package trash

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/yuanying/sd-viewer/internal/index"
)

var now = time.Date(2026, 8, 21, 10, 0, 0, 0, time.UTC)

// env はゴミ箱とその置き場所をまとめたテスト環境。
type env struct {
	bin    *Bin
	db     *index.DB
	roots  map[string]string
	purged []int64
}

// newEnv は out と other の 2 つのルートを持つテスト環境を作る。
func newEnv(t *testing.T) *env {
	t.Helper()

	db, err := index.Open(filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatalf("index.Open() error = %v", err)
	}
	t.Cleanup(func() { db.Close() })

	e := &env{
		db:    db,
		roots: map[string]string{"out": t.TempDir(), "other": t.TempDir()},
	}
	e.bin = New(Options{
		DB:       db,
		Roots:    e.roots,
		Now:      func() time.Time { return now },
		OnPurged: func(id int64) { e.purged = append(e.purged, id) },
	})
	return e
}

// add はルートへ実ファイルを置き、インデックスへも登録して ID を返す。
func (e *env) add(t *testing.T, root, rel string) int64 {
	t.Helper()
	e.write(t, filepath.Join(e.roots[root], filepath.FromSlash(rel)), rel)

	img := &index.Image{Root: root, Path: rel, CreatedAt: now, Size: int64(len(rel))}
	if err := e.db.Put(context.Background(), img); err != nil {
		t.Fatalf("Put(%s) error = %v", rel, err)
	}
	return img.ID
}

func (e *env) write(t *testing.T, abs, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// abs はルート相対のパスを絶対パスへ直す。
func (e *env) abs(root, rel string) string {
	return filepath.Join(e.roots[root], filepath.FromSlash(rel))
}

func (e *env) exists(root, rel string) bool {
	_, err := os.Stat(e.abs(root, rel))
	return err == nil
}

// content はファイルの中身を返す。読めなければ空文字。
func (e *env) content(t *testing.T, root, rel string) string {
	t.Helper()
	b, err := os.ReadFile(e.abs(root, rel))
	if err != nil {
		return ""
	}
	return string(b)
}

func (e *env) get(t *testing.T, id int64) *index.Image {
	t.Helper()
	img, err := e.db.Get(context.Background(), id)
	if err != nil {
		t.Fatalf("Get(%d) error = %v", id, err)
	}
	return img
}

const sample = "txt2img/2026-08-13/00001.png"

func TestMove_ルート直下のゴミ箱へファイルごと移す(t *testing.T) {
	// Given
	e := newEnv(t)
	id := e.add(t, "out", sample)

	// When
	res := e.bin.Move(context.Background(), []int64{id})

	// Then: 実ファイルが .trash へ移り、元の場所からは消える
	if res.Done != 1 || len(res.Failed) != 0 {
		t.Fatalf("Move() = %+v, want 1 件成功", res)
	}
	if e.exists("out", sample) {
		t.Error("元の場所にファイルが残っている")
	}
	if got := e.content(t, "out", ".trash/"+sample); got != sample {
		t.Errorf("ゴミ箱の中身 = %q, want %q", got, sample)
	}

	// Then: インデックスもゴミ箱の中を指す
	img := e.get(t, id)
	if img.Path != ".trash/"+sample {
		t.Errorf("Path = %q, want %q", img.Path, ".trash/"+sample)
	}
	if img.OrigPath != sample {
		t.Errorf("OrigPath = %q, want %q", img.OrigPath, sample)
	}
	if !img.TrashedAt.Equal(now) {
		t.Errorf("TrashedAt = %v, want %v", img.TrashedAt, now)
	}
}

func TestMove_ゴミ箱に同じパスがあれば連番を付ける(t *testing.T) {
	// Given: 1 枚をゴミ箱へ入れたあと、同じパスへ別の画像が現れる
	e := newEnv(t)
	first := e.add(t, "out", sample)
	e.bin.Move(context.Background(), []int64{first})
	second := e.add(t, "out", sample)

	// When
	res := e.bin.Move(context.Background(), []int64{second})

	// Then: 先にあるファイルを上書きしない
	if res.Done != 1 {
		t.Fatalf("Move() = %+v, want 1 件成功", res)
	}
	want := ".trash/txt2img/2026-08-13/00001 (2).png"
	if !e.exists("out", want) {
		t.Errorf("%s が作られていない", want)
	}
	if !e.exists("out", ".trash/"+sample) {
		t.Error("先にゴミ箱へ入れたファイルが消えている")
	}
	if got := e.get(t, second).Path; got != want {
		t.Errorf("Path = %q, want %q", got, want)
	}
	if got := e.get(t, second).OrigPath; got != sample {
		t.Errorf("OrigPath = %q, want %q", got, sample)
	}
}

func TestMove_すでにゴミ箱にある画像は断る(t *testing.T) {
	// Given
	e := newEnv(t)
	id := e.add(t, "out", sample)
	e.bin.Move(context.Background(), []int64{id})

	// When
	res := e.bin.Move(context.Background(), []int64{id})

	// Then
	if res.Done != 0 || len(res.Failed) != 1 || res.Failed[0].ID != id {
		t.Fatalf("Move() = %+v, want 1 件失敗", res)
	}
}

func TestMove_実ファイルがなければゴミ箱へ入れない(t *testing.T) {
	// Given: インデックスにはあるが実体のない画像
	e := newEnv(t)
	id := e.add(t, "out", sample)
	if err := os.Remove(e.abs("out", sample)); err != nil {
		t.Fatal(err)
	}

	// When
	res := e.bin.Move(context.Background(), []int64{id})

	// Then: 失敗を返し、インデックスはゴミ箱の外のままにしておく
	if res.Done != 0 || len(res.Failed) != 1 {
		t.Fatalf("Move() = %+v, want 1 件失敗", res)
	}
	img := e.get(t, id)
	if !img.TrashedAt.IsZero() {
		t.Errorf("TrashedAt = %v, want ゼロ値", img.TrashedAt)
	}
	if img.Path != sample {
		t.Errorf("Path = %q, want %q", img.Path, sample)
	}
}

func TestMove_知らないルートの画像は断る(t *testing.T) {
	// Given: --dir から外されたルートの画像
	e := newEnv(t)
	img := &index.Image{Root: "gone", Path: sample, CreatedAt: now}
	if err := e.db.Put(context.Background(), img); err != nil {
		t.Fatal(err)
	}

	// When
	res := e.bin.Move(context.Background(), []int64{img.ID})

	// Then
	if res.Done != 0 || len(res.Failed) != 1 {
		t.Fatalf("Move() = %+v, want 1 件失敗", res)
	}
	if res.Failed[0].Reason == "" {
		t.Error("理由が空になっている")
	}
}

func TestMove_一部が失敗しても残りは進める(t *testing.T) {
	// Given: 1 枚は実体があり、もう 1 枚はない
	e := newEnv(t)
	ok := e.add(t, "out", sample)
	ng := e.add(t, "out", "txt2img/2026-08-13/00002.png")
	if err := os.Remove(e.abs("out", "txt2img/2026-08-13/00002.png")); err != nil {
		t.Fatal(err)
	}

	// When
	res := e.bin.Move(context.Background(), []int64{ok, ng})

	// Then
	if res.Done != 1 || len(res.Failed) != 1 {
		t.Fatalf("Move() = %+v, want 1 件成功・1 件失敗", res)
	}
	if res.Failed[0].ID != ng {
		t.Errorf("失敗した ID = %d, want %d", res.Failed[0].ID, ng)
	}
	if !e.exists("out", ".trash/"+sample) {
		t.Error("成功したはずの画像がゴミ箱にない")
	}
}

func TestRestore_元のパスへ戻す(t *testing.T) {
	// Given
	e := newEnv(t)
	id := e.add(t, "out", sample)
	e.bin.Move(context.Background(), []int64{id})

	// When
	res := e.bin.Restore(context.Background(), []int64{id})

	// Then
	if res.Done != 1 || len(res.Failed) != 0 {
		t.Fatalf("Restore() = %+v, want 1 件成功", res)
	}
	if got := e.content(t, "out", sample); got != sample {
		t.Errorf("戻したファイルの中身 = %q, want %q", got, sample)
	}
	if e.exists("out", ".trash/"+sample) {
		t.Error("ゴミ箱にファイルが残っている")
	}
	img := e.get(t, id)
	if !img.TrashedAt.IsZero() || img.Path != sample {
		t.Errorf("Get() = %+v, ゴミ箱の外へ戻っていない", img)
	}
}

func TestRestore_戻し先が埋まっていれば別名で戻す(t *testing.T) {
	// Given: ゴミ箱へ入れたあと、同じパスへ別の画像が現れる
	e := newEnv(t)
	id := e.add(t, "out", sample)
	e.bin.Move(context.Background(), []int64{id})
	e.write(t, e.abs("out", sample), "newcomer")

	// When
	res := e.bin.Restore(context.Background(), []int64{id})

	// Then: 先にある画像を上書きしない
	if res.Done != 1 {
		t.Fatalf("Restore() = %+v, want 1 件成功", res)
	}
	if got := e.content(t, "out", sample); got != "newcomer" {
		t.Errorf("元からあったファイルの中身 = %q, 上書きされている", got)
	}
	want := "txt2img/2026-08-13/00001 (2).png"
	if got := e.content(t, "out", want); got != sample {
		t.Errorf("%s の中身 = %q, want %q", want, got, sample)
	}
	if got := e.get(t, id).Path; got != want {
		t.Errorf("Path = %q, want %q", got, want)
	}
}

func TestRestore_ゴミ箱にない画像は断る(t *testing.T) {
	// Given
	e := newEnv(t)
	id := e.add(t, "out", sample)

	// When
	res := e.bin.Restore(context.Background(), []int64{id})

	// Then
	if res.Done != 0 || len(res.Failed) != 1 {
		t.Fatalf("Restore() = %+v, want 1 件失敗", res)
	}
}

func TestPurge_ファイルも行もサムネイルも消す(t *testing.T) {
	// Given
	e := newEnv(t)
	id := e.add(t, "out", sample)
	e.bin.Move(context.Background(), []int64{id})

	// When
	res := e.bin.Purge(context.Background(), []int64{id})

	// Then
	if res.Done != 1 || len(res.Failed) != 0 {
		t.Fatalf("Purge() = %+v, want 1 件成功", res)
	}
	if e.exists("out", ".trash/"+sample) {
		t.Error("ファイルが残っている")
	}
	if _, err := e.db.Get(context.Background(), id); err == nil {
		t.Error("インデックスに行が残っている")
	}
	if !slices.Equal(e.purged, []int64{id}) {
		t.Errorf("OnPurged に渡った ID = %v, want [%d]", e.purged, id)
	}
}

func TestPurge_空になったディレクトリを片付ける(t *testing.T) {
	// Given
	e := newEnv(t)
	id := e.add(t, "out", sample)
	e.bin.Move(context.Background(), []int64{id})

	// When
	e.bin.Purge(context.Background(), []int64{id})

	// Then: ゴミ箱の中の空ディレクトリは残さない。ゴミ箱そのものは残す
	if e.exists("out", ".trash/txt2img") {
		t.Error("空になったディレクトリが残っている")
	}
	if !e.exists("out", ".trash") {
		t.Error("ゴミ箱のディレクトリまで消している")
	}
}

func TestPurge_ゴミ箱にない画像は断る(t *testing.T) {
	// Given
	e := newEnv(t)
	id := e.add(t, "out", sample)

	// When
	res := e.bin.Purge(context.Background(), []int64{id})

	// Then: 行もファイルも残す
	if res.Done != 0 || len(res.Failed) != 1 {
		t.Fatalf("Purge() = %+v, want 1 件失敗", res)
	}
	if !e.exists("out", sample) {
		t.Error("ゴミ箱の外のファイルを消している")
	}
}

func TestEmpty_指定したルートのゴミ箱だけを空にする(t *testing.T) {
	// Given: 2 つのルートそれぞれにゴミ箱の中身がある
	e := newEnv(t)
	a := e.add(t, "out", sample)
	b := e.add(t, "other", "a/b.png")
	e.bin.Move(context.Background(), []int64{a, b})

	// When
	res, err := e.bin.Empty(context.Background(), "out")
	if err != nil {
		t.Fatalf("Empty() error = %v", err)
	}

	// Then
	if res.Done != 1 || len(res.Failed) != 0 {
		t.Fatalf("Empty() = %+v, want 1 件成功", res)
	}
	if _, err := e.db.Get(context.Background(), a); err == nil {
		t.Error("out のゴミ箱が空になっていない")
	}
	if _, err := e.db.Get(context.Background(), b); err != nil {
		t.Errorf("other のゴミ箱まで空にしている: %v", err)
	}
}

func TestEmpty_ルートを省くとすべてのゴミ箱を空にする(t *testing.T) {
	// Given
	e := newEnv(t)
	a := e.add(t, "out", sample)
	b := e.add(t, "other", "a/b.png")
	e.bin.Move(context.Background(), []int64{a, b})

	// When
	res, err := e.bin.Empty(context.Background(), "")
	if err != nil {
		t.Fatalf("Empty() error = %v", err)
	}

	// Then
	if res.Done != 2 {
		t.Fatalf("Empty() = %+v, want 2 件成功", res)
	}
	counts, err := e.db.TrashCounts(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(counts) != 0 {
		t.Errorf("TrashCounts() = %v, want 空", counts)
	}
}

func TestIsTrashPath_ルート直下のゴミ箱だけを見分ける(t *testing.T) {
	tests := []struct {
		rel  string
		want bool
	}{
		{".trash", true},
		{".trash/a/b.png", true},
		{"txt2img/2026-08-13/00001.png", false},
		{"", false},
		{"a/.trash/b.png", false},
		{".trashcan/b.png", false},
	}
	for _, tt := range tests {
		if got := IsTrashPath(tt.rel); got != tt.want {
			t.Errorf("IsTrashPath(%q) = %v, want %v", tt.rel, got, tt.want)
		}
	}
}
