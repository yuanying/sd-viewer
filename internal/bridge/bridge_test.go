package bridge

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSendPostsRequestToWebUI(t *testing.T) {
	var (
		gotPath   string
		gotMethod string
		gotBody   Request
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &gotBody)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c, err := New(srv.URL)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	req := Request{Target: TargetImg2Img, Infotext: "a, b\nSteps: 20", ImagePath: "/out/a.png"}
	if err := c.Send(context.Background(), req); err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotPath != sendPath {
		t.Errorf("path = %q, want %q", gotPath, sendPath)
	}
	if gotBody != req {
		t.Errorf("body = %+v, want %+v", gotBody, req)
	}
}

// 末尾のスラッシュが付いていても同じ宛先になる。
func TestNewTrimsTrailingSlash(t *testing.T) {
	c, err := New("http://127.0.0.1:7860/")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if want := "http://127.0.0.1:7860"; c.base != want {
		t.Errorf("base = %q, want %q", c.base, want)
	}
}

func TestNewRejectsBadURL(t *testing.T) {
	for _, raw := range []string{"", "127.0.0.1:7860", "ftp://host"} {
		if _, err := New(raw); err == nil {
			t.Errorf("New(%q) error = nil, want error", raw)
		}
	}
}

func TestSendRejectsUnknownTarget(t *testing.T) {
	c, err := New("http://127.0.0.1:7860")
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	err = c.Send(context.Background(), Request{Target: "extras", Infotext: "x"})
	if !errors.Is(err, ErrBadTarget) {
		t.Errorf("Send() error = %v, want ErrBadTarget", err)
	}
}

// 拡張が入っていない WebUI は 404 を返す。原因が分かる誤りにする。
func TestSendReportsMissingExtension(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	c, _ := New(srv.URL)
	err := c.Send(context.Background(), Request{Target: TargetTxt2Img, Infotext: "x"})
	if !errors.Is(err, ErrNoExtension) {
		t.Fatalf("Send() error = %v, want ErrNoExtension", err)
	}
}

func TestSendReportsServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		io.WriteString(w, "boom")
	}))
	defer srv.Close()

	c, _ := New(srv.URL)
	err := c.Send(context.Background(), Request{Target: TargetTxt2Img, Infotext: "x"})
	if err == nil {
		t.Fatal("Send() error = nil, want error")
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Errorf("Send() error = %v, want it to carry the response body", err)
	}
}
