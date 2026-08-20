package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yuanying/sd-viewer/internal/bridge"
	"github.com/yuanying/sd-viewer/internal/index"
	"github.com/yuanying/sd-viewer/internal/scanner"
	"github.com/yuanying/sd-viewer/internal/thumb"
)

// fakeSender は WebUI の代わりに受け取った内容を覚えておく。
type fakeSender struct {
	got  []bridge.Request
	fail error
}

func (f *fakeSender) Send(_ context.Context, req bridge.Request) error {
	if f.fail != nil {
		return f.fail
	}
	f.got = append(f.got, req)
	return nil
}

// newSendEnv は送信先を差し替えたテスト環境を作る。
func newSendEnv(t *testing.T, sender Sender) *testEnv {
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

	env := &testEnv{db: db, dir: dir, ids: map[string]int64{}}
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
		WebUI:  sender,
	})
	return env
}

// post は JSON の本文を付けてリクエストを送る。
func (e *testEnv) post(t *testing.T, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	e.server.ServeHTTP(rec, req)
	return rec
}

func TestSend_生成情報と画像の位置をWebUIへ渡す(t *testing.T) {
	tests := []struct {
		name   string
		target string
	}{
		{name: "txt2img へ送る", target: "txt2img"},
		{name: "img2img へ送る", target: "img2img"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Given: 生成情報を持つ画像
			sender := &fakeSender{}
			env := newSendEnv(t, sender)
			id := env.ids["txt2img/2026-08-13/00001.png"]

			// When: 送信を要求する
			rec := env.post(t, "/api/send", `{"id":`+itoa(id)+`,"target":"`+tt.target+`"}`)

			// Then: 受け付けられ、元のテキストと実ファイルの位置が渡る
			if rec.Code != http.StatusOK {
				t.Fatalf("POST /api/send = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
			}
			if len(sender.got) != 1 {
				t.Fatalf("送信回数 = %d, want 1", len(sender.got))
			}
			got := sender.got[0]
			if got.Target != tt.target {
				t.Errorf("Target = %q, want %q", got.Target, tt.target)
			}
			if want := "1girl, smile, long hair\nSteps: 28"; got.Infotext != want {
				t.Errorf("Infotext = %q, want %q", got.Infotext, want)
			}
			want := filepath.Join(env.dir, filepath.FromSlash("txt2img/2026-08-13/00001.png"))
			if got.ImagePath != want {
				t.Errorf("ImagePath = %q, want %q", got.ImagePath, want)
			}
		})
	}
}

func TestSend_生成情報がなくても画像の位置は渡す(t *testing.T) {
	// Given: パラメータが埋まっていない画像
	sender := &fakeSender{}
	env := newSendEnv(t, sender)
	id := env.ids["txt2img/2026-08-12/00002.png"]

	// When: img2img へ送る
	rec := env.post(t, "/api/send", `{"id":`+itoa(id)+`,"target":"img2img"}`)

	// Then: 画像だけでも送られる
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/send = %d, want 200 (body: %s)", rec.Code, rec.Body.String())
	}
	if len(sender.got) != 1 || sender.got[0].ImagePath == "" {
		t.Fatalf("送信内容 = %+v, want 画像の位置を含む 1 件", sender.got)
	}
}

func TestSend_送り先が未設定なら断る(t *testing.T) {
	// Given: WebUI の URL を渡していないサーバ
	env := newSendEnv(t, nil)
	id := env.ids["txt2img/2026-08-13/00001.png"]

	// When: 送信を要求する
	rec := env.post(t, "/api/send", `{"id":`+itoa(id)+`,"target":"txt2img"}`)

	// Then: 設定が要ると分かる誤りを返す
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("POST /api/send = %d, want 503 (body: %s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "--webui-url") {
		t.Errorf("body = %s, want それが --webui-url の設定だと分かる内容", rec.Body.String())
	}
}

func TestSend_受け付けられない要求を退ける(t *testing.T) {
	tests := []struct {
		name string
		body string
		want int
	}{
		{name: "本文が JSON でない", body: `not json`, want: http.StatusBadRequest},
		{name: "送り先が空", body: `{"id":1}`, want: http.StatusBadRequest},
		{name: "知らない送り先", body: `{"id":1,"target":"extras"}`, want: http.StatusBadRequest},
		{name: "ない画像", body: `{"id":99999,"target":"txt2img"}`, want: http.StatusNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Given: 送信先が設定されたサーバ
			sender := &fakeSender{}
			env := newSendEnv(t, sender)

			// When: 誤った要求を送る
			rec := env.post(t, "/api/send", tt.body)

			// Then: 送信されず、対応する誤りが返る
			if rec.Code != tt.want {
				t.Fatalf("POST /api/send = %d, want %d (body: %s)", rec.Code, tt.want, rec.Body.String())
			}
			if len(sender.got) != 0 {
				t.Errorf("送信回数 = %d, want 0", len(sender.got))
			}
		})
	}
}

func TestSend_拡張が入っていないことを伝える(t *testing.T) {
	// Given: 拡張のない WebUI
	sender := &fakeSender{fail: bridge.ErrNoExtension}
	env := newSendEnv(t, sender)
	id := env.ids["txt2img/2026-08-13/00001.png"]

	// When: 送信を要求する
	rec := env.post(t, "/api/send", `{"id":`+itoa(id)+`,"target":"txt2img"}`)

	// Then: WebUI 側の支度が足りないと分かる
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("POST /api/send = %d, want 502 (body: %s)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "extension") {
		t.Errorf("body = %s, want 拡張が要ると分かる内容", rec.Body.String())
	}
}

func TestSend_WebUIへ届かなければ失敗を返す(t *testing.T) {
	// Given: 応答しない WebUI
	sender := &fakeSender{fail: errors.New("connection refused")}
	env := newSendEnv(t, sender)
	id := env.ids["txt2img/2026-08-13/00001.png"]

	// When: 送信を要求する
	rec := env.post(t, "/api/send", `{"id":`+itoa(id)+`,"target":"txt2img"}`)

	// Then: 失敗として返る
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("POST /api/send = %d, want 502 (body: %s)", rec.Code, rec.Body.String())
	}
}

func TestStatus_WebUIへ送れるかどうかを伝える(t *testing.T) {
	tests := []struct {
		name   string
		sender Sender
		want   bool
	}{
		{name: "送り先があれば送れる", sender: &fakeSender{}, want: true},
		{name: "送り先がなければ送れない", sender: nil, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Given: 送信先の有無が異なるサーバ
			env := newSendEnv(t, tt.sender)

			// When: 状態を要求する
			var got Status
			env.getJSON(t, "/api/status", &got)

			// Then: 画面が送信ボタンを出せるかどうか判断できる
			if got.WebUI != tt.want {
				t.Errorf("WebUI = %v, want %v", got.WebUI, tt.want)
			}
		})
	}
}
