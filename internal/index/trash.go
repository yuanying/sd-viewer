package index

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path"
	"time"
)

var (
	// ErrTrashed はすでにゴミ箱の中にあることを表す。
	ErrTrashed = errors.New("index: image is already in the trash")
	// ErrNotTrashed はゴミ箱の中にないことを表す。
	ErrNotTrashed = errors.New("index: image is not in the trash")
)

// TrashCount はルート 1 つ分のゴミ箱の件数。
type TrashCount struct {
	Root  string `json:"root"`
	Count int    `json:"count"`
}

// Trash は画像をゴミ箱へ入れたことを記録する。
// 行のパスを trashPath へ付け替え、元のパスは戻し先として控える。
func (d *DB) Trash(ctx context.Context, id int64, trashPath string, at time.Time) error {
	dir, name := path.Split(trashPath)

	d.writeMu.Lock()
	defer d.writeMu.Unlock()

	if err := d.assertTrashed(ctx, id, false); err != nil {
		return err
	}
	_, err := d.db.ExecContext(ctx, `
		UPDATE images
		SET trashed_at = ?, orig_path = path, path = ?, dir = ?, name = ?
		WHERE id = ? AND trashed_at = 0`,
		at.UnixNano(), trashPath, trimSlash(dir), name, id)
	if err != nil {
		return fmt.Errorf("index: trash image %d: %w", id, err)
	}
	return nil
}

// Restore は画像をゴミ箱から戻したことを記録する。
// restoredPath は実際に戻した先で、元のパスが埋まっていた場合は別名になる。
func (d *DB) Restore(ctx context.Context, id int64, restoredPath string) error {
	dir, name := path.Split(restoredPath)

	d.writeMu.Lock()
	defer d.writeMu.Unlock()

	if err := d.assertTrashed(ctx, id, true); err != nil {
		return err
	}
	_, err := d.db.ExecContext(ctx, `
		UPDATE images
		SET trashed_at = 0, orig_path = '', path = ?, dir = ?, name = ?
		WHERE id = ? AND trashed_at > 0`,
		restoredPath, trimSlash(dir), name, id)
	if err != nil {
		return fmt.Errorf("index: restore image %d: %w", id, err)
	}
	return nil
}

// Purge はゴミ箱の中の画像を行ごと取り除く。ゴミ箱の外の画像は消さない。
func (d *DB) Purge(ctx context.Context, id int64) error {
	d.writeMu.Lock()
	defer d.writeMu.Unlock()

	if err := d.assertTrashed(ctx, id, true); err != nil {
		return err
	}
	if _, err := d.db.ExecContext(ctx, `DELETE FROM images WHERE id = ? AND trashed_at > 0`, id); err != nil {
		return fmt.Errorf("index: purge image %d: %w", id, err)
	}
	return nil
}

// TrashCounts はルートごとのゴミ箱の件数を、多い順に返す。
func (d *DB) TrashCounts(ctx context.Context) ([]TrashCount, error) {
	rows, err := d.db.QueryContext(ctx, `
		SELECT root, COUNT(*) AS n FROM images
		WHERE trashed_at > 0
		GROUP BY root ORDER BY n DESC, root ASC`)
	if err != nil {
		return nil, fmt.Errorf("index: count trash: %w", err)
	}
	defer rows.Close()

	var counts []TrashCount
	for rows.Next() {
		var c TrashCount
		if err := rows.Scan(&c.Root, &c.Count); err != nil {
			return nil, fmt.Errorf("index: scan trash count: %w", err)
		}
		counts = append(counts, c)
	}
	return counts, rows.Err()
}

// assertTrashed は画像がゴミ箱の中にあるかを確かめる。
// want と食い違えば ErrTrashed か ErrNotTrashed を返す。
func (d *DB) assertTrashed(ctx context.Context, id int64, want bool) error {
	var at int64
	err := d.db.QueryRowContext(ctx, `SELECT trashed_at FROM images WHERE id = ?`, id).Scan(&at)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("index: check trash state of %d: %w", id, err)
	}
	if got := at > 0; got != want {
		if got {
			return ErrTrashed
		}
		return ErrNotTrashed
	}
	return nil
}
