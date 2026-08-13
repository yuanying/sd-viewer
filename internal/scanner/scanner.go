// Package scanner は出力ディレクトリを走査し、ファイルの変化を
// インデックスへ反映する。
package scanner

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/yuanying/sd-viewer/internal/index"
	"github.com/yuanying/sd-viewer/internal/metadata"
)

// Root は監視対象のディレクトリ。Name は検索条件やファセットで使う識別子。
type Root struct {
	Name string
	Path string
}

// Options はスキャナの設定。
type Options struct {
	// Roots は監視対象のディレクトリ。1 つ以上必要。
	Roots []Root
	// Debounce はファイルイベントをまとめる時間。書き込み中のファイルを
	// 読まないために置く。既定は 500ms。
	Debounce time.Duration
	// Workers は解析の並列度。既定は CPU 数。
	Workers int
	// OnIndexed は画像を登録した直後に呼ばれる。サムネイル生成に使う。
	OnIndexed func(ctx context.Context, img *index.Image, absPath string)
	// OnRemoved は画像をインデックスから取り除いた直後に呼ばれる。
	OnRemoved func(ctx context.Context, id int64)
	Logger    *slog.Logger
}

// Stats はスキャンの進捗。
type Stats struct {
	Scanning bool  `json:"scanning"`
	Walked   int64 `json:"walked"`
	Indexed  int64 `json:"indexed"`
	Removed  int64 `json:"removed"`
	Failed   int64 `json:"failed"`
}

// Scanner は出力ディレクトリとインデックスを同期させる。
type Scanner struct {
	db        *index.DB
	roots     []Root
	debounce  time.Duration
	workers   int
	onIndexed func(context.Context, *index.Image, string)
	onRemoved func(context.Context, int64)
	log       *slog.Logger

	watcher  *fsnotify.Watcher
	watching atomic.Bool

	scanning atomic.Bool
	walked   atomic.Int64
	indexed  atomic.Int64
	removed  atomic.Int64
	failed   atomic.Int64
}

// New はスキャナを作る。Roots のパスは絶対パスへ正規化する。
func New(db *index.DB, opts Options) (*Scanner, error) {
	if len(opts.Roots) == 0 {
		return nil, errors.New("scanner: at least one root is required")
	}
	roots := make([]Root, 0, len(opts.Roots))
	for _, r := range opts.Roots {
		abs, err := filepath.Abs(r.Path)
		if err != nil {
			return nil, fmt.Errorf("scanner: resolve root %s: %w", r.Path, err)
		}
		if r.Name == "" {
			r.Name = filepath.Base(abs)
		}
		roots = append(roots, Root{Name: r.Name, Path: abs})
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("scanner: create watcher: %w", err)
	}

	s := &Scanner{
		db:        db,
		roots:     roots,
		debounce:  cmpOr(opts.Debounce, 500*time.Millisecond),
		workers:   cmpOr(opts.Workers, runtime.NumCPU()),
		onIndexed: opts.OnIndexed,
		onRemoved: opts.OnRemoved,
		log:       cmpOr(opts.Logger, slog.Default()),
		watcher:   watcher,
	}
	return s, nil
}

// Close は監視を終了し、資源を解放する。
func (s *Scanner) Close() error {
	return s.watcher.Close()
}

// Roots は監視対象のディレクトリを返す。
func (s *Scanner) Roots() []Root {
	return s.roots
}

// Stats は現在の進捗を返す。
func (s *Scanner) Stats() Stats {
	return Stats{
		Scanning: s.scanning.Load(),
		Walked:   s.walked.Load(),
		Indexed:  s.indexed.Load(),
		Removed:  s.removed.Load(),
		Failed:   s.failed.Load(),
	}
}

// Watching は監視が始まっているかを返す。
func (s *Scanner) Watching() bool {
	return s.watching.Load()
}

// Run は初回のフルスキャンを行ってから監視を続ける。
func (s *Scanner) Run(ctx context.Context) error {
	if err := s.Scan(ctx); err != nil {
		return err
	}
	return s.Watch(ctx)
}

// Scan はすべてのルートを走査し、インデックスとの差分を埋める。
// 前回のスキャン以降に消えたファイルはインデックスからも取り除く。
func (s *Scanner) Scan(ctx context.Context) error {
	s.scanning.Store(true)
	defer s.scanning.Store(false)

	for _, root := range s.roots {
		if err := s.scanRoot(ctx, root); err != nil {
			return err
		}
	}
	return nil
}

func (s *Scanner) scanRoot(ctx context.Context, root Root) error {
	known, err := s.db.States(ctx, root.Name)
	if err != nil {
		return err
	}

	jobs := make(chan job)
	var wg sync.WaitGroup
	for range s.workers {
		wg.Go(func() {
			for j := range jobs {
				s.indexFile(ctx, j)
			}
		})
	}

	seen := make(map[string]bool, len(known))
	walkErr := filepath.WalkDir(root.Path, func(abs string, d fs.DirEntry, err error) error {
		if err != nil {
			// 読めないディレクトリがあっても走査は続ける。
			s.log.Warn("cannot walk", "path", abs, "error", err)
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if d.IsDir() {
			return nil
		}
		if !isPNG(abs) {
			return nil
		}
		rel, err := relPath(root, abs)
		if err != nil {
			return nil
		}
		s.walked.Add(1)
		seen[rel] = true

		info, err := d.Info()
		if err != nil {
			return nil
		}
		if state, ok := known[rel]; ok && unchanged(state, info) {
			return nil
		}
		select {
		case jobs <- job{root: root, rel: rel, abs: abs}:
		case <-ctx.Done():
			return ctx.Err()
		}
		return nil
	})
	close(jobs)
	wg.Wait()

	if walkErr != nil {
		if errors.Is(walkErr, fs.ErrNotExist) {
			return fmt.Errorf("scanner: root %s does not exist: %w", root.Path, walkErr)
		}
		return walkErr
	}

	// インデックスに残っていて実体のないものを取り除く。
	for rel, state := range known {
		if seen[rel] {
			continue
		}
		s.remove(ctx, root, rel, state.ID)
	}
	return nil
}

type job struct {
	root Root
	rel  string
	abs  string
}

// indexFile は 1 枚の画像を解析してインデックスへ登録する。
// 解析に失敗したファイルは登録しない。
func (s *Scanner) indexFile(ctx context.Context, j job) {
	stat, err := os.Stat(j.abs)
	if err != nil {
		return
	}
	info, err := metadata.ReadFile(j.abs)
	if err != nil {
		s.failed.Add(1)
		s.log.Debug("cannot read image", "path", j.abs, "error", err)
		return
	}
	params := metadata.Parse(info.Parameters)

	img := &index.Image{
		Root:         j.root.Name,
		Path:         j.rel,
		Size:         stat.Size(),
		ModTime:      stat.ModTime(),
		Width:        info.Width,
		Height:       info.Height,
		CreatedAt:    stat.ModTime(),
		HasParams:    info.Parameters != "",
		Prompt:       params.Prompt,
		Negative:     params.NegativePrompt,
		Model:        params.Model,
		ModelHash:    params.ModelHash,
		Sampler:      params.Sampler,
		ScheduleType: params.ScheduleType,
		Steps:        params.Steps,
		CFGScale:     params.CFGScale,
		Seed:         params.Seed,
		Denoising:    params.Denoising,
		Version:      params.Version,
		GenWidth:     params.Width,
		GenHeight:    params.Height,
		Loras:        params.Loras,
		PositiveTags: params.PositiveTags,
		NegativeTags: params.NegativeTags,
		Extras:       params.Extras,
		Raw:          params.Raw,
	}
	if err := s.db.Put(ctx, img); err != nil {
		s.failed.Add(1)
		s.log.Error("cannot index image", "path", j.abs, "error", err)
		return
	}
	s.indexed.Add(1)
	if s.onIndexed != nil {
		s.onIndexed(ctx, img, j.abs)
	}
}

// remove は画像をインデックスから取り除く。
func (s *Scanner) remove(ctx context.Context, root Root, rel string, id int64) {
	if err := s.db.Delete(ctx, root.Name, rel); err != nil {
		s.log.Error("cannot remove image", "path", rel, "error", err)
		return
	}
	s.removed.Add(1)
	if s.onRemoved != nil && id != 0 {
		s.onRemoved(ctx, id)
	}
}

func unchanged(state index.FileState, info fs.FileInfo) bool {
	return state.Size == info.Size() && state.ModTime.Equal(info.ModTime())
}

func isPNG(p string) bool {
	return strings.EqualFold(filepath.Ext(p), ".png")
}

// relPath は絶対パスをルート相対のスラッシュ区切りへ変換する。
func relPath(root Root, abs string) (string, error) {
	rel, err := filepath.Rel(root.Path, abs)
	if err != nil {
		return "", err
	}
	if rel == "." || strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("scanner: %s is outside of %s", abs, root.Path)
	}
	return filepath.ToSlash(rel), nil
}

// cmpOr は値がゼロ値なら既定値を返す。
func cmpOr[T comparable](v, fallback T) T {
	var zero T
	if v == zero {
		return fallback
	}
	return v
}
