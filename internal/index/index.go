// Package index は画像のメタデータを SQLite へ永続化し、
// 検索・ファセット集計・タグ補完を提供する。
package index

import (
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"sync"
	"time"

	"github.com/yuanying/sd-viewer/internal/metadata"
	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schema string

// ErrNotFound は指定した画像が見つからないことを表す。
var ErrNotFound = errors.New("index: image not found")

// Image はインデックスに保持する画像 1 枚の情報を表す。
//
// Width と Height は実際の画像サイズ、GenWidth と GenHeight は
// 生成時に指定されたサイズで、Hires fix などでは両者が食い違う。
//
// PositiveTags / NegativeTags / Extras / Raw は Get でのみ埋められる。
// 一覧を返す Search では転送量を抑えるため空のままとなる。
type Image struct {
	ID           int64             `json:"id"`
	Root         string            `json:"root"`
	Path         string            `json:"path"`
	Dir          string            `json:"dir"`
	Name         string            `json:"name"`
	Size         int64             `json:"size"`
	ModTime      time.Time         `json:"mod_time"`
	Width        int               `json:"width"`
	Height       int               `json:"height"`
	CreatedAt    time.Time         `json:"created_at"`
	HasParams    bool              `json:"has_params"`
	Prompt       string            `json:"prompt"`
	Negative     string            `json:"negative"`
	Model        string            `json:"model"`
	ModelHash    string            `json:"model_hash"`
	Sampler      string            `json:"sampler"`
	ScheduleType string            `json:"schedule_type"`
	Steps        int               `json:"steps"`
	CFGScale     float64           `json:"cfg_scale"`
	Seed         int64             `json:"seed"`
	Denoising    float64           `json:"denoising"`
	Version      string            `json:"version"`
	GenWidth     int               `json:"gen_width"`
	GenHeight    int               `json:"gen_height"`
	Loras        []metadata.Lora   `json:"loras,omitempty"`
	PositiveTags []string          `json:"positive_tags,omitempty"`
	NegativeTags []string          `json:"negative_tags,omitempty"`
	Extras       map[string]string `json:"extras,omitempty"`
	Raw          string            `json:"raw,omitempty"`
}

// FileState は差分スキャンのためにインデックスが覚えているファイルの状態。
type FileState struct {
	ID      int64
	Size    int64
	ModTime time.Time
}

// DB はインデックスへの接続を表す。
type DB struct {
	db *sql.DB
	// SQLite の書き込みは 1 つずつに直列化する。
	writeMu sync.Mutex
}

// Open はインデックスを開き、必要ならスキーマを作成する。
func Open(dbPath string) (*DB, error) {
	dsn := "file:" + dbPath +
		"?_pragma=journal_mode(WAL)" +
		"&_pragma=busy_timeout(10000)" +
		"&_pragma=foreign_keys(1)" +
		"&_pragma=synchronous(NORMAL)"

	sqlDB, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("index: open database: %w", err)
	}
	if _, err := sqlDB.Exec(schema); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("index: apply schema: %w", err)
	}
	return &DB{db: sqlDB}, nil
}

// Close は接続を閉じる。
func (d *DB) Close() error {
	return d.db.Close()
}

const putSQL = `
INSERT INTO images (
    root, path, dir, name, size, mtime, width, height, created_at, has_params,
    prompt, negative, model, model_hash, sampler, schedule_type, steps,
    cfg_scale, seed, denoising, version, gen_width, gen_height, extras, raw
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (root, path) DO UPDATE SET
    dir = excluded.dir, name = excluded.name, size = excluded.size,
    mtime = excluded.mtime, width = excluded.width, height = excluded.height,
    created_at = excluded.created_at, has_params = excluded.has_params,
    prompt = excluded.prompt, negative = excluded.negative,
    model = excluded.model, model_hash = excluded.model_hash,
    sampler = excluded.sampler, schedule_type = excluded.schedule_type,
    steps = excluded.steps, cfg_scale = excluded.cfg_scale, seed = excluded.seed,
    denoising = excluded.denoising, version = excluded.version,
    gen_width = excluded.gen_width, gen_height = excluded.gen_height,
    extras = excluded.extras, raw = excluded.raw
RETURNING id`

// Put は画像を登録する。同じ root と path の行があれば内容を置き換え、
// ID は維持する。img.ID には確定した ID が書き戻される。
func (d *DB) Put(ctx context.Context, img *Image) error {
	extras := ""
	if len(img.Extras) > 0 {
		b, err := json.Marshal(img.Extras)
		if err != nil {
			return fmt.Errorf("index: encode extras: %w", err)
		}
		extras = string(b)
	}

	dir, name := path.Split(img.Path)
	img.Dir = trimSlash(dir)
	img.Name = name

	d.writeMu.Lock()
	defer d.writeMu.Unlock()

	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("index: begin transaction: %w", err)
	}
	defer tx.Rollback()

	var id int64
	err = tx.QueryRowContext(ctx, putSQL,
		img.Root, img.Path, img.Dir, img.Name, img.Size, img.ModTime.UnixNano(),
		img.Width, img.Height, img.CreatedAt.UnixNano(), img.HasParams,
		img.Prompt, img.Negative, img.Model, img.ModelHash, img.Sampler,
		img.ScheduleType, img.Steps, img.CFGScale, img.Seed, img.Denoising,
		img.Version, img.GenWidth, img.GenHeight, extras, img.Raw,
	).Scan(&id)
	if err != nil {
		return fmt.Errorf("index: put image %s: %w", img.Path, err)
	}
	img.ID = id

	if err := replaceLoras(ctx, tx, id, img.Loras); err != nil {
		return err
	}
	if err := replaceTags(ctx, tx, id, img.PositiveTags, img.NegativeTags); err != nil {
		return err
	}
	return tx.Commit()
}

func replaceLoras(ctx context.Context, tx *sql.Tx, id int64, loras []metadata.Lora) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM image_loras WHERE image_id = ?`, id); err != nil {
		return fmt.Errorf("index: clear loras: %w", err)
	}
	for i, l := range loras {
		_, err := tx.ExecContext(ctx,
			`INSERT INTO image_loras (image_id, seq, name, weight, hash) VALUES (?, ?, ?, ?, ?)`,
			id, i, l.Name, l.Weight, l.Hash)
		if err != nil {
			return fmt.Errorf("index: insert lora %s: %w", l.Name, err)
		}
	}
	return nil
}

func replaceTags(ctx context.Context, tx *sql.Tx, id int64, positive, negative []string) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM image_tags WHERE image_id = ?`, id); err != nil {
		return fmt.Errorf("index: clear tags: %w", err)
	}
	for kind, tags := range [][]string{positive, negative} {
		for i, tag := range tags {
			_, err := tx.ExecContext(ctx,
				`INSERT INTO image_tags (image_id, kind, seq, tag) VALUES (?, ?, ?, ?)`,
				id, kind, i, tag)
			if err != nil {
				return fmt.Errorf("index: insert tag %s: %w", tag, err)
			}
		}
	}
	return nil
}

// Delete は指定パスの画像をインデックスから取り除く。
func (d *DB) Delete(ctx context.Context, root, imgPath string) error {
	d.writeMu.Lock()
	defer d.writeMu.Unlock()

	_, err := d.db.ExecContext(ctx, `DELETE FROM images WHERE root = ? AND path = ?`, root, imgPath)
	if err != nil {
		return fmt.Errorf("index: delete %s: %w", imgPath, err)
	}
	return nil
}

// DeleteUnder は指定ディレクトリ配下の画像をまとめて取り除く。
func (d *DB) DeleteUnder(ctx context.Context, root, dir string) error {
	d.writeMu.Lock()
	defer d.writeMu.Unlock()

	_, err := d.db.ExecContext(ctx,
		`DELETE FROM images WHERE root = ? AND (dir = ? OR dir LIKE ? ESCAPE '\')`,
		root, dir, escapeLike(dir)+`/%`)
	if err != nil {
		return fmt.Errorf("index: delete under %s: %w", dir, err)
	}
	return nil
}

// Move は画像のパスを付け替える。ID は維持される。
func (d *DB) Move(ctx context.Context, root, oldPath, newPath string) error {
	dir, name := path.Split(newPath)

	d.writeMu.Lock()
	defer d.writeMu.Unlock()

	res, err := d.db.ExecContext(ctx,
		`UPDATE images SET path = ?, dir = ?, name = ? WHERE root = ? AND path = ?`,
		newPath, trimSlash(dir), name, root, oldPath)
	if err != nil {
		return fmt.Errorf("index: move %s: %w", oldPath, err)
	}
	if n, err := res.RowsAffected(); err == nil && n == 0 {
		return ErrNotFound
	}
	return nil
}

// Get は ID を指定して画像の全情報を取得する。
func (d *DB) Get(ctx context.Context, id int64) (*Image, error) {
	row := d.db.QueryRowContext(ctx,
		`SELECT `+imageColumns+`, extras, raw FROM images WHERE id = ?`, id)

	var (
		img     Image
		extras  string
		mtime   int64
		created int64
	)
	err := row.Scan(
		&img.ID, &img.Root, &img.Path, &img.Dir, &img.Name, &img.Size, &mtime,
		&img.Width, &img.Height, &created, &img.HasParams, &img.Prompt, &img.Negative,
		&img.Model, &img.ModelHash, &img.Sampler, &img.ScheduleType, &img.Steps,
		&img.CFGScale, &img.Seed, &img.Denoising, &img.Version, &img.GenWidth, &img.GenHeight,
		&extras, &img.Raw,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("index: get image %d: %w", id, err)
	}
	img.ModTime = time.Unix(0, mtime).UTC()
	img.CreatedAt = time.Unix(0, created).UTC()

	if extras != "" {
		if err := json.Unmarshal([]byte(extras), &img.Extras); err != nil {
			return nil, fmt.Errorf("index: decode extras: %w", err)
		}
	}
	if err := d.loadLoras(ctx, []*Image{&img}); err != nil {
		return nil, err
	}
	if err := d.loadTags(ctx, &img); err != nil {
		return nil, err
	}
	return &img, nil
}

// States はルート配下の既知ファイルの状態を、ルート相対パスをキーに返す。
func (d *DB) States(ctx context.Context, root string) (map[string]FileState, error) {
	rows, err := d.db.QueryContext(ctx,
		`SELECT id, path, size, mtime FROM images WHERE root = ?`, root)
	if err != nil {
		return nil, fmt.Errorf("index: list states: %w", err)
	}
	defer rows.Close()

	states := map[string]FileState{}
	for rows.Next() {
		var (
			p     string
			state FileState
			mtime int64
		)
		if err := rows.Scan(&state.ID, &p, &state.Size, &mtime); err != nil {
			return nil, fmt.Errorf("index: scan state: %w", err)
		}
		state.ModTime = time.Unix(0, mtime).UTC()
		states[p] = state
	}
	return states, rows.Err()
}

// State は 1 件のファイルの状態を返す。見つからなければ ok が false になる。
func (d *DB) State(ctx context.Context, root, imgPath string) (FileState, bool, error) {
	var (
		state FileState
		mtime int64
	)
	err := d.db.QueryRowContext(ctx,
		`SELECT id, size, mtime FROM images WHERE root = ? AND path = ?`, root, imgPath).
		Scan(&state.ID, &state.Size, &mtime)
	if errors.Is(err, sql.ErrNoRows) {
		return FileState{}, false, nil
	}
	if err != nil {
		return FileState{}, false, fmt.Errorf("index: get state %s: %w", imgPath, err)
	}
	state.ModTime = time.Unix(0, mtime).UTC()
	return state, true, nil
}

func (d *DB) loadTags(ctx context.Context, img *Image) error {
	rows, err := d.db.QueryContext(ctx,
		`SELECT kind, tag FROM image_tags WHERE image_id = ? ORDER BY kind, seq`, img.ID)
	if err != nil {
		return fmt.Errorf("index: load tags: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			kind int
			tag  string
		)
		if err := rows.Scan(&kind, &tag); err != nil {
			return fmt.Errorf("index: scan tag: %w", err)
		}
		if kind == 0 {
			img.PositiveTags = append(img.PositiveTags, tag)
		} else {
			img.NegativeTags = append(img.NegativeTags, tag)
		}
	}
	return rows.Err()
}

// loadLoras は与えられた画像群の LoRA をまとめて 1 度のクエリで読み込む。
func (d *DB) loadLoras(ctx context.Context, images []*Image) error {
	if len(images) == 0 {
		return nil
	}
	byID := make(map[int64]*Image, len(images))
	ids := make([]any, 0, len(images))
	for _, img := range images {
		byID[img.ID] = img
		ids = append(ids, img.ID)
	}

	query := `SELECT image_id, name, weight, hash FROM image_loras
	          WHERE image_id IN (` + placeholders(len(ids)) + `) ORDER BY image_id, seq`
	rows, err := d.db.QueryContext(ctx, query, ids...)
	if err != nil {
		return fmt.Errorf("index: load loras: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			id   int64
			lora metadata.Lora
		)
		if err := rows.Scan(&id, &lora.Name, &lora.Weight, &lora.Hash); err != nil {
			return fmt.Errorf("index: scan lora: %w", err)
		}
		if img, ok := byID[id]; ok {
			img.Loras = append(img.Loras, lora)
		}
	}
	return rows.Err()
}

func trimSlash(s string) string {
	for len(s) > 0 && s[len(s)-1] == '/' {
		s = s[:len(s)-1]
	}
	return s
}
