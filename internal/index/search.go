package index

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode"
)

// imageColumns は一覧・詳細で共通して読み出す列。
const imageColumns = `id, root, path, dir, name, size, mtime, width, height, created_at,
	has_params, prompt, negative, model, model_hash, sampler, schedule_type, steps,
	cfg_scale, seed, denoising, version, gen_width, gen_height, trashed_at, orig_path, fav_at`

// imageTimes は SQLite が整数で持つ日時の受け皿。
type imageTimes struct {
	mtime   int64
	created int64
	trashed int64
	faved   int64
}

// apply は読み出した整数を Image の日時へ移す。
func (t imageTimes) apply(img *Image) {
	img.ModTime = time.Unix(0, t.mtime).UTC()
	img.CreatedAt = time.Unix(0, t.created).UTC()
	if t.trashed > 0 {
		img.TrashedAt = time.Unix(0, t.trashed).UTC()
	}
	if t.faved > 0 {
		img.FavAt = time.Unix(0, t.faved).UTC()
	}
}

// scanTargets は imageColumns の並びに対応した Scan の引数を返す。
func scanTargets(img *Image, times *imageTimes) []any {
	return []any{
		&img.ID, &img.Root, &img.Path, &img.Dir, &img.Name, &img.Size, &times.mtime,
		&img.Width, &img.Height, &times.created, &img.HasParams, &img.Prompt, &img.Negative,
		&img.Model, &img.ModelHash, &img.Sampler, &img.ScheduleType, &img.Steps,
		&img.CFGScale, &img.Seed, &img.Denoising, &img.Version, &img.GenWidth, &img.GenHeight,
		&times.trashed, &img.OrigPath, &times.faved,
	}
}

// SortOrder は検索結果の並び順。
type SortOrder string

const (
	// SortNewest は生成日時の新しい順。既定値。
	SortNewest SortOrder = "newest"
	// SortOldest は生成日時の古い順。
	SortOldest SortOrder = "oldest"
	// SortName はパスの昇順。
	SortName SortOrder = "name"
)

// maxFacetValues はファセットとして返す候補数の上限。
const maxFacetValues = 300

// Query は検索条件を表す。
//
// 同じ項目に複数の値を指定した場合は OR、異なる項目どうしは AND で結合する。
// ただし Tags だけは指定したすべてを含むもの（AND）を返す。
type Query struct {
	Text        string
	Models      []string
	Loras       []string
	Samplers    []string
	Sizes       []string
	Dirs        []string
	Roots       []string
	Tags        []string
	ExcludeTags []string
	// Trashed が真ならゴミ箱の中だけを、偽ならゴミ箱の外だけを対象とする。
	Trashed bool
	// Fav が真なら Fav にした画像だけを対象とする。ほかの条件とは AND で結ぶ。
	Fav    bool
	From   time.Time
	To     time.Time
	Sort   SortOrder
	Limit  int
	Offset int
}

// SearchResult は検索結果と、条件に一致した総件数を表す。
type SearchResult struct {
	Total  int      `json:"total"`
	Images []*Image `json:"images"`
}

// FacetValue はファセットの候補 1 件と、その件数。
type FacetValue struct {
	Value string `json:"value"`
	Count int    `json:"count"`
}

// FacetSet は絞り込み候補の一覧。
type FacetSet struct {
	Models   []FacetValue `json:"models"`
	Loras    []FacetValue `json:"loras"`
	Samplers []FacetValue `json:"samplers"`
	Sizes    []FacetValue `json:"sizes"`
	Dirs     []FacetValue `json:"dirs"`
	Roots    []FacetValue `json:"roots"`
}

// TagCount はタグ候補 1 件と、その出現数。
type TagCount struct {
	Tag   string `json:"tag"`
	Count int    `json:"count"`
}

// facet は絞り込み条件の種別。ファセット集計では自身の条件だけを外す。
type facet int

const (
	facetNone facet = iota
	facetModel
	facetLora
	facetSampler
	facetSize
	facetDir
	facetRoot
)

// Search は条件に一致する画像を返す。
func (d *DB) Search(ctx context.Context, q Query) (*SearchResult, error) {
	where, args := buildWhere(q, facetNone)

	res := &SearchResult{}
	if err := d.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM images`+where, args...).Scan(&res.Total); err != nil {
		return nil, fmt.Errorf("index: count images: %w", err)
	}
	if res.Total == 0 {
		return res, nil
	}

	limit := q.Limit
	if limit <= 0 {
		limit = -1
	}
	query := `SELECT ` + imageColumns + ` FROM images` + where + ` ORDER BY ` + orderBy(q) + ` LIMIT ? OFFSET ?`
	rows, err := d.db.QueryContext(ctx, query, append(append([]any{}, args...), limit, q.Offset)...)
	if err != nil {
		return nil, fmt.Errorf("index: search images: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			img   Image
			times imageTimes
		)
		if err := rows.Scan(scanTargets(&img, &times)...); err != nil {
			return nil, fmt.Errorf("index: scan image: %w", err)
		}
		times.apply(&img)
		res.Images = append(res.Images, &img)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := d.loadLoras(ctx, res.Images); err != nil {
		return nil, err
	}
	return res, nil
}

func orderBy(q Query) string {
	// ゴミ箱は捨てた順に見たいため、並び順の指定は使わない。
	if q.Trashed {
		return `trashed_at DESC, id DESC`
	}
	switch q.Sort {
	case SortOldest:
		return `created_at ASC, id ASC`
	case SortName:
		return `path ASC, id ASC`
	default:
		return `created_at DESC, id DESC`
	}
}

// Facets は現在の絞り込み条件のもとでの候補と件数を返す。
// 各ファセットは自身の選択を除いた条件で集計するため、選択の切り替えができる。
func (d *DB) Facets(ctx context.Context, q Query) (*FacetSet, error) {
	set := &FacetSet{}

	columns := []struct {
		expr   string
		skip   facet
		target *[]FacetValue
	}{
		{`model`, facetModel, &set.Models},
		{`sampler`, facetSampler, &set.Samplers},
		{`width || 'x' || height`, facetSize, &set.Sizes},
		{`dir`, facetDir, &set.Dirs},
		{`root`, facetRoot, &set.Roots},
	}
	for _, c := range columns {
		where, args := buildWhere(q, c.skip)
		query := `SELECT ` + c.expr + ` AS value, COUNT(*) AS n FROM images` + where +
			` AND value <> '' GROUP BY value ORDER BY n DESC, value ASC LIMIT ?`
		values, err := d.facetValues(ctx, query, append(append([]any{}, args...), maxFacetValues)...)
		if err != nil {
			return nil, err
		}
		*c.target = values
	}

	where, args := buildWhere(q, facetLora)
	query := `SELECT image_loras.name AS value, COUNT(*) AS n
	          FROM image_loras JOIN images ON images.id = image_loras.image_id` + where +
		` AND value <> '' GROUP BY value ORDER BY n DESC, value ASC LIMIT ?`
	loras, err := d.facetValues(ctx, query, append(append([]any{}, args...), maxFacetValues)...)
	if err != nil {
		return nil, err
	}
	set.Loras = loras

	return set, nil
}

func (d *DB) facetValues(ctx context.Context, query string, args ...any) ([]FacetValue, error) {
	rows, err := d.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("index: aggregate facet: %w", err)
	}
	defer rows.Close()

	// 候補が無くても nil にはしない。JSON で null になると、画面が長さを読めずに落ちる。
	values := []FacetValue{}
	for rows.Next() {
		var v FacetValue
		if err := rows.Scan(&v.Value, &v.Count); err != nil {
			return nil, fmt.Errorf("index: scan facet: %w", err)
		}
		values = append(values, v)
	}
	return values, rows.Err()
}

// TagSuggest は入力中の文字列に一致するタグを、出現数の多い順に返す。
// 前方一致するものを先に並べる。
func (d *DB) TagSuggest(ctx context.Context, prefix string, limit int) ([]TagCount, error) {
	if limit <= 0 {
		limit = 20
	}
	prefix = strings.ToLower(strings.TrimSpace(prefix))
	pattern := escapeLike(prefix)

	rows, err := d.db.QueryContext(ctx, `
		SELECT tag, COUNT(*) AS n FROM image_tags
		JOIN images ON images.id = image_tags.image_id AND images.trashed_at = 0
		WHERE kind = 0 AND tag LIKE ? ESCAPE '\'
		GROUP BY tag
		ORDER BY (CASE WHEN tag LIKE ? ESCAPE '\' THEN 0 ELSE 1 END), n DESC, tag ASC
		LIMIT ?`,
		"%"+pattern+"%", pattern+"%", limit)
	if err != nil {
		return nil, fmt.Errorf("index: suggest tags: %w", err)
	}
	defer rows.Close()

	var tags []TagCount
	for rows.Next() {
		var t TagCount
		if err := rows.Scan(&t.Tag, &t.Count); err != nil {
			return nil, fmt.Errorf("index: scan tag suggestion: %w", err)
		}
		tags = append(tags, t)
	}
	return tags, rows.Err()
}

// buildWhere は検索条件から WHERE 句を組み立てる。
// skip に指定したファセットの条件は含めない。
// ゴミ箱の内外を必ず絞るため、戻り値の WHERE 句が空になることはない。
func buildWhere(q Query, skip facet) (string, []any) {
	// ゴミ箱の中と外は混ぜない。条件を書かないと消したものが一覧へ戻ってしまう。
	conds := []string{`images.trashed_at = 0`}
	if q.Trashed {
		conds[0] = `images.trashed_at > 0`
	}
	// Fav はファセットではないため、どの集計でも外さない。
	if q.Fav {
		conds = append(conds, `images.fav_at > 0`)
	}
	var args []any

	if match := ftsMatch(q.Text); match != "" {
		conds = append(conds, `images.id IN (SELECT rowid FROM images_fts WHERE images_fts MATCH ?)`)
		args = append(args, match)
	}

	inCond := func(expr string, values []string, kind facet) {
		if len(values) == 0 || skip == kind {
			return
		}
		conds = append(conds, expr+` IN (`+placeholders(len(values))+`)`)
		for _, v := range values {
			args = append(args, v)
		}
	}
	inCond(`images.model`, q.Models, facetModel)
	inCond(`images.sampler`, q.Samplers, facetSampler)
	inCond(`images.width || 'x' || images.height`, q.Sizes, facetSize)
	inCond(`images.root`, q.Roots, facetRoot)

	if len(q.Loras) > 0 && skip != facetLora {
		conds = append(conds,
			`images.id IN (SELECT image_id FROM image_loras WHERE name IN (`+placeholders(len(q.Loras))+`))`)
		for _, v := range q.Loras {
			args = append(args, v)
		}
	}

	// ディレクトリは配下も含めて絞り込む。
	if len(q.Dirs) > 0 && skip != facetDir {
		var parts []string
		for _, dir := range q.Dirs {
			parts = append(parts, `(images.dir = ? OR images.dir LIKE ? ESCAPE '\')`)
			args = append(args, dir, escapeLike(dir)+`/%`)
		}
		conds = append(conds, `(`+strings.Join(parts, ` OR `)+`)`)
	}

	for _, tag := range q.Tags {
		conds = append(conds,
			`EXISTS (SELECT 1 FROM image_tags WHERE image_tags.image_id = images.id AND image_tags.kind = 0 AND image_tags.tag = ?)`)
		args = append(args, tag)
	}
	for _, tag := range q.ExcludeTags {
		conds = append(conds,
			`NOT EXISTS (SELECT 1 FROM image_tags WHERE image_tags.image_id = images.id AND image_tags.kind = 0 AND image_tags.tag = ?)`)
		args = append(args, tag)
	}

	if !q.From.IsZero() {
		conds = append(conds, `images.created_at >= ?`)
		args = append(args, q.From.UnixNano())
	}
	if !q.To.IsZero() {
		conds = append(conds, `images.created_at < ?`)
		args = append(args, q.To.UnixNano())
	}

	return ` WHERE ` + strings.Join(conds, ` AND `), args
}

// ftsMatch は利用者が入力した検索語を FTS5 のクエリへ変換する。
// 記号だけの語は取り除き、どんな入力でも構文エラーにならないようにする。
func ftsMatch(text string) string {
	var include, exclude []string
	for _, token := range tokenizeQuery(text) {
		negate := strings.HasPrefix(token, "-")
		token = strings.TrimPrefix(token, "-")
		token = sanitizeToken(token)
		if token == "" {
			continue
		}
		phrase := `"` + strings.ReplaceAll(token, `"`, `""`) + `"`
		if negate {
			exclude = append(exclude, phrase)
		} else {
			include = append(include, phrase)
		}
	}
	if len(include) == 0 {
		return ""
	}
	var match strings.Builder
	match.WriteString(strings.Join(include, " AND "))
	for _, e := range exclude {
		match.WriteString(" NOT ")
		match.WriteString(e)
	}
	return match.String()
}

// tokenizeQuery は空白で語を区切る。引用符で囲まれた部分は 1 語として扱う。
func tokenizeQuery(s string) []string {
	var (
		tokens  []string
		cur     strings.Builder
		inQuote bool
	)
	flush := func() {
		if cur.Len() > 0 {
			tokens = append(tokens, cur.String())
			cur.Reset()
		}
	}
	for _, r := range s {
		switch {
		case r == '"':
			inQuote = !inQuote
		case unicode.IsSpace(r) && !inQuote:
			flush()
		default:
			cur.WriteRune(r)
		}
	}
	flush()
	return tokens
}

// sanitizeToken は FTS5 が語として扱えない文字を空白へ落とす。
func sanitizeToken(s string) string {
	var b strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || unicode.IsSpace(r) {
			b.WriteRune(r)
			continue
		}
		b.WriteRune(' ')
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

func placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

// escapeLike は LIKE のワイルドカードを打ち消す。
func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}
