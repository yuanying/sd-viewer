package server

import (
	"net/http"
	"slices"
	"testing"

	"github.com/yuanying/sd-viewer/internal/index"
	"github.com/yuanying/sd-viewer/internal/trash"
)

// faved は画像を Fav にする。
func (e *testEnv) faved(t *testing.T, paths ...string) []int64 {
	t.Helper()
	ids := make([]int64, 0, len(paths))
	for _, p := range paths {
		ids = append(ids, e.ids[p])
	}
	var res trash.Result
	e.postJSON(t, "/api/fav", map[string]any{"ids": ids}, &res)
	if res.Done != len(ids) {
		t.Fatalf("POST /api/fav = %+v, want %d 件成功", res, len(ids))
	}
	return ids
}

// listPaths は一覧のパスを返す。
func (e *testEnv) listPaths(t *testing.T, target string) []string {
	t.Helper()
	var list index.SearchResult
	e.getJSON(t, target, &list)
	paths := make([]string, 0, len(list.Images))
	for _, img := range list.Images {
		paths = append(paths, img.Path)
	}
	return paths
}

func TestFav_Favにした画像だけを一覧できる(t *testing.T) {
	// Given
	e := newTestEnv(t)

	// When
	e.faved(t, second)

	// Then
	if got := e.listPaths(t, "/api/images?fav=1"); !slices.Equal(got, []string{second}) {
		t.Errorf("GET /api/images?fav=1 = %v, want [%s]", got, second)
	}
	if got := e.listPaths(t, "/api/images"); len(got) != 3 {
		t.Errorf("GET /api/images = %v, 条件なしなら全件", got)
	}
}

func TestFav_一覧と詳細にFavの日時が入る(t *testing.T) {
	// Given
	e := newTestEnv(t)
	ids := e.faved(t, first)

	// When
	var list index.SearchResult
	e.getJSON(t, "/api/images?fav=1", &list)
	var img index.Image
	e.getJSON(t, "/api/images/"+itoa(ids[0]), &img)

	// Then
	if len(list.Images) != 1 || list.Images[0].FavAt.IsZero() {
		t.Errorf("一覧の FavAt が入っていない: %+v", list.Images)
	}
	if img.FavAt.IsZero() {
		t.Error("詳細の FavAt が入っていない")
	}
}

func TestFav_解除するとFavの一覧から消える(t *testing.T) {
	// Given
	e := newTestEnv(t)
	ids := e.faved(t, first, second)

	// When
	var res trash.Result
	e.postJSON(t, "/api/fav/remove", map[string]any{"ids": ids[:1]}, &res)

	// Then
	if res.Done != 1 || len(res.Failed) != 0 {
		t.Fatalf("POST /api/fav/remove = %+v, want 1 件成功", res)
	}
	if got := e.listPaths(t, "/api/images?fav=1"); !slices.Equal(got, []string{second}) {
		t.Errorf("GET /api/images?fav=1 = %v, want [%s]", got, second)
	}
}

func TestFav_ファセットはFavの画像だけを数える(t *testing.T) {
	// Given: modelA の 2 枚のうち 1 枚を Fav にする
	e := newTestEnv(t)
	e.faved(t, first)

	// When
	var facets index.FacetSet
	e.getJSON(t, "/api/facets?fav=1", &facets)

	// Then
	if want := []index.FacetValue{{Value: "modelA", Count: 1}}; !slices.Equal(facets.Models, want) {
		t.Errorf("Models = %v, want %v", facets.Models, want)
	}
}

func TestFav_ほかの条件と組み合わせられる(t *testing.T) {
	// Given: modelA の 1 枚と modelB の 1 枚を Fav にする
	e := newTestEnv(t)
	e.faved(t, first, "img2img/2026-08-13/00003.png")

	tests := []struct {
		name  string
		query string
		want  []string
	}{
		{"モデル", "fav=1&model=modelB", []string{"img2img/2026-08-13/00003.png"}},
		{"全文検索", "fav=1&q=smile", []string{first}},
		{"ルート", "fav=1&root=out", []string{first, "img2img/2026-08-13/00003.png"}},
		{"ほかのルート", "fav=1&root=missing", []string{}},
		{"true も受け付ける", "fav=true", []string{first, "img2img/2026-08-13/00003.png"}},
		{"解釈できない値は指定なし", "fav=abc", []string{first, second, "img2img/2026-08-13/00003.png"}},
		{"0 は指定なし", "fav=0", []string{first, second, "img2img/2026-08-13/00003.png"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := e.listPaths(t, "/api/images?"+tt.query); !slices.Equal(got, tt.want) {
				t.Errorf("GET /api/images?%s = %v, want %v", tt.query, got, tt.want)
			}
		})
	}
}

func TestFav_ゴミ箱へ入れるとFavの一覧から消え戻すと残っている(t *testing.T) {
	// Given
	e := newTestEnv(t)
	ids := e.faved(t, first)

	// When
	e.dropped(t, first)

	// Then
	if got := e.listPaths(t, "/api/images?fav=1"); len(got) != 0 {
		t.Errorf("GET /api/images?fav=1 = %v, ゴミ箱の画像は出さない", got)
	}

	// When
	var res trash.Result
	e.postJSON(t, "/api/trash/restore", map[string]any{"ids": ids}, &res)

	// Then
	if got := e.listPaths(t, "/api/images?fav=1"); !slices.Equal(got, []string{first}) {
		t.Errorf("GET /api/images?fav=1 = %v, want [%s]", got, first)
	}
}

func TestFav_処理できなかった画像は理由つきで返す(t *testing.T) {
	// Given: 1 件は実在し、1 件は知らない ID
	e := newTestEnv(t)

	// When
	var res trash.Result
	e.postJSON(t, "/api/fav", map[string]any{"ids": []int64{e.ids[first], 9999}}, &res)

	// Then: できたところまでは進める
	if res.Done != 1 || len(res.Failed) != 1 {
		t.Fatalf("fav = %+v, want 1 件成功・1 件失敗", res)
	}
	if res.Failed[0].ID != 9999 || res.Failed[0].Reason == "" {
		t.Errorf("Failed = %+v, want ID 9999 と理由", res.Failed[0])
	}
}

func TestFav_壊れた指示は断る(t *testing.T) {
	tests := []struct {
		name   string
		target string
		body   any
		want   int
	}{
		{"ID がない", "/api/fav", map[string]any{}, http.StatusBadRequest},
		{"ID が空", "/api/fav", map[string]any{"ids": []int64{}}, http.StatusBadRequest},
		{"外す指示に ID がない", "/api/fav/remove", map[string]any{}, http.StatusBadRequest},
		{"本文が JSON でない", "/api/fav", "not json", http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := newTestEnv(t)
			if rec := e.send(t, tt.target, tt.body); rec.Code != tt.want {
				t.Errorf("POST %s = %d, want %d", tt.target, rec.Code, tt.want)
			}
		})
	}
}
