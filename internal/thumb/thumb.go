// Package thumb は一覧表示用のサムネイルを生成し、ディスクへ蓄える。
package thumb

import (
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"time"

	"golang.org/x/image/draw"
)

// defaultQuality は JPEG の品質。一覧で見るぶんには十分で、容量も抑えられる。
const defaultQuality = 82

// shards はサムネイルを分散させるサブディレクトリの数。
// 1 つのディレクトリに数万ファイルを置かないようにする。
const shards = 64

// Cache はサムネイルの置き場所を管理する。
type Cache struct {
	dir     string
	size    int
	quality int

	generated atomic.Int64
}

// New はキャッシュディレクトリを用意する。size はサムネイルの長辺。
func New(dir string, size int) (*Cache, error) {
	if size <= 0 {
		return nil, errors.New("thumb: size must be positive")
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("thumb: resolve cache dir: %w", err)
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return nil, fmt.Errorf("thumb: create cache dir: %w", err)
	}
	return &Cache{dir: abs, size: size, quality: defaultQuality}, nil
}

// Dir はキャッシュディレクトリを返す。
func (c *Cache) Dir() string {
	return c.dir
}

// Size はサムネイルの長辺を返す。
func (c *Cache) Size() int {
	return c.size
}

// Path は画像 ID に対応するサムネイルの置き場所を返す。
func (c *Cache) Path(id int64) string {
	shard := strconv.FormatInt(id%shards, 16)
	return filepath.Join(c.dir, shard, strconv.FormatInt(id, 10)+".jpg")
}

// Generated はこれまでにサムネイルを生成した回数を返す。
func (c *Cache) Generated() int64 {
	return c.generated.Load()
}

// Ensure はサムネイルがなければ生成し、その置き場所を返す。
//
// サムネイルの更新日時には元画像の更新日時を写す。両者が一致していれば
// 最新とみなして作り直さない。
func (c *Cache) Ensure(id int64, srcPath string) (string, error) {
	dst := c.Path(id)

	src, err := os.Stat(srcPath)
	if err != nil {
		return "", fmt.Errorf("thumb: stat source: %w", err)
	}
	if cached, err := os.Stat(dst); err == nil && cached.ModTime().Equal(src.ModTime()) {
		return dst, nil
	}
	if err := c.generate(dst, srcPath, src.ModTime()); err != nil {
		return "", err
	}
	c.generated.Add(1)
	return dst, nil
}

// Remove はサムネイルを取り除く。もともとなければ何もしない。
func (c *Cache) Remove(id int64) error {
	if err := os.Remove(c.Path(id)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("thumb: remove thumbnail: %w", err)
	}
	return nil
}

// generate は元画像を読み込んで縮小し、サムネイルを書き出す。
// 書き出しは一時ファイル経由で行い、途中で失敗しても壊れたファイルを残さない。
func (c *Cache) generate(dst, srcPath string, srcModTime time.Time) error {
	src, err := os.Open(srcPath)
	if err != nil {
		return fmt.Errorf("thumb: open source: %w", err)
	}
	defer src.Close()

	img, err := png.Decode(src)
	if err != nil {
		return fmt.Errorf("thumb: decode %s: %w", srcPath, err)
	}

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("thumb: create shard dir: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".tmp-*")
	if err != nil {
		return fmt.Errorf("thumb: create temp file: %w", err)
	}
	defer os.Remove(tmp.Name())

	if err := jpeg.Encode(tmp, c.resize(img), &jpeg.Options{Quality: c.quality}); err != nil {
		tmp.Close()
		return fmt.Errorf("thumb: encode thumbnail: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("thumb: close temp file: %w", err)
	}
	if err := os.Chtimes(tmp.Name(), srcModTime, srcModTime); err != nil {
		return fmt.Errorf("thumb: stamp thumbnail: %w", err)
	}
	if err := os.Rename(tmp.Name(), dst); err != nil {
		return fmt.Errorf("thumb: place thumbnail: %w", err)
	}
	return nil
}

// resize は縦横比を保ったまま長辺を size に収める。元画像が小さければそのまま返す。
func (c *Cache) resize(img image.Image) image.Image {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= c.size && h <= c.size {
		return img
	}

	scale := float64(c.size) / float64(max(w, h))
	dstW := max(1, int(float64(w)*scale))
	dstH := max(1, int(float64(h)*scale))

	dst := image.NewRGBA(image.Rect(0, 0, dstW, dstH))
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, b, draw.Src, nil)
	return dst
}
