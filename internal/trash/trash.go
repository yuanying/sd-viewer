// Package trash はゴミ箱への移動・復元・完全削除を行う。
//
// ゴミ箱はルートごとに持ち、実体はルート直下の .trash ディレクトリである。
// その下には元の相対パスをそのまま再現するため、戻し先は迷わずに決まる。
package trash

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/yuanying/sd-viewer/internal/index"
)

// DirName はルート直下に置くゴミ箱ディレクトリの名前。
const DirName = ".trash"

// maxSuffix は名前の衝突を避けるために試す連番の上限。
const maxSuffix = 1000

// Options はゴミ箱の設定。
type Options struct {
	DB *index.DB
	// Roots はルート名から実ディレクトリを引くための対応表。
	Roots map[string]string
	// OnPurged は画像を完全に削除した直後に呼ばれる。サムネイルの削除に使う。
	OnPurged func(id int64)
	// Now は現在時刻。省略すると time.Now を使う。
	Now    func() time.Time
	Logger *slog.Logger
}

// Bin はゴミ箱。
type Bin struct {
	db       *index.DB
	roots    map[string]string
	onPurged func(int64)
	now      func() time.Time
	log      *slog.Logger
}

// Failure は処理できなかった画像 1 件と、その理由。
type Failure struct {
	ID     int64  `json:"id"`
	Reason string `json:"reason"`
}

// Result はまとめて処理した結果。1 件の失敗で全体を止めず、
// できたところまで進めた件数と、できなかったものを返す。
type Result struct {
	Done   int       `json:"done"`
	Failed []Failure `json:"failed,omitempty"`
}

// fail は失敗を 1 件書き留める。
func (r *Result) fail(id int64, err error) {
	r.Failed = append(r.Failed, Failure{ID: id, Reason: err.Error()})
}

// New はゴミ箱を組み立てる。
func New(opts Options) *Bin {
	b := &Bin{
		db:       opts.DB,
		roots:    opts.Roots,
		onPurged: opts.OnPurged,
		now:      opts.Now,
		log:      opts.Logger,
	}
	if b.now == nil {
		b.now = time.Now
	}
	if b.log == nil {
		b.log = slog.Default()
	}
	return b
}

// IsTrashPath はルート相対パスがゴミ箱の中を指すかを返す。
// ゴミ箱はルート直下にしか作らないため、先頭の要素だけを見る。
func IsTrashPath(rel string) bool {
	return rel == DirName || strings.HasPrefix(rel, DirName+"/")
}

// Move は画像をゴミ箱へ入れる。
//
// インデックスの状態を先に書き換えてからファイルを移す。逆順にすると、
// 監視がファイルの消失を先に拾って行ごと消してしまうことがある。
func (b *Bin) Move(ctx context.Context, ids []int64) Result {
	var res Result
	for _, id := range ids {
		if err := b.move(ctx, id); err != nil {
			res.fail(id, err)
			continue
		}
		res.Done++
	}
	return res
}

func (b *Bin) move(ctx context.Context, id int64) error {
	img, root, err := b.locate(ctx, id)
	if err != nil {
		return err
	}
	if !img.TrashedAt.IsZero() {
		return errors.New("すでにゴミ箱に入っています")
	}

	src := filepath.Join(root, filepath.FromSlash(img.Path))
	dst, rel, err := uniquePath(root, path.Join(DirName, img.Path))
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("ゴミ箱を作れません: %w", err)
	}

	if err := b.db.Trash(ctx, id, rel, b.now()); err != nil {
		return err
	}
	if err := os.Rename(src, dst); err != nil {
		// 動かせなかったので、インデックスも元の状態へ戻す。
		if undo := b.db.Restore(ctx, id, img.Path); undo != nil {
			b.log.Error("cannot undo trash", "id", id, "error", undo)
		}
		return fmt.Errorf("ゴミ箱へ移せません: %w", err)
	}
	return nil
}

// Restore は画像を元の場所へ戻す。
// 戻し先が埋まっている場合は連番を付けた名前にして、戻す操作自体は通す。
func (b *Bin) Restore(ctx context.Context, ids []int64) Result {
	var res Result
	for _, id := range ids {
		if err := b.restore(ctx, id); err != nil {
			res.fail(id, err)
			continue
		}
		res.Done++
	}
	return res
}

func (b *Bin) restore(ctx context.Context, id int64) error {
	img, root, err := b.locate(ctx, id)
	if err != nil {
		return err
	}
	if img.TrashedAt.IsZero() {
		return errors.New("ゴミ箱に入っていません")
	}

	src := filepath.Join(root, filepath.FromSlash(img.Path))
	dst, rel, err := uniquePath(root, img.OrigPath)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("戻し先を作れません: %w", err)
	}

	if err := b.db.Restore(ctx, id, rel); err != nil {
		return err
	}
	if err := os.Rename(src, dst); err != nil {
		if undo := b.db.Trash(ctx, id, img.Path, img.TrashedAt); undo != nil {
			b.log.Error("cannot undo restore", "id", id, "error", undo)
		}
		return fmt.Errorf("元の場所へ戻せません: %w", err)
	}
	b.prune(root, path.Dir(img.Path))
	return nil
}

// Purge は画像を完全に削除する。ゴミ箱の中のものだけを対象とする。
func (b *Bin) Purge(ctx context.Context, ids []int64) Result {
	var res Result
	for _, id := range ids {
		if err := b.purge(ctx, id); err != nil {
			res.fail(id, err)
			continue
		}
		res.Done++
	}
	return res
}

func (b *Bin) purge(ctx context.Context, id int64) error {
	img, root, err := b.locate(ctx, id)
	if err != nil {
		return err
	}
	if img.TrashedAt.IsZero() {
		return errors.New("ゴミ箱に入っていません")
	}

	abs := filepath.Join(root, filepath.FromSlash(img.Path))
	if err := os.Remove(abs); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("ファイルを消せません: %w", err)
	}
	if err := b.db.Purge(ctx, id); err != nil {
		return err
	}
	if b.onPurged != nil {
		b.onPurged(id)
	}
	b.prune(root, path.Dir(img.Path))
	return nil
}

// Empty はゴミ箱を空にする。root が空ならすべてのルートを対象とする。
func (b *Bin) Empty(ctx context.Context, root string) (Result, error) {
	q := index.Query{Trashed: true}
	if root != "" {
		q.Roots = []string{root}
	}
	found, err := b.db.Search(ctx, q)
	if err != nil {
		return Result{}, err
	}

	ids := make([]int64, 0, len(found.Images))
	for _, img := range found.Images {
		ids = append(ids, img.ID)
	}
	return b.Purge(ctx, ids), nil
}

// locate は画像と、その置き場所となるルートのディレクトリを引く。
func (b *Bin) locate(ctx context.Context, id int64) (*index.Image, string, error) {
	img, err := b.db.Get(ctx, id)
	if errors.Is(err, index.ErrNotFound) {
		return nil, "", errors.New("画像が見つかりません")
	}
	if err != nil {
		return nil, "", err
	}
	root, ok := b.roots[img.Root]
	if !ok {
		return nil, "", fmt.Errorf("ルート %s は監視対象ではありません", img.Root)
	}
	return img, root, nil
}

// prune は空になったディレクトリを、ルートへ届く手前まで遡って片付ける。
// ゴミ箱そのものは、次に使うため残しておく。
func (b *Bin) prune(root, dir string) {
	for dir != "" && dir != "." && dir != DirName && !strings.HasPrefix(dir, "..") {
		if err := os.Remove(filepath.Join(root, filepath.FromSlash(dir))); err != nil {
			// 中身が残っていれば失敗する。そこで打ち切ってよい。
			return
		}
		dir = path.Dir(dir)
	}
}

// uniquePath は空いている置き場所を探し、絶対パスとルート相対パスを返す。
// 埋まっている場合は拡張子の手前へ連番を足す。
func uniquePath(root, rel string) (string, string, error) {
	abs := filepath.Join(root, filepath.FromSlash(rel))
	if _, err := os.Lstat(abs); errors.Is(err, os.ErrNotExist) {
		return abs, rel, nil
	}

	ext := path.Ext(rel)
	stem := strings.TrimSuffix(rel, ext)
	for i := 2; i < maxSuffix; i++ {
		candidate := fmt.Sprintf("%s (%d)%s", stem, i, ext)
		abs := filepath.Join(root, filepath.FromSlash(candidate))
		if _, err := os.Lstat(abs); errors.Is(err, os.ErrNotExist) {
			return abs, candidate, nil
		}
	}
	return "", "", fmt.Errorf("%s は名前が埋まっていて置けません", rel)
}
