package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/yuanying/sd-viewer/internal/bridge"
	"github.com/yuanying/sd-viewer/internal/index"
)

// Sender は生成情報を WebUI へ届ける。
type Sender interface {
	Send(ctx context.Context, req bridge.Request) error
}

// sendRequest は画面から届く送信の指示。
type sendRequest struct {
	ID     int64  `json:"id"`
	Target string `json:"target"`
}

// handleSend は指定された画像の生成情報を WebUI の入力欄へ送り込む。
func (s *Server) handleSend(w http.ResponseWriter, r *http.Request) {
	if s.webui == nil {
		writeError(w, http.StatusServiceUnavailable,
			"WebUI への送信先が設定されていません。--webui-url を指定して起動してください")
		return
	}

	var req sendRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Target != bridge.TargetTxt2Img && req.Target != bridge.TargetImg2Img {
		writeError(w, http.StatusBadRequest, "target must be txt2img or img2img")
		return
	}

	img, err := s.db.Get(r.Context(), req.ID)
	if errors.Is(err, index.ErrNotFound) {
		writeError(w, http.StatusNotFound, "image not found")
		return
	}
	if err != nil {
		s.fail(w, "get image", err)
		return
	}

	abs, ok := s.resolve(img)
	if !ok {
		writeError(w, http.StatusNotFound, "image file not found")
		return
	}

	// 生成情報がなくても、画像そのものは img2img で使えるので送る。
	err = s.webui.Send(r.Context(), bridge.Request{
		Target:    req.Target,
		Infotext:  img.Raw,
		ImagePath: abs,
	})
	if errors.Is(err, bridge.ErrNoExtension) {
		writeError(w, http.StatusBadGateway,
			"WebUI に sd-viewer-bridge extension が入っていません")
		return
	}
	if err != nil {
		s.log.Error("cannot send to WebUI", "target", req.Target, "id", req.ID, "error", err)
		writeError(w, http.StatusBadGateway, "WebUI へ送れませんでした: "+err.Error())
		return
	}

	writeJSON(w, map[string]string{"target": req.Target})
}
