package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/yuanying/sd-viewer/internal/index"
	"github.com/yuanying/sd-viewer/internal/trash"
)

// Bin はゴミ箱への出し入れ。
type Bin interface {
	Move(ctx context.Context, ids []int64) trash.Result
	Restore(ctx context.Context, ids []int64) trash.Result
	Purge(ctx context.Context, ids []int64) trash.Result
	Empty(ctx context.Context, root string) (trash.Result, error)
}

// idsRequest はまとめて処理する画像の指定。
type idsRequest struct {
	IDs []int64 `json:"ids"`
}

// emptyRequest は空にするゴミ箱の指定。root が空ならすべてのルート。
type emptyRequest struct {
	Root string `json:"root"`
}

// handleTrashList はゴミ箱の中身を返す。
func (s *Server) handleTrashList(w http.ResponseWriter, r *http.Request) {
	q := parseQuery(r)
	q.Trashed = true
	res, err := s.db.Search(r.Context(), q)
	if err != nil {
		s.fail(w, "list trash", err)
		return
	}
	if res.Images == nil {
		res.Images = []*index.Image{}
	}
	writeJSON(w, res)
}

// handleTrash は指定された画像をゴミ箱へ入れる。
func (s *Server) handleTrash(w http.ResponseWriter, r *http.Request) {
	s.applyToIDs(w, r, func(ctx context.Context, ids []int64) trash.Result {
		return s.trash.Move(ctx, ids)
	})
}

// handleRestore は指定された画像を元の場所へ戻す。
func (s *Server) handleRestore(w http.ResponseWriter, r *http.Request) {
	s.applyToIDs(w, r, func(ctx context.Context, ids []int64) trash.Result {
		return s.trash.Restore(ctx, ids)
	})
}

// handlePurge は指定された画像を完全に削除する。
func (s *Server) handlePurge(w http.ResponseWriter, r *http.Request) {
	s.applyToIDs(w, r, func(ctx context.Context, ids []int64) trash.Result {
		return s.trash.Purge(ctx, ids)
	})
}

// handleEmpty はゴミ箱を空にする。
func (s *Server) handleEmpty(w http.ResponseWriter, r *http.Request) {
	if !s.trashReady(w) {
		return
	}
	var req emptyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	res, err := s.trash.Empty(r.Context(), strings.TrimSpace(req.Root))
	if err != nil {
		s.fail(w, "empty trash", err)
		return
	}
	writeJSON(w, res)
}

// applyToIDs は本文の ID を読み、まとめた処理の結果を返す。
func (s *Server) applyToIDs(w http.ResponseWriter, r *http.Request, apply func(context.Context, []int64) trash.Result) {
	if !s.trashReady(w) {
		return
	}
	ids, ok := decodeIDs(w, r)
	if !ok {
		return
	}
	writeJSON(w, apply(r.Context(), ids))
}

// decodeIDs は本文から処理する画像の ID を読む。
// 読めない場合や空の場合は応答を書き終え、ok が false になる。
func decodeIDs(w http.ResponseWriter, r *http.Request) ([]int64, bool) {
	var req idsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return nil, false
	}
	if len(req.IDs) == 0 {
		writeError(w, http.StatusBadRequest, "ids を 1 つ以上指定してください")
		return nil, false
	}
	return req.IDs, true
}

// trashReady はゴミ箱を使えるかを確かめる。使えない場合は応答を書き終える。
func (s *Server) trashReady(w http.ResponseWriter) bool {
	if s.trash == nil {
		writeError(w, http.StatusServiceUnavailable, "ゴミ箱を使えません")
		return false
	}
	return true
}
