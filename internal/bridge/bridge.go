// Package bridge は生成情報を Stable Diffusion WebUI へ送り込む。
//
// 送り先は WebUI 側に入れた拡張 sd-viewer-bridge で、
// 受け取った生成情報を txt2img / img2img の入力欄へ反映する。
package bridge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// 送り先のタブ。
const (
	TargetTxt2Img = "txt2img"
	TargetImg2Img = "img2img"
)

// sendPath は WebUI 拡張が待ち受けるパス。
const sendPath = "/sd-viewer-bridge/send"

var (
	// ErrBadTarget は送り先のタブ名が正しくないことを表す。
	ErrBadTarget = errors.New("unknown target")
	// ErrNoExtension は WebUI 側に拡張が入っていないことを表す。
	ErrNoExtension = errors.New("sd-viewer-bridge extension is not installed in the WebUI")
)

// Request は WebUI へ送る 1 件分の内容。
type Request struct {
	// Target は反映先のタブ。
	Target string `json:"target"`
	// Infotext は PNG に埋め込まれていた生成情報そのまま。
	Infotext string `json:"infotext"`
	// ImagePath は元画像の位置。img2img の初期画像に使う。
	// sd-viewer と WebUI は同じホストで動く前提。
	ImagePath string `json:"image_path,omitempty"`
}

// Client は WebUI 拡張への送信を担う。
type Client struct {
	base string
	http *http.Client
}

// New は WebUI の URL を指定してクライアントを作る。
func New(rawURL string) (*Client, error) {
	if rawURL == "" {
		return nil, errors.New("empty WebUI url")
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("cannot parse WebUI url %q: %w", rawURL, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("WebUI url %q must start with http:// or https://", rawURL)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("WebUI url %q has no host", rawURL)
	}
	return &Client{
		base: strings.TrimRight(rawURL, "/"),
		http: &http.Client{Timeout: 10 * time.Second},
	}, nil
}

// URL は送り先の WebUI を返す。
func (c *Client) URL() string { return c.base }

// Send は生成情報を WebUI へ送る。
func (c *Client) Send(ctx context.Context, req Request) error {
	if req.Target != TargetTxt2Img && req.Target != TargetImg2Img {
		return fmt.Errorf("%w: %q", ErrBadTarget, req.Target)
	}

	body, err := json.Marshal(req)
	if err != nil {
		return err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+sendPath, bytes.NewReader(body))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	res, err := c.http.Do(httpReq)
	if err != nil {
		return fmt.Errorf("cannot reach the WebUI at %s: %w", c.base, err)
	}
	defer res.Body.Close()

	if res.StatusCode == http.StatusNotFound {
		return ErrNoExtension
	}
	if res.StatusCode >= 300 {
		// 原因が追えるように応答の中身も持たせる。
		detail, _ := io.ReadAll(io.LimitReader(res.Body, 512))
		return fmt.Errorf("WebUI returned %s: %s", res.Status, strings.TrimSpace(string(detail)))
	}
	return nil
}
