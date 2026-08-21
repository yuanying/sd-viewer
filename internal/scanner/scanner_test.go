package scanner

import (
	"bytes"
	"context"
	"encoding/binary"
	"hash/crc32"
	"os"
	"path/filepath"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yuanying/sd-viewer/internal/index"
)

const sampleParams = "1girl, smile\nNegative prompt: watermark\n" +
	"Steps: 28, Sampler: Euler a, CFG scale: 5, Seed: 1, Size: 896x1152, Model: modelA"

// pngBytes は parameters を埋め込んだ最小構成の PNG を作る。
func pngBytes(params string) []byte {
	var buf bytes.Buffer
	buf.Write([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'})

	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:], 896)
	binary.BigEndian.PutUint32(ihdr[4:], 1152)
	ihdr[8], ihdr[9] = 8, 2
	writeChunk(&buf, "IHDR", ihdr)

	if params != "" {
		writeChunk(&buf, "tEXt", append(append([]byte("parameters"), 0), params...))
	}
	writeChunk(&buf, "IDAT", bytes.Repeat([]byte{0}, 16))
	writeChunk(&buf, "IEND", nil)
	return buf.Bytes()
}

func writeChunk(buf *bytes.Buffer, typ string, data []byte) {
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(data)))
	buf.Write(length[:])
	body := append([]byte(typ), data...)
	buf.Write(body)
	var crc [4]byte
	binary.BigEndian.PutUint32(crc[:], crc32.ChecksumIEEE(body))
	buf.Write(crc[:])
}

func writeFile(t *testing.T, dir, rel string, content []byte) string {
	t.Helper()
	full := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, content, 0o644); err != nil {
		t.Fatal(err)
	}
	return full
}

func newTestIndex(t *testing.T) *index.DB {
	t.Helper()
	db, err := index.Open(filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatalf("index.Open() error = %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// indexedPaths はインデックスに登録されているパスをソートして返す。
func indexedPaths(t *testing.T, db *index.DB) []string {
	t.Helper()
	res, err := db.Search(context.Background(), index.Query{})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	paths := make([]string, 0, len(res.Images))
	for _, img := range res.Images {
		paths = append(paths, img.Path)
	}
	slices.Sort(paths)
	return paths
}

// waitFor は条件が満たされるまで短い間隔で待つ。
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestScan_ディレクトリを走査してインデックスへ登録する(t *testing.T) {
	type file struct {
		rel     string
		content []byte
	}
	tests := []struct {
		name  string
		files []file
		want  []string
	}{
		{
			name: "サブディレクトリの PNG も再帰的に登録する",
			files: []file{
				{"00001.png", pngBytes(sampleParams)},
				{"2026-08-13/00002.png", pngBytes(sampleParams)},
				{"2026-08-13/nested/00003.png", pngBytes(sampleParams)},
			},
			want: []string{"00001.png", "2026-08-13/00002.png", "2026-08-13/nested/00003.png"},
		},
		{
			name: "PNG 以外のファイルは登録しない",
			files: []file{
				{"00001.png", pngBytes(sampleParams)},
				{"note.txt", []byte("hello")},
				{"photo.jpg", []byte{0xff, 0xd8, 0xff}},
			},
			want: []string{"00001.png"},
		},
		{
			name: "拡張子の大文字小文字は問わない",
			files: []file{
				{"00001.PNG", pngBytes(sampleParams)},
			},
			want: []string{"00001.PNG"},
		},
		{
			name: "生成情報のない PNG も登録する",
			files: []file{
				{"manual.png", pngBytes("")},
			},
			want: []string{"manual.png"},
		},
		{
			name: "PNG として壊れたファイルは登録しない",
			files: []file{
				{"broken.png", []byte("not a png")},
				{"ok.png", pngBytes(sampleParams)},
			},
			want: []string{"ok.png"},
		},
		{
			name:  "ファイルがなければ何も登録しない",
			files: nil,
			want:  []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Given: ファイルを配置したディレクトリ
			dir := t.TempDir()
			for _, f := range tt.files {
				writeFile(t, dir, f.rel, f.content)
			}
			db := newTestIndex(t)
			s := newScanner(t, db, dir, nil)

			// When: フルスキャンする
			if err := s.Scan(context.Background()); err != nil {
				t.Fatalf("Scan() error = %v", err)
			}

			// Then: 期待したファイルだけが登録される
			if got := indexedPaths(t, db); !slices.Equal(got, tt.want) {
				t.Errorf("paths = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestScan_解析した内容をインデックスへ反映する(t *testing.T) {
	// Given: parameters を持つ PNG
	dir := t.TempDir()
	writeFile(t, dir, "2026-08-13/00001.png", pngBytes(sampleParams))
	db := newTestIndex(t)
	s := newScanner(t, db, dir, nil)

	// When: フルスキャンする
	if err := s.Scan(context.Background()); err != nil {
		t.Fatalf("Scan() error = %v", err)
	}

	// Then: メタデータが検索できる形で登録される
	res, err := db.Search(context.Background(), index.Query{Models: []string{"modelA"}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Total != 1 {
		t.Fatalf("Total = %d, want 1", res.Total)
	}
	got, err := db.Get(context.Background(), res.Images[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Root != "out" {
		t.Errorf("Root = %q, want out", got.Root)
	}
	if got.Prompt != "1girl, smile" || got.Sampler != "Euler a" || got.Steps != 28 {
		t.Errorf("params not indexed: %#v", got)
	}
	if got.Width != 896 || got.Height != 1152 {
		t.Errorf("size = %dx%d, want 896x1152", got.Width, got.Height)
	}
	if !slices.Equal(got.PositiveTags, []string{"1girl", "smile"}) {
		t.Errorf("PositiveTags = %v", got.PositiveTags)
	}
	if got.CreatedAt.IsZero() {
		t.Error("CreatedAt is zero")
	}
}

func TestScan_2回目以降は変更のあったファイルだけを解析する(t *testing.T) {
	tests := []struct {
		name        string
		change      func(t *testing.T, dir string)
		wantIndexed int
	}{
		{
			name:        "変更がなければ解析しない",
			change:      func(t *testing.T, dir string) {},
			wantIndexed: 0,
		},
		{
			name: "内容が変わったら解析し直す",
			change: func(t *testing.T, dir string) {
				writeFile(t, dir, "00001.png", pngBytes(sampleParams+", Version: 2"))
			},
			wantIndexed: 1,
		},
		{
			name: "新しいファイルは解析する",
			change: func(t *testing.T, dir string) {
				writeFile(t, dir, "00002.png", pngBytes(sampleParams))
			},
			wantIndexed: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Given: 一度スキャン済みのディレクトリ
			dir := t.TempDir()
			writeFile(t, dir, "00001.png", pngBytes(sampleParams))
			db := newTestIndex(t)

			var indexed atomic.Int32
			s := newScanner(t, db, dir, func(o *Options) {
				o.OnIndexed = func(context.Context, *index.Image, string) { indexed.Add(1) }
			})
			if err := s.Scan(context.Background()); err != nil {
				t.Fatalf("Scan() error = %v", err)
			}
			indexed.Store(0)

			// When: 変更を加えて再スキャンする
			tt.change(t, dir)
			if err := s.Scan(context.Background()); err != nil {
				t.Fatalf("Scan() error = %v", err)
			}

			// Then: 変更のあったファイルだけが解析される
			if got := int(indexed.Load()); got != tt.wantIndexed {
				t.Errorf("indexed = %d, want %d", got, tt.wantIndexed)
			}
		})
	}
}

func TestScan_停止中に消えたファイルをインデックスから取り除く(t *testing.T) {
	// Given: 2 枚をスキャン済みのディレクトリ
	dir := t.TempDir()
	writeFile(t, dir, "00001.png", pngBytes(sampleParams))
	writeFile(t, dir, "00002.png", pngBytes(sampleParams))
	db := newTestIndex(t)
	s := newScanner(t, db, dir, nil)
	if err := s.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}

	// When: 片方を消して再スキャンする
	if err := os.Remove(filepath.Join(dir, "00001.png")); err != nil {
		t.Fatal(err)
	}
	if err := s.Scan(context.Background()); err != nil {
		t.Fatalf("Scan() error = %v", err)
	}

	// Then: 消えたファイルはインデックスからも消える
	if got := indexedPaths(t, db); !slices.Equal(got, []string{"00002.png"}) {
		t.Errorf("paths = %v, want [00002.png]", got)
	}
}

func TestWatch_ファイルの変化を追いかける(t *testing.T) {
	tests := []struct {
		name string
		act  func(t *testing.T, dir string, db *index.DB)
		want []string
	}{
		{
			name: "新しく作られた画像を登録する",
			act: func(t *testing.T, dir string, _ *index.DB) {
				writeFile(t, dir, "created.png", pngBytes(sampleParams))
			},
			want: []string{"created.png", "existing.png"},
		},
		{
			name: "新しいディレクトリに作られた画像も登録する",
			act: func(t *testing.T, dir string, _ *index.DB) {
				writeFile(t, dir, "2026-08-14/created.png", pngBytes(sampleParams))
			},
			want: []string{"2026-08-14/created.png", "existing.png"},
		},
		{
			name: "削除された画像を取り除く",
			act: func(t *testing.T, dir string, _ *index.DB) {
				if err := os.Remove(filepath.Join(dir, "existing.png")); err != nil {
					t.Fatal(err)
				}
			},
			want: []string{},
		},
		{
			name: "移動された画像のパスを追いかける",
			act: func(t *testing.T, dir string, _ *index.DB) {
				if err := os.MkdirAll(filepath.Join(dir, "archive"), 0o755); err != nil {
					t.Fatal(err)
				}
				err := os.Rename(filepath.Join(dir, "existing.png"), filepath.Join(dir, "archive", "moved.png"))
				if err != nil {
					t.Fatal(err)
				}
			},
			want: []string{"archive/moved.png"},
		},
		{
			name: "ディレクトリごと削除された画像を取り除く",
			act: func(t *testing.T, dir string, db *index.DB) {
				writeFile(t, dir, "sub/inside.png", pngBytes(sampleParams))
				waitFor(t, "サブディレクトリの画像が登録されること", func() bool {
					return slices.Equal(indexedPaths(t, db), []string{"existing.png", "sub/inside.png"})
				})
				if err := os.RemoveAll(filepath.Join(dir, "sub")); err != nil {
					t.Fatal(err)
				}
			},
			want: []string{"existing.png"},
		},
		{
			name: "PNG 以外のファイルは無視する",
			act: func(t *testing.T, dir string, _ *index.DB) {
				writeFile(t, dir, "note.txt", []byte("hello"))
				writeFile(t, dir, "later.png", pngBytes(sampleParams))
			},
			want: []string{"existing.png", "later.png"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Given: 監視中のディレクトリ
			dir := t.TempDir()
			writeFile(t, dir, "existing.png", pngBytes(sampleParams))
			db := newTestIndex(t)
			s := newScanner(t, db, dir, nil)
			ctx := t.Context()
			startWatching(t, ctx, s)

			// When: ファイルを操作する
			tt.act(t, dir, db)

			// Then: インデックスが追従する
			waitFor(t, "インデックスの追従", func() bool {
				return slices.Equal(indexedPaths(t, db), tt.want)
			})
		})
	}
}

func TestWatch_移動しても同じ画像として扱う(t *testing.T) {
	// Given: 監視中のディレクトリと登録済みの画像
	dir := t.TempDir()
	writeFile(t, dir, "existing.png", pngBytes(sampleParams))
	db := newTestIndex(t)
	s := newScanner(t, db, dir, nil)
	ctx := t.Context()
	startWatching(t, ctx, s)

	before, err := db.Search(ctx, index.Query{})
	if err != nil || before.Total != 1 {
		t.Fatalf("setup failed: %v %#v", err, before)
	}
	wantID := before.Images[0].ID

	// When: 同じルート内で移動する
	if err := os.Rename(filepath.Join(dir, "existing.png"), filepath.Join(dir, "moved.png")); err != nil {
		t.Fatal(err)
	}

	// Then: ID を保ったままパスが変わる
	waitFor(t, "移動の反映", func() bool {
		return slices.Equal(indexedPaths(t, db), []string{"moved.png"})
	})
	after, err := db.Search(ctx, index.Query{})
	if err != nil {
		t.Fatal(err)
	}
	if after.Images[0].ID != wantID {
		t.Errorf("ID = %d, want %d (移動で ID が変わっている)", after.Images[0].ID, wantID)
	}
}

func TestWatch_書き込み中のファイルは完了してから解析する(t *testing.T) {
	// Given: 監視中のディレクトリ
	dir := t.TempDir()
	db := newTestIndex(t)
	s := newScanner(t, db, dir, nil)
	ctx := t.Context()
	startWatching(t, ctx, s)

	// When: ファイルを少しずつ書き込む
	full := filepath.Join(dir, "slow.png")
	f, err := os.Create(full)
	if err != nil {
		t.Fatal(err)
	}
	content := pngBytes(sampleParams)
	for _, chunk := range [][]byte{content[:8], content[8:40], content[40:]} {
		if _, err := f.Write(chunk); err != nil {
			t.Fatal(err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	// Then: 書き込み完了後の内容で登録される
	waitFor(t, "登録", func() bool {
		return slices.Equal(indexedPaths(t, db), []string{"slow.png"})
	})
	res, err := db.Search(ctx, index.Query{Models: []string{"modelA"}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Total != 1 {
		t.Errorf("Total = %d, want 1 (書き込み途中の内容で登録されている)", res.Total)
	}
}

// newScanner はテスト用の設定でスキャナを作る。
func newScanner(t *testing.T, db *index.DB, dir string, mod func(*Options)) *Scanner {
	t.Helper()
	opts := Options{
		Roots:    []Root{{Name: "out", Path: dir}},
		Debounce: 50 * time.Millisecond,
	}
	if mod != nil {
		mod(&opts)
	}
	s, err := New(db, opts)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// startWatching は初回スキャンを済ませてから監視を開始する。
func startWatching(t *testing.T, ctx context.Context, s *Scanner) {
	t.Helper()
	if err := s.Scan(ctx); err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- s.Watch(ctx) }()
	t.Cleanup(func() {
		select {
		case err := <-done:
			if err != nil && ctx.Err() == nil {
				t.Errorf("Watch() error = %v", err)
			}
		case <-time.After(time.Second):
		}
	})
	// 監視が始まるまで待つ。
	waitFor(t, "監視の開始", s.Watching)
}

func TestScan_ゴミ箱の中は走査しない(t *testing.T) {
	// Given: ゴミ箱の中と外にそれぞれ画像がある
	dir := t.TempDir()
	writeFile(t, dir, "keep.png", pngBytes(sampleParams))
	writeFile(t, dir, ".trash/2026-08-13/dropped.png", pngBytes(sampleParams))
	db := newTestIndex(t)
	s := newScanner(t, db, dir, nil)

	// When
	if err := s.Scan(t.Context()); err != nil {
		t.Fatalf("Scan() error = %v", err)
	}

	// Then
	if got := indexedPaths(t, db); !slices.Equal(got, []string{"keep.png"}) {
		t.Errorf("indexedPaths() = %v, want [keep.png]", got)
	}
}

func TestScan_ゴミ箱の中の画像を消したことにしない(t *testing.T) {
	// Given: ゴミ箱へ入れた 1 枚と、ゴミ箱の外の 1 枚
	dir := t.TempDir()
	writeFile(t, dir, "keep.png", pngBytes(sampleParams))
	writeFile(t, dir, "dropped.png", pngBytes(sampleParams))
	db := newTestIndex(t)
	s := newScanner(t, db, dir, nil)
	ctx := t.Context()
	if err := s.Scan(ctx); err != nil {
		t.Fatalf("Scan() error = %v", err)
	}

	dropped, ok, err := db.State(ctx, "out", "dropped.png")
	if err != nil || !ok {
		t.Fatalf("State() = ok %v, err %v", ok, err)
	}
	if err := db.Trash(ctx, dropped.ID, ".trash/dropped.png", time.Now()); err != nil {
		t.Fatalf("Trash() error = %v", err)
	}
	if err := os.Rename(filepath.Join(dir, "dropped.png"), writePath(t, dir, ".trash/dropped.png")); err != nil {
		t.Fatal(err)
	}

	// When: 停止中の変更を回収するつもりで走査し直す
	if err := s.Scan(ctx); err != nil {
		t.Fatalf("Scan() error = %v", err)
	}

	// Then: ゴミ箱の行は残り、一覧には出ない
	if got := indexedPaths(t, db); !slices.Equal(got, []string{"keep.png"}) {
		t.Errorf("indexedPaths() = %v, want [keep.png]", got)
	}
	if _, err := db.Get(ctx, dropped.ID); err != nil {
		t.Errorf("Get() error = %v, ゴミ箱の行が消えている", err)
	}
}

func TestWatch_ゴミ箱の中の変化は追いかけない(t *testing.T) {
	// Given: 監視中のディレクトリ
	dir := t.TempDir()
	writeFile(t, dir, "keep.png", pngBytes(sampleParams))
	db := newTestIndex(t)
	s := newScanner(t, db, dir, nil)
	ctx := t.Context()
	startWatching(t, ctx, s)

	// When: ゴミ箱の中にディレクトリごとファイルが現れる
	writeFile(t, dir, ".trash/2026-08-13/dropped.png", pngBytes(sampleParams))

	// Then: 見張っている keep.png の更新が反映されるまで待ってから確かめる
	writeFile(t, dir, "later.png", pngBytes(sampleParams))
	waitFor(t, "ゴミ箱の外の登録", func() bool {
		return slices.Contains(indexedPaths(t, db), "later.png")
	})
	if got := indexedPaths(t, db); !slices.Equal(got, []string{"keep.png", "later.png"}) {
		t.Errorf("indexedPaths() = %v, ゴミ箱の中まで登録している", got)
	}
}

// writePath は書き込み先のディレクトリを用意して絶対パスを返す。
func writePath(t *testing.T, dir, rel string) string {
	t.Helper()
	full := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	return full
}
