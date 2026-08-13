package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/yuanying/sd-viewer/internal/index"
	"github.com/yuanying/sd-viewer/internal/metadata"
	"github.com/yuanying/sd-viewer/internal/scanner"
	"github.com/yuanying/sd-viewer/internal/thumb"
)

var fixtureBase = time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)

// testEnv はサーバとその依存をまとめたテスト環境。
type testEnv struct {
	server *Server
	db     *index.DB
	dir    string
	ids    map[string]int64
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	return newEnv(t, nil)
}

// newEnv は静的ファイルの有無を選べるテスト環境を作る。
func newEnv(t *testing.T, static fs.FS) *testEnv {
	t.Helper()

	dir := t.TempDir()
	db, err := index.Open(filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatalf("index.Open() error = %v", err)
	}
	t.Cleanup(func() { db.Close() })

	thumbs, err := thumb.New(filepath.Join(t.TempDir(), "thumbs"), 128)
	if err != nil {
		t.Fatalf("thumb.New() error = %v", err)
	}

	env := &testEnv{
		db:  db,
		dir: dir,
		ids: map[string]int64{},
	}
	for _, img := range fixtureImages() {
		writePNG(t, filepath.Join(dir, filepath.FromSlash(img.Path)), img.Width, img.Height)
		if err := db.Put(context.Background(), img); err != nil {
			t.Fatalf("Put() error = %v", err)
		}
		env.ids[img.Path] = img.ID
	}

	env.server = New(Options{
		DB:     db,
		Thumbs: thumbs,
		Roots:  []scanner.Root{{Name: "out", Path: dir}},
		Static: static,
	})
	return env
}

func fixtureImages() []*index.Image {
	return []*index.Image{
		{
			Root: "out", Path: "txt2img/2026-08-13/00001.png",
			Size: 1024, ModTime: fixtureBase, CreatedAt: fixtureBase,
			Width: 64, Height: 96, HasParams: true,
			Prompt: "1girl, smile, long hair", Negative: "watermark",
			Model: "modelA", Sampler: "Euler a", Steps: 28, CFGScale: 5, Seed: 1,
			Loras:        []metadata.Lora{{Name: "style_v3", Weight: 0.8, Hash: "h1"}},
			PositiveTags: []string{"1girl", "smile", "long hair"},
			NegativeTags: []string{"watermark"},
			Extras:       map[string]string{"RNG": "CPU"},
			Raw:          "1girl, smile, long hair\nSteps: 28",
		},
		{
			Root: "out", Path: "txt2img/2026-08-12/00002.png",
			Size: 2048, ModTime: fixtureBase.AddDate(0, 0, -1), CreatedAt: fixtureBase.AddDate(0, 0, -1),
			Width: 64, Height: 64, HasParams: true,
			Prompt: "1girl, angry",
			Model:  "modelA", Sampler: "DPM++ 2M", Steps: 20,
			PositiveTags: []string{"1girl", "angry"},
		},
		{
			Root: "out", Path: "img2img/2026-08-13/00003.png",
			Size: 4096, ModTime: fixtureBase.AddDate(0, 0, -2), CreatedAt: fixtureBase.AddDate(0, 0, -2),
			Width: 96, Height: 64, HasParams: true,
			Prompt: "1boy, short hair",
			Model:  "modelB", Sampler: "Euler a", Steps: 30,
			Loras:        []metadata.Lora{{Name: "style_v3", Weight: 0.5}},
			PositiveTags: []string{"1boy", "short hair"},
		},
	}
}

// writePNG はテスト用の実データとして PNG を書き出す。
func writePNG(t *testing.T, path string, width, height int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := range height {
		for x := range width {
			img.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 200, A: 255})
		}
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}

func (e *testEnv) get(t *testing.T, target string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	e.server.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec
}

// getJSON はリクエストを送り、JSON レスポンスを v へ読み込む。
func (e *testEnv) getJSON(t *testing.T, target string, v any) *httptest.ResponseRecorder {
	t.Helper()
	rec := e.get(t, target)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s = %d, want 200 (body: %s)", target, rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), v); err != nil {
		t.Fatalf("decode response of %s: %v (body: %s)", target, err, rec.Body.String())
	}
	return rec
}

func TestImages_検索条件を解釈して一覧を返す(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  []string
	}{
		{
			name:  "条件がなければ新しい順に全件返す",
			query: "",
			want: []string{
				"txt2img/2026-08-13/00001.png",
				"txt2img/2026-08-12/00002.png",
				"img2img/2026-08-13/00003.png",
			},
		},
		{
			name:  "全文検索の語で絞り込む",
			query: "q=smile",
			want:  []string{"txt2img/2026-08-13/00001.png"},
		},
		{
			name:  "モデルで絞り込む",
			query: "model=modelB",
			want:  []string{"img2img/2026-08-13/00003.png"},
		},
		{
			name:  "同じ項目を複数指定すると OR になる",
			query: "model=modelA&model=modelB",
			want: []string{
				"txt2img/2026-08-13/00001.png",
				"txt2img/2026-08-12/00002.png",
				"img2img/2026-08-13/00003.png",
			},
		},
		{
			name:  "LoRA で絞り込む",
			query: "lora=style_v3",
			want: []string{
				"txt2img/2026-08-13/00001.png",
				"img2img/2026-08-13/00003.png",
			},
		},
		{
			name:  "サンプラーで絞り込む",
			query: "sampler=DPM%2B%2B+2M",
			want:  []string{"txt2img/2026-08-12/00002.png"},
		},
		{
			name:  "解像度で絞り込む",
			query: "size=64x64",
			want:  []string{"txt2img/2026-08-12/00002.png"},
		},
		{
			name:  "ディレクトリで絞り込む",
			query: "dir=txt2img",
			want: []string{
				"txt2img/2026-08-13/00001.png",
				"txt2img/2026-08-12/00002.png",
			},
		},
		{
			name:  "タグで絞り込む",
			query: "tag=1girl&tag=smile",
			want:  []string{"txt2img/2026-08-13/00001.png"},
		},
		{
			name:  "除外タグを指定する",
			query: "tag=1girl&exclude_tag=angry",
			want:  []string{"txt2img/2026-08-13/00001.png"},
		},
		{
			name:  "日付で絞り込む",
			query: "from=2026-08-12&to=2026-08-14",
			want: []string{
				"txt2img/2026-08-13/00001.png",
				"txt2img/2026-08-12/00002.png",
			},
		},
		{
			name:  "古い順に並べ替える",
			query: "sort=oldest",
			want: []string{
				"img2img/2026-08-13/00003.png",
				"txt2img/2026-08-12/00002.png",
				"txt2img/2026-08-13/00001.png",
			},
		},
		{
			name:  "ルートで絞り込む",
			query: "root=out&model=modelB",
			want:  []string{"img2img/2026-08-13/00003.png"},
		},
		{
			name:  "空の値は条件として扱わない",
			query: "model=&q=&tag=",
			want: []string{
				"txt2img/2026-08-13/00001.png",
				"txt2img/2026-08-12/00002.png",
				"img2img/2026-08-13/00003.png",
			},
		},
	}

	env := newTestEnv(t)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Given: 登録済みの画像群
			// When: 条件をクエリ文字列で渡す
			var got index.SearchResult
			env.getJSON(t, "/api/images?"+tt.query, &got)

			// Then: 期待した画像が返る
			var paths []string
			for _, img := range got.Images {
				paths = append(paths, img.Path)
			}
			if !slices.Equal(paths, tt.want) {
				t.Errorf("paths = %v, want %v", paths, tt.want)
			}
		})
	}
}

func TestImages_ページングの指定を解釈する(t *testing.T) {
	tests := []struct {
		name      string
		query     string
		wantCount int
		wantTotal int
	}{
		{name: "件数を指定する", query: "limit=2", wantCount: 2, wantTotal: 3},
		{name: "位置を指定する", query: "limit=2&offset=2", wantCount: 1, wantTotal: 3},
		{name: "範囲を超えた位置", query: "limit=2&offset=10", wantCount: 0, wantTotal: 3},
		{name: "指定がなければ既定の件数", query: "", wantCount: 3, wantTotal: 3},
		{name: "解釈できない値は既定として扱う", query: "limit=abc&offset=-5", wantCount: 3, wantTotal: 3},
	}

	env := newTestEnv(t)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Given: 3 枚の画像
			// When: ページングを指定する
			var got index.SearchResult
			env.getJSON(t, "/api/images?"+tt.query, &got)

			// Then: 総件数は全体、返る件数は指定どおりになる
			if got.Total != tt.wantTotal {
				t.Errorf("Total = %d, want %d", got.Total, tt.wantTotal)
			}
			if len(got.Images) != tt.wantCount {
				t.Errorf("len(Images) = %d, want %d", len(got.Images), tt.wantCount)
			}
		})
	}
}

func TestImage_1枚の詳細を返す(t *testing.T) {
	// Given: 登録済みの画像
	env := newTestEnv(t)
	id := env.ids["txt2img/2026-08-13/00001.png"]

	// When: ID を指定して取得する
	var got index.Image
	env.getJSON(t, "/api/images/"+itoa(id), &got)

	// Then: 一覧では省かれる情報まで含めて返る
	if got.Prompt != "1girl, smile, long hair" {
		t.Errorf("Prompt = %q", got.Prompt)
	}
	if !slices.Equal(got.PositiveTags, []string{"1girl", "smile", "long hair"}) {
		t.Errorf("PositiveTags = %v", got.PositiveTags)
	}
	if got.Extras["RNG"] != "CPU" {
		t.Errorf("Extras = %v", got.Extras)
	}
	if got.Raw == "" {
		t.Error("Raw is empty")
	}
	if len(got.Loras) != 1 || got.Loras[0].Name != "style_v3" {
		t.Errorf("Loras = %#v", got.Loras)
	}
}

func TestServer_見つからない要求にはエラーを返す(t *testing.T) {
	tests := []struct {
		name     string
		target   string
		wantCode int
	}{
		{name: "存在しない画像の詳細", target: "/api/images/9999", wantCode: http.StatusNotFound},
		{name: "解釈できない ID", target: "/api/images/abc", wantCode: http.StatusBadRequest},
		{name: "存在しない画像のサムネイル", target: "/api/thumb/9999", wantCode: http.StatusNotFound},
		{name: "存在しない画像の原寸", target: "/api/raw/9999", wantCode: http.StatusNotFound},
		{name: "存在しない API", target: "/api/unknown", wantCode: http.StatusNotFound},
	}

	env := newTestEnv(t)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Given: 実行中のサーバ
			// When: 存在しない対象を要求する
			rec := env.get(t, tt.target)

			// Then: 対応する状態コードが返る
			if rec.Code != tt.wantCode {
				t.Errorf("code = %d, want %d", rec.Code, tt.wantCode)
			}
		})
	}
}

func TestFacets_絞り込み候補を返す(t *testing.T) {
	tests := []struct {
		name  string
		query string
		pick  func(index.FacetSet) []index.FacetValue
		want  []index.FacetValue
	}{
		{
			name:  "モデルの候補を件数つきで返す",
			query: "",
			pick:  func(f index.FacetSet) []index.FacetValue { return f.Models },
			want:  []index.FacetValue{{Value: "modelA", Count: 2}, {Value: "modelB", Count: 1}},
		},
		{
			name:  "絞り込み後の集合で LoRA を数える",
			query: "model=modelB",
			pick:  func(f index.FacetSet) []index.FacetValue { return f.Loras },
			want:  []index.FacetValue{{Value: "style_v3", Count: 1}},
		},
		{
			name:  "ルートの候補を返す",
			query: "",
			pick:  func(f index.FacetSet) []index.FacetValue { return f.Roots },
			want:  []index.FacetValue{{Value: "out", Count: 3}},
		},
	}

	env := newTestEnv(t)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Given: 登録済みの画像群
			// When: 絞り込み条件つきでファセットを求める
			var got index.FacetSet
			env.getJSON(t, "/api/facets?"+tt.query, &got)

			// Then: 候補と件数が返る
			if !slices.Equal(tt.pick(got), tt.want) {
				t.Errorf("facet = %#v, want %#v", tt.pick(got), tt.want)
			}
		})
	}
}

func TestTags_補完候補を返す(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  []index.TagCount
	}{
		{
			name:  "入力に一致する候補を出現数の多い順に返す",
			query: "q=1",
			want:  []index.TagCount{{Tag: "1girl", Count: 2}, {Tag: "1boy", Count: 1}},
		},
		{
			name:  "件数の上限を指定できる",
			query: "q=1&limit=1",
			want:  []index.TagCount{{Tag: "1girl", Count: 2}},
		},
		{
			name:  "一致しなければ空で返す",
			query: "q=unicorn",
			want:  nil,
		},
	}

	env := newTestEnv(t)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Given: 登録済みの画像群
			// When: 入力中の文字列を渡す
			var got []index.TagCount
			env.getJSON(t, "/api/tags?"+tt.query, &got)

			// Then: 候補が返る
			if !slices.Equal(got, tt.want) {
				t.Errorf("tags = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestThumb_サムネイルを配信する(t *testing.T) {
	// Given: 登録済みの画像
	env := newTestEnv(t)
	id := env.ids["txt2img/2026-08-13/00001.png"]

	// When: サムネイルを要求する
	rec := env.get(t, "/api/thumb/"+itoa(id))

	// Then: JPEG が返る
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "image/jpeg" {
		t.Errorf("Content-Type = %q, want image/jpeg", ct)
	}
	if !bytes.HasPrefix(rec.Body.Bytes(), []byte{0xff, 0xd8}) {
		t.Error("body is not a JPEG")
	}
}

func TestRaw_原寸の画像を配信する(t *testing.T) {
	// Given: 登録済みの画像
	env := newTestEnv(t)
	id := env.ids["txt2img/2026-08-13/00001.png"]

	// When: 原寸の画像を要求する
	rec := env.get(t, "/api/raw/"+itoa(id))

	// Then: PNG がそのまま返る
	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "image/png") {
		t.Errorf("Content-Type = %q, want image/png", ct)
	}
	if !bytes.HasPrefix(rec.Body.Bytes(), []byte{0x89, 'P', 'N', 'G'}) {
		t.Error("body is not a PNG")
	}
}

func TestRaw_インデックスにあってもファイルがなければ見つからないと返す(t *testing.T) {
	// Given: 実体を消した画像
	env := newTestEnv(t)
	rel := "txt2img/2026-08-13/00001.png"
	if err := os.Remove(filepath.Join(env.dir, filepath.FromSlash(rel))); err != nil {
		t.Fatal(err)
	}

	// When: 原寸の画像を要求する
	rec := env.get(t, "/api/raw/"+itoa(env.ids[rel]))

	// Then: 404 が返る
	if rec.Code != http.StatusNotFound {
		t.Errorf("code = %d, want 404", rec.Code)
	}
}

func TestStatus_インデックスの状態を返す(t *testing.T) {
	// Given: 登録済みの画像群
	env := newTestEnv(t)

	// When: 状態を要求する
	var got Status
	env.getJSON(t, "/api/status", &got)

	// Then: 総枚数とルートが返る
	if got.Total != 3 {
		t.Errorf("Total = %d, want 3", got.Total)
	}
	if !slices.Equal(got.Roots, []string{"out"}) {
		t.Errorf("Roots = %v, want [out]", got.Roots)
	}
}

func TestEvents_状態を継続して配信する(t *testing.T) {
	// Given: 実行中のサーバ
	env := newTestEnv(t)
	srv := httptest.NewServer(env.server)
	defer srv.Close()

	// When: イベントストリームへ接続する
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+"/api/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	// Then: 接続直後に現在の状態が届く
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type = %q, want text/event-stream", ct)
	}
	line, err := bufio.NewReader(resp.Body).ReadString('\n')
	if err != nil {
		t.Fatalf("read event: %v", err)
	}
	payload, ok := strings.CutPrefix(strings.TrimSpace(line), "data: ")
	if !ok {
		t.Fatalf("unexpected event line: %q", line)
	}
	var got Status
	if err := json.Unmarshal([]byte(payload), &got); err != nil {
		t.Fatalf("decode event: %v", err)
	}
	if got.Total != 3 {
		t.Errorf("Total = %d, want 3", got.Total)
	}
}

func TestStatic_埋め込んだ画面を配信する(t *testing.T) {
	tests := []struct {
		name     string
		target   string
		wantBody string
	}{
		{name: "トップページ", target: "/", wantBody: "<html>viewer</html>"},
		{name: "静的ファイル", target: "/assets/app.js", wantBody: "console.log(1)"},
		{name: "画面側のルーティングは index を返す", target: "/images/123", wantBody: "<html>viewer</html>"},
	}

	env := newEnv(t, fstest.MapFS{
		"index.html":    {Data: []byte("<html>viewer</html>")},
		"assets/app.js": {Data: []byte("console.log(1)")},
	})
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Given: 埋め込んだ画面を持つサーバ
			// When: 画面を要求する
			rec := env.get(t, tt.target)

			// Then: 対応する内容が返る
			if rec.Code != http.StatusOK {
				t.Fatalf("code = %d, want 200", rec.Code)
			}
			if got := strings.TrimSpace(rec.Body.String()); got != tt.wantBody {
				t.Errorf("body = %q, want %q", got, tt.wantBody)
			}
		})
	}
}

func itoa(id int64) string {
	return strconv.FormatInt(id, 10)
}
