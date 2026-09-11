package server

import (
	"encoding/json"
	"testing"
)

// facetKeys は /api/facets が返す項目。
var facetKeys = []string{"models", "loras", "samplers", "sizes", "dirs", "roots"}

// rawJSON は応答を項目ごとの生の JSON として読む。
func (e *testEnv) rawJSON(t *testing.T, target string) map[string]json.RawMessage {
	t.Helper()
	var raw map[string]json.RawMessage
	e.getJSON(t, target, &raw)
	return raw
}

func TestFacets_該当がなければ全項目を空配列で返す(t *testing.T) {
	tests := []struct {
		name  string
		query string
	}{
		{"Fav が 1 枚もない", "fav=1"},
		{"一致しない全文検索", "q=nomatch"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Given
			e := newTestEnv(t)

			// When
			raw := e.rawJSON(t, "/api/facets?"+tt.query)

			// Then: 画面が長さを読めるよう、null ではなく [] で返す
			for _, key := range facetKeys {
				if got := string(raw[key]); got != "[]" {
					t.Errorf("%s = %s, want []", key, got)
				}
			}
		})
	}
}

func TestFacets_LoRAを持つ画像がなければloraを空配列で返す(t *testing.T) {
	// Given: 00002.png だけに絞る。この画像は LoRA を持たない
	e := newTestEnv(t)

	// When
	raw := e.rawJSON(t, "/api/facets?model=modelA&sampler=DPM%2B%2B+2M")

	// Then
	if got := string(raw["loras"]); got != "[]" {
		t.Errorf("loras = %s, want []", got)
	}
	if got := string(raw["models"]); got == "[]" || got == "null" {
		t.Errorf("models = %s, 候補が入っていてほしい", got)
	}
}

func TestList_該当がなければ一覧を空配列で返す(t *testing.T) {
	tests := []struct {
		name   string
		target string
		key    string
	}{
		{"画像の一覧", "/api/images?q=nomatch", "images"},
		{"Fav の一覧", "/api/images?fav=1", "images"},
		{"ゴミ箱の一覧", "/api/trash", "images"},
		{"状態のゴミ箱の件数", "/api/status", "trash"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newTestEnv(t)
			if got := string(e.rawJSON(t, tt.target)[tt.key]); got != "[]" {
				t.Errorf("GET %s の %s = %s, want []", tt.target, tt.key, got)
			}
		})
	}
}

func TestTags_該当がなければ空配列で返す(t *testing.T) {
	// Given
	e := newTestEnv(t)

	// When
	rec := e.get(t, "/api/tags?q=nomatch")

	// Then
	if got := rec.Body.String(); got != "[]\n" {
		t.Errorf("GET /api/tags = %q, want []", got)
	}
}
