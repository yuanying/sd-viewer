package index

import (
	"context"
	"fmt"
	"time"
)

// SetFav は画像を Fav にする、または Fav から外す。
//
// すでに Fav の画像をもう一度 Fav にしても、最初に Fav にした日時を保つ。
// ゴミ箱の中の画像も状態を持てるため、戻したときに Fav が残る。
func (d *DB) SetFav(ctx context.Context, id int64, fav bool, at time.Time) error {
	d.writeMu.Lock()
	defer d.writeMu.Unlock()

	query := `UPDATE images SET fav_at = 0 WHERE id = ?`
	args := []any{id}
	if fav {
		query = `UPDATE images SET fav_at = CASE WHEN fav_at > 0 THEN fav_at ELSE ? END WHERE id = ?`
		args = []any{at.UnixNano(), id}
	}
	res, err := d.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("index: set fav of %d: %w", id, err)
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return ErrNotFound
	}
	return nil
}
