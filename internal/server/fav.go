package server

import (
	"errors"
	"net/http"
	"time"

	"github.com/yuanying/sd-viewer/internal/index"
	"github.com/yuanying/sd-viewer/internal/trash"
)

// handleFav は指定された画像を Fav にする。
func (s *Server) handleFav(w http.ResponseWriter, r *http.Request) {
	s.setFav(w, r, true)
}

// handleUnfav は指定された画像を Fav から外す。
func (s *Server) handleUnfav(w http.ResponseWriter, r *http.Request) {
	s.setFav(w, r, false)
}

// setFav は本文の ID をまとめて Fav の状態にする。
// 結果の形はゴミ箱の操作と揃え、1 件の失敗で全体を止めない。
func (s *Server) setFav(w http.ResponseWriter, r *http.Request, fav bool) {
	ids, ok := decodeIDs(w, r)
	if !ok {
		return
	}
	now := time.Now()
	var res trash.Result
	for _, id := range ids {
		err := s.db.SetFav(r.Context(), id, fav, now)
		switch {
		case errors.Is(err, index.ErrNotFound):
			res.Failed = append(res.Failed, trash.Failure{ID: id, Reason: "画像が見つかりません"})
		case err != nil:
			s.log.Error("cannot set fav", "id", id, "error", err)
			res.Failed = append(res.Failed, trash.Failure{ID: id, Reason: "Fav を変更できませんでした"})
		default:
			res.Done++
		}
	}
	writeJSON(w, res)
}
