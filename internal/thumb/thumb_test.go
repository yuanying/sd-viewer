package thumb

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeImage は指定サイズの PNG を書き出す。
func writeImage(t *testing.T, path string, width, height int) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			img.Set(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: 128, A: 255})
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}

func newCache(t *testing.T, size int) *Cache {
	t.Helper()
	c, err := New(filepath.Join(t.TempDir(), "thumbs"), size)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	return c
}

// decodeSize はサムネイルの寸法を返す。
func decodeSize(t *testing.T, path string) (int, int) {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := jpeg.DecodeConfig(bytes.NewReader(b))
	if err != nil {
		t.Fatalf("decode thumbnail: %v", err)
	}
	return cfg.Width, cfg.Height
}

func TestEnsure_長辺を指定した大きさに収めたサムネイルを作る(t *testing.T) {
	tests := []struct {
		name             string
		size             int
		srcW, srcH       int
		wantW, wantH     int
		wantAspectSource bool
	}{
		{
			name: "縦長の画像は高さを基準に縮小する",
			size: 256, srcW: 512, srcH: 1024,
			wantW: 128, wantH: 256,
		},
		{
			name: "横長の画像は幅を基準に縮小する",
			size: 256, srcW: 1024, srcH: 512,
			wantW: 256, wantH: 128,
		},
		{
			name: "正方形の画像は指定した大きさになる",
			size: 200, srcW: 800, srcH: 800,
			wantW: 200, wantH: 200,
		},
		{
			name: "指定より小さい画像は拡大しない",
			size: 512, srcW: 256, srcH: 128,
			wantW: 256, wantH: 128,
		},
		{
			name: "極端に細長い画像でも 1 ピクセル以上を保つ",
			size: 64, srcW: 1000, srcH: 4,
			wantW: 64, wantH: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Given: 元画像とサムネイルキャッシュ
			src := filepath.Join(t.TempDir(), "src.png")
			writeImage(t, src, tt.srcW, tt.srcH)
			c := newCache(t, tt.size)

			// When: サムネイルを用意する
			path, err := c.Ensure(1, src)
			if err != nil {
				t.Fatalf("Ensure() error = %v", err)
			}

			// Then: 長辺が指定の大きさに収まり、縦横比が保たれる
			gotW, gotH := decodeSize(t, path)
			if gotW != tt.wantW || gotH != tt.wantH {
				t.Errorf("size = %dx%d, want %dx%d", gotW, gotH, tt.wantW, tt.wantH)
			}
		})
	}
}

func TestEnsure_再生成の要否を判断する(t *testing.T) {
	tests := []struct {
		name        string
		change      func(t *testing.T, src string)
		removeThumb bool
		wantRebuilt bool
	}{
		{
			name:        "元画像が変わっていなければ作り直さない",
			change:      func(t *testing.T, src string) {},
			wantRebuilt: false,
		},
		{
			name: "元画像が新しくなっていれば作り直す",
			change: func(t *testing.T, src string) {
				writeImage(t, src, 400, 200)
				later := time.Now().Add(time.Hour)
				if err := os.Chtimes(src, later, later); err != nil {
					t.Fatal(err)
				}
			},
			wantRebuilt: true,
		},
		{
			name:        "サムネイルが消えていれば作り直す",
			change:      func(t *testing.T, src string) {},
			removeThumb: true,
			wantRebuilt: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Given: 一度サムネイルを作った状態
			src := filepath.Join(t.TempDir(), "src.png")
			writeImage(t, src, 800, 400)
			c := newCache(t, 256)
			path, err := c.Ensure(1, src)
			if err != nil {
				t.Fatalf("Ensure() error = %v", err)
			}
			if tt.removeThumb {
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			}
			before := c.Generated()

			// When: 変更を加えてから再びサムネイルを用意する
			tt.change(t, src)
			if _, err := c.Ensure(1, src); err != nil {
				t.Fatalf("Ensure() error = %v", err)
			}

			// Then: 必要なときだけ作り直される
			if _, err := os.Stat(path); err != nil {
				t.Fatalf("thumbnail is missing: %v", err)
			}
			if rebuilt := c.Generated() > before; rebuilt != tt.wantRebuilt {
				t.Errorf("rebuilt = %v, want %v", rebuilt, tt.wantRebuilt)
			}
		})
	}
}

func TestEnsure_解析できない画像はエラーを返す(t *testing.T) {
	tests := []struct {
		name    string
		content []byte
	}{
		{name: "PNG ではないファイル", content: []byte("not an image")},
		{name: "途中で切れた PNG", content: []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Given: 壊れた画像ファイル
			src := filepath.Join(t.TempDir(), "broken.png")
			if err := os.WriteFile(src, tt.content, 0o644); err != nil {
				t.Fatal(err)
			}
			c := newCache(t, 256)

			// When: サムネイルを用意する
			_, err := c.Ensure(1, src)

			// Then: エラーになり、中途半端なファイルを残さない
			if err == nil {
				t.Fatal("error = nil, want non-nil")
			}
			if _, statErr := os.Stat(c.Path(1)); statErr == nil {
				t.Error("broken thumbnail was left behind")
			}
		})
	}
}

func TestEnsure_存在しない元画像はエラーを返す(t *testing.T) {
	// Given: 元画像のないパス
	c := newCache(t, 256)

	// When: サムネイルを用意する
	_, err := c.Ensure(1, filepath.Join(t.TempDir(), "missing.png"))

	// Then: エラーになる
	if err == nil {
		t.Error("error = nil, want non-nil")
	}
}

func TestRemove_サムネイルを削除する(t *testing.T) {
	tests := []struct {
		name    string
		prepare bool
	}{
		{name: "存在するサムネイルを消す", prepare: true},
		{name: "存在しないサムネイルの削除はエラーにしない", prepare: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Given: サムネイルの有無が異なる状態
			c := newCache(t, 256)
			if tt.prepare {
				src := filepath.Join(t.TempDir(), "src.png")
				writeImage(t, src, 400, 400)
				if _, err := c.Ensure(7, src); err != nil {
					t.Fatal(err)
				}
			}

			// When: 削除する
			err := c.Remove(7)

			// Then: エラーにならず、ファイルも残らない
			if err != nil {
				t.Errorf("Remove() error = %v", err)
			}
			if _, statErr := os.Stat(c.Path(7)); statErr == nil {
				t.Error("thumbnail still exists")
			}
		})
	}
}

func TestPath_IDごとに異なる場所へ振り分ける(t *testing.T) {
	// Given: サムネイルキャッシュ
	c := newCache(t, 256)

	// When: 異なる ID のパスを求める
	// Then: 重複せず、キャッシュディレクトリの下に収まる
	seen := map[string]bool{}
	for id := int64(1); id <= 1000; id++ {
		p := c.Path(id)
		if seen[p] {
			t.Fatalf("duplicated path for id %d: %s", id, p)
		}
		seen[p] = true
		if rel, err := filepath.Rel(c.Dir(), p); err != nil || rel == ".." {
			t.Fatalf("path %s is outside of %s", p, c.Dir())
		}
	}
}
