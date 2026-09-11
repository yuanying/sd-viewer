package server

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/yuanying/sd-viewer/internal/index"
)

const (
	defaultLimit = 100
	maxLimit     = 500
)

// dateLayouts は日付指定として受け付ける書式。
var dateLayouts = []string{time.RFC3339, "2006-01-02T15:04", "2006-01-02"}

// parseQuery はクエリ文字列を検索条件へ変換する。
// 解釈できない値は指定がなかったものとして扱い、エラーにはしない。
func parseQuery(r *http.Request) index.Query {
	v := r.URL.Query()

	q := index.Query{
		Text:        strings.TrimSpace(v.Get("q")),
		Models:      values(v, "model"),
		Loras:       values(v, "lora"),
		Samplers:    values(v, "sampler"),
		Sizes:       values(v, "size"),
		Dirs:        values(v, "dir"),
		Roots:       values(v, "root"),
		Tags:        values(v, "tag"),
		ExcludeTags: values(v, "exclude_tag"),
		Fav:         parseFlag(v.Get("fav")),
		From:        parseDate(v.Get("from")),
		To:          parseDate(v.Get("to")),
		Limit:       intParam(r, "limit", defaultLimit, maxLimit),
		Offset:      intParam(r, "offset", 0, 0),
	}

	switch index.SortOrder(v.Get("sort")) {
	case index.SortOldest:
		q.Sort = index.SortOldest
	case index.SortName:
		q.Sort = index.SortName
	default:
		q.Sort = index.SortNewest
	}
	return q
}

// values は同名パラメータをまとめて取り出す。空の値は無視する。
func values(v url.Values, key string) []string {
	var out []string
	for _, s := range v[key] {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// intParam は整数のパラメータを読む。負の値や解釈できない値は既定値とする。
// max が 0 より大きい場合は上限で頭打ちにする。
func intParam(r *http.Request, key string, fallback, max int) int {
	raw := r.URL.Query().Get(key)
	if raw == "" {
		return fallback
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return fallback
	}
	if max > 0 && n > max {
		return max
	}
	return n
}

// parseFlag は真偽のパラメータを読む。真と解釈できない値は偽とする。
func parseFlag(raw string) bool {
	b, err := strconv.ParseBool(strings.TrimSpace(raw))
	return err == nil && b
}

// parseDate は日付または日時を読む。時刻を省いた場合はその日の始まりとする。
func parseDate(raw string) time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}
	}
	for _, layout := range dateLayouts {
		if t, err := time.ParseInLocation(layout, raw, time.Local); err == nil {
			return t
		}
	}
	return time.Time{}
}
