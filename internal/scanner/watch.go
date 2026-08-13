package scanner

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// Watch はファイルシステムのイベントを監視し、インデックスへ反映し続ける。
// ctx が終わるまで戻らない。
func (s *Scanner) Watch(ctx context.Context) error {
	for _, root := range s.roots {
		if err := s.watchTree(root.Path); err != nil {
			return err
		}
	}

	// イベントは書き込みが落ち着くまでまとめてから処理する。
	pending := map[string]bool{}
	timer := time.NewTimer(s.debounce)
	if !timer.Stop() {
		<-timer.C
	}
	defer timer.Stop()

	s.watching.Store(true)
	defer s.watching.Store(false)

	for {
		select {
		case <-ctx.Done():
			return nil

		case event, ok := <-s.watcher.Events:
			if !ok {
				return nil
			}
			pending[event.Name] = true
			// 同じ対象へのイベントが続く間は処理を待つ。
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(s.debounce)

		case err, ok := <-s.watcher.Errors:
			if !ok {
				return nil
			}
			s.log.Warn("watcher error", "error", err)

		case <-timer.C:
			s.apply(ctx, pending)
			pending = map[string]bool{}
		}
	}
}

// watchTree はディレクトリを再帰的に監視対象へ加える。
func (s *Scanner) watchTree(dir string) error {
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			s.log.Warn("cannot watch", "path", path, "error", err)
			return nil
		}
		if !d.IsDir() {
			return nil
		}
		if err := s.watcher.Add(path); err != nil {
			s.log.Warn("cannot watch directory", "path", path, "error", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("scanner: watch %s: %w", dir, err)
	}
	return nil
}

// change はイベントが起きたパスを、ルートからの相対位置つきで表す。
type change struct {
	root Root
	rel  string
	abs  string
	info os.FileInfo
}

// apply はまとめたイベントをインデックスへ反映する。
//
// 消えたパスと現れたパスを突き合わせ、サイズと更新日時が同じものは
// 移動とみなしてインデックスの行を付け替える。
func (s *Scanner) apply(ctx context.Context, pending map[string]bool) {
	var (
		created []change
		removed []change
		dirs    []change
	)
	for abs := range pending {
		root, rel, ok := s.locate(abs)
		if !ok {
			continue
		}
		c := change{root: root, rel: rel, abs: abs}

		info, err := os.Stat(abs)
		switch {
		case err != nil:
			removed = append(removed, c)
		case info.IsDir():
			dirs = append(dirs, c)
		case isPNG(abs):
			c.info = info
			created = append(created, c)
		}
	}

	created = s.applyMoves(ctx, removed, created)
	for _, c := range created {
		s.indexFile(ctx, job{root: c.root, rel: c.rel, abs: c.abs})
	}
	for _, c := range dirs {
		s.applyNewDir(ctx, c)
	}
}

// applyMoves は移動を検出して反映し、移動として扱えなかった作成分を返す。
func (s *Scanner) applyMoves(ctx context.Context, removed, created []change) []change {
	moved := map[int]bool{}

	for _, c := range removed {
		state, ok, err := s.db.State(ctx, c.root.Name, c.rel)
		if err != nil {
			s.log.Error("cannot look up state", "path", c.rel, "error", err)
			continue
		}
		if !ok {
			// ディレクトリごと消えた場合は配下をまとめて取り除く。
			s.removeTree(ctx, c)
			continue
		}

		// 同じ内容のファイルが現れていれば移動とみなす。
		target := -1
		for i, n := range created {
			if moved[i] || n.root.Name != c.root.Name {
				continue
			}
			if n.info.Size() == state.Size && n.info.ModTime().Equal(state.ModTime) {
				target = i
				break
			}
		}
		if target < 0 {
			s.remove(ctx, c.root, c.rel, state.ID)
			continue
		}
		if err := s.db.Move(ctx, c.root.Name, c.rel, created[target].rel); err != nil {
			s.log.Error("cannot move image", "from", c.rel, "to", created[target].rel, "error", err)
			continue
		}
		moved[target] = true
	}

	rest := created[:0]
	for i, c := range created {
		if !moved[i] {
			rest = append(rest, c)
		}
	}
	return rest
}

// removeTree は消えたディレクトリの配下をインデックスから取り除く。
func (s *Scanner) removeTree(ctx context.Context, c change) {
	if err := s.db.DeleteUnder(ctx, c.root.Name, c.rel); err != nil {
		s.log.Error("cannot remove directory", "path", c.rel, "error", err)
	}
}

// applyNewDir は新しく現れたディレクトリを監視対象へ加え、
// 監視が始まる前に作られたファイルを取りこぼさないよう走査する。
func (s *Scanner) applyNewDir(ctx context.Context, c change) {
	if err := s.watchTree(c.abs); err != nil {
		s.log.Warn("cannot watch new directory", "path", c.abs, "error", err)
	}
	err := filepath.WalkDir(c.abs, func(abs string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !isPNG(abs) {
			return nil
		}
		rel, err := relPath(c.root, abs)
		if err != nil {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		state, ok, err := s.db.State(ctx, c.root.Name, rel)
		if err == nil && ok && unchanged(state, info) {
			return nil
		}
		s.indexFile(ctx, job{root: c.root, rel: rel, abs: abs})
		return nil
	})
	if err != nil {
		s.log.Warn("cannot scan new directory", "path", c.abs, "error", err)
	}
}

// locate は絶対パスがどのルートに属するかを調べる。
func (s *Scanner) locate(abs string) (Root, string, bool) {
	var (
		found Root
		rel   string
	)
	for _, root := range s.roots {
		r, err := relPath(root, abs)
		if err != nil {
			continue
		}
		// ルートが入れ子の場合はより深いほうを採用する。
		if rel == "" || len(r) < len(rel) {
			found, rel = root, r
		}
	}
	if rel == "" {
		return Root{}, "", false
	}
	return found, rel, true
}
