package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/yuanying/sd-viewer/internal/index"
	"github.com/yuanying/sd-viewer/internal/trash"
)

const (
	first  = "txt2img/2026-08-13/00001.png"
	second = "txt2img/2026-08-12/00002.png"
)

// send は本文を JSON へ直してリクエストを送る。
func (e *testEnv) send(t *testing.T, target string, body any) *httptest.ResponseRecorder {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	return e.post(t, target, string(b))
}

// postJSON は JSON を送り、応答を v へ読み込む。200 でなければ止まる。
func (e *testEnv) postJSON(t *testing.T, target string, body, v any) {
	t.Helper()
	rec := e.send(t, target, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("POST %s = %d, want 200 (body: %s)", target, rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), v); err != nil {
		t.Fatalf("decode response of %s: %v (body: %s)", target, err, rec.Body.String())
	}
}

// dropped は ID の画像をゴミ箱へ入れる。
func (e *testEnv) dropped(t *testing.T, paths ...string) []int64 {
	t.Helper()
	ids := make([]int64, 0, len(paths))
	for _, p := range paths {
		ids = append(ids, e.ids[p])
	}
	var res trash.Result
	e.postJSON(t, "/api/trash", map[string]any{"ids": ids}, &res)
	if res.Done != len(ids) {
		t.Fatalf("POST /api/trash = %+v, want %d 件成功", res, len(ids))
	}
	return ids
}

func (e *testEnv) exists(rel string) bool {
	_, err := os.Stat(filepath.Join(e.dir, filepath.FromSlash(rel)))
	return err == nil
}

func TestTrash_選んだ画像をゴミ箱へ入れると一覧から消える(t *testing.T) {
	// Given
	e := newTestEnv(t)

	// When
	ids := e.dropped(t, first)

	// Then: 一覧に出ず、実ファイルはゴミ箱の中にある
	var list index.SearchResult
	e.getJSON(t, "/api/images", &list)
	if list.Total != 2 {
		t.Errorf("Total = %d, want 2", list.Total)
	}
	if e.exists(first) {
		t.Error("元の場所にファイルが残っている")
	}
	if !e.exists(".trash/" + first) {
		t.Error("ゴミ箱にファイルがない")
	}

	// Then: ゴミ箱の一覧には出る
	var bin index.SearchResult
	e.getJSON(t, "/api/trash", &bin)
	if bin.Total != 1 {
		t.Fatalf("ゴミ箱の件数 = %d, want 1", bin.Total)
	}
	if bin.Images[0].ID != ids[0] {
		t.Errorf("ID = %d, want %d", bin.Images[0].ID, ids[0])
	}
	if bin.Images[0].OrigPath != first {
		t.Errorf("OrigPath = %q, want %q", bin.Images[0].OrigPath, first)
	}
}

func TestTrash_ゴミ箱の画像はファセットにも出てこない(t *testing.T) {
	// Given: modelA の 2 枚のうち 1 枚をゴミ箱へ入れる
	e := newTestEnv(t)
	e.dropped(t, first)

	// When
	var facets index.FacetSet
	e.getJSON(t, "/api/facets", &facets)

	// Then
	for _, v := range facets.Models {
		if v.Value == "modelA" && v.Count != 1 {
			t.Errorf("modelA の件数 = %d, want 1", v.Count)
		}
	}
}

func TestTrash_ゴミ箱の画像もサムネイルと原寸を配れる(t *testing.T) {
	// Given
	e := newTestEnv(t)
	ids := e.dropped(t, first)

	// When / Then
	for _, path := range []string{"/api/thumb/", "/api/raw/"} {
		target := path + itoa(ids[0])
		if rec := e.get(t, target); rec.Code != http.StatusOK {
			t.Errorf("GET %s = %d, want 200", target, rec.Code)
		}
	}
}

func TestTrash_ルートを指定してゴミ箱を絞り込む(t *testing.T) {
	// Given
	e := newTestEnv(t)
	e.dropped(t, first, second)

	// When
	var bin index.SearchResult
	e.getJSON(t, "/api/trash?root=missing", &bin)

	// Then
	if bin.Total != 0 {
		t.Errorf("Total = %d, want 0", bin.Total)
	}
	e.getJSON(t, "/api/trash?root=out", &bin)
	if bin.Total != 2 {
		t.Errorf("Total = %d, want 2", bin.Total)
	}
}

func TestTrash_状態にルートごとのゴミ箱の件数が入る(t *testing.T) {
	// Given
	e := newTestEnv(t)
	e.dropped(t, first, second)

	// When
	var status Status
	e.getJSON(t, "/api/status", &status)

	// Then
	if status.Total != 1 {
		t.Errorf("Total = %d, ゴミ箱の分まで数えている", status.Total)
	}
	want := []index.TrashCount{{Root: "out", Count: 2}}
	if len(status.Trash) != 1 || status.Trash[0] != want[0] {
		t.Errorf("Trash = %v, want %v", status.Trash, want)
	}
}

func TestRestore_元に戻すと再び一覧に出る(t *testing.T) {
	// Given
	e := newTestEnv(t)
	ids := e.dropped(t, first)

	// When
	var res trash.Result
	e.postJSON(t, "/api/trash/restore", map[string]any{"ids": ids}, &res)

	// Then
	if res.Done != 1 || len(res.Failed) != 0 {
		t.Fatalf("restore = %+v, want 1 件成功", res)
	}
	if !e.exists(first) {
		t.Error("元の場所にファイルが戻っていない")
	}
	var list index.SearchResult
	e.getJSON(t, "/api/images", &list)
	if list.Total != 3 {
		t.Errorf("Total = %d, want 3", list.Total)
	}
}

func TestPurge_完全に削除するとファイルごと消える(t *testing.T) {
	// Given
	e := newTestEnv(t)
	ids := e.dropped(t, first)

	// When
	var res trash.Result
	e.postJSON(t, "/api/trash/purge", map[string]any{"ids": ids}, &res)

	// Then
	if res.Done != 1 {
		t.Fatalf("purge = %+v, want 1 件成功", res)
	}
	if e.exists(".trash/" + first) {
		t.Error("ファイルが残っている")
	}
	if _, err := e.db.Get(context.Background(), ids[0]); err == nil {
		t.Error("インデックスに行が残っている")
	}
}

func TestEmpty_ゴミ箱を空にする(t *testing.T) {
	// Given
	e := newTestEnv(t)
	e.dropped(t, first, second)

	// When
	var res trash.Result
	e.postJSON(t, "/api/trash/empty", map[string]any{"root": "out"}, &res)

	// Then
	if res.Done != 2 || len(res.Failed) != 0 {
		t.Fatalf("empty = %+v, want 2 件成功", res)
	}
	var bin index.SearchResult
	e.getJSON(t, "/api/trash", &bin)
	if bin.Total != 0 {
		t.Errorf("ゴミ箱の件数 = %d, want 0", bin.Total)
	}
}

func TestTrash_処理できなかった画像は理由つきで返す(t *testing.T) {
	// Given: 1 件は実在し、1 件は知らない ID
	e := newTestEnv(t)

	// When
	var res trash.Result
	e.postJSON(t, "/api/trash", map[string]any{"ids": []int64{e.ids[first], 9999}}, &res)

	// Then: できたところまでは進める
	if res.Done != 1 || len(res.Failed) != 1 {
		t.Fatalf("trash = %+v, want 1 件成功・1 件失敗", res)
	}
	if res.Failed[0].ID != 9999 || res.Failed[0].Reason == "" {
		t.Errorf("Failed = %+v, want ID 9999 と理由", res.Failed[0])
	}
}

func TestTrash_壊れた指示は断る(t *testing.T) {
	tests := []struct {
		name   string
		target string
		body   any
		want   int
	}{
		{"ID がない", "/api/trash", map[string]any{}, http.StatusBadRequest},
		{"ID が空", "/api/trash", map[string]any{"ids": []int64{}}, http.StatusBadRequest},
		{"戻す指示に ID がない", "/api/trash/restore", map[string]any{}, http.StatusBadRequest},
		{"消す指示に ID がない", "/api/trash/purge", map[string]any{}, http.StatusBadRequest},
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

func TestEmpty_ルートを省くとすべてのゴミ箱を空にする(t *testing.T) {
	// Given
	e := newTestEnv(t)
	e.dropped(t, first, second)

	// When
	var res trash.Result
	e.postJSON(t, "/api/trash/empty", map[string]any{}, &res)

	// Then
	if res.Done != 2 {
		t.Fatalf("empty = %+v, want 2 件成功", res)
	}
}
