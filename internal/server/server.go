// Package server は画像の検索と配信を行う HTTP API と、
// 埋め込んだ画面の配信を担う。
package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/yuanying/sd-viewer/internal/index"
	"github.com/yuanying/sd-viewer/internal/scanner"
	"github.com/yuanying/sd-viewer/internal/thumb"
)

// Options はサーバの設定。
type Options struct {
	DB     *index.DB
	Thumbs *thumb.Cache
	// Roots は画像ファイルの置き場所。原寸画像の配信に使う。
	Roots []scanner.Root
	// Scanner は進捗の取得に使う。監視を行わない場合は nil でよい。
	Scanner *scanner.Scanner
	// Static は埋め込んだ画面。nil なら API だけを提供する。
	Static fs.FS
	// WebUI は生成情報の送り先。nil なら送信機能を提供しない。
	WebUI Sender
	// Trash はゴミ箱。nil ならゴミ箱の操作を提供しない。
	Trash  Bin
	Logger *slog.Logger
}

// Server は HTTP のハンドラ。
type Server struct {
	db     *index.DB
	thumbs *thumb.Cache
	scan   *scanner.Scanner
	roots  map[string]string
	static fs.FS
	webui  Sender
	trash  Bin
	log    *slog.Logger
	mux    *http.ServeMux
}

// Status はインデックスと監視の状態。
type Status struct {
	Total      int           `json:"total"`
	Roots      []string      `json:"roots"`
	Scan       scanner.Stats `json:"scan"`
	Thumbnails int64         `json:"thumbnails"`
	// WebUI は生成情報を WebUI へ送れるかどうか。
	WebUI bool `json:"webui"`
	// Trash はルートごとのゴミ箱の件数。
	Trash []index.TrashCount `json:"trash"`
}

// New はハンドラを組み立てる。
func New(opts Options) *Server {
	roots := make(map[string]string, len(opts.Roots))
	for _, r := range opts.Roots {
		roots[r.Name] = r.Path
	}
	log := opts.Logger
	if log == nil {
		log = slog.Default()
	}

	s := &Server{
		db:     opts.DB,
		thumbs: opts.Thumbs,
		scan:   opts.Scanner,
		roots:  roots,
		static: opts.Static,
		webui:  opts.WebUI,
		trash:  opts.Trash,
		log:    log,
		mux:    http.NewServeMux(),
	}

	s.mux.HandleFunc("GET /api/images", s.handleImages)
	s.mux.HandleFunc("GET /api/images/{id}", s.handleImage)
	s.mux.HandleFunc("GET /api/facets", s.handleFacets)
	s.mux.HandleFunc("GET /api/tags", s.handleTags)
	s.mux.HandleFunc("GET /api/thumb/{id}", s.handleThumb)
	s.mux.HandleFunc("GET /api/raw/{id}", s.handleRaw)
	s.mux.HandleFunc("GET /api/status", s.handleStatus)
	s.mux.HandleFunc("GET /api/events", s.handleEvents)
	s.mux.HandleFunc("POST /api/send", s.handleSend)
	s.mux.HandleFunc("GET /api/trash", s.handleTrashList)
	s.mux.HandleFunc("POST /api/trash", s.handleTrash)
	s.mux.HandleFunc("POST /api/trash/restore", s.handleRestore)
	s.mux.HandleFunc("POST /api/trash/purge", s.handlePurge)
	s.mux.HandleFunc("POST /api/trash/empty", s.handleEmpty)
	// 未知の API は画面ではなく 404 として扱う。
	s.mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not found")
	})
	s.mux.HandleFunc("/", s.handleStatic)

	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

func (s *Server) handleImages(w http.ResponseWriter, r *http.Request) {
	res, err := s.db.Search(r.Context(), parseQuery(r))
	if err != nil {
		s.fail(w, "search images", err)
		return
	}
	if res.Images == nil {
		res.Images = []*index.Image{}
	}
	writeJSON(w, res)
}

func (s *Server) handleImage(w http.ResponseWriter, r *http.Request) {
	img, ok := s.lookup(w, r)
	if !ok {
		return
	}
	writeJSON(w, img)
}

func (s *Server) handleFacets(w http.ResponseWriter, r *http.Request) {
	facets, err := s.db.Facets(r.Context(), parseQuery(r))
	if err != nil {
		s.fail(w, "aggregate facets", err)
		return
	}
	writeJSON(w, facets)
}

func (s *Server) handleTags(w http.ResponseWriter, r *http.Request) {
	limit := intParam(r, "limit", 20, 200)
	tags, err := s.db.TagSuggest(r.Context(), r.URL.Query().Get("q"), limit)
	if err != nil {
		s.fail(w, "suggest tags", err)
		return
	}
	if tags == nil {
		tags = []index.TagCount{}
	}
	writeJSON(w, tags)
}

func (s *Server) handleThumb(w http.ResponseWriter, r *http.Request) {
	img, ok := s.lookup(w, r)
	if !ok {
		return
	}
	abs, ok := s.resolve(img)
	if !ok {
		writeError(w, http.StatusNotFound, "image file not found")
		return
	}
	path, err := s.thumbs.Ensure(img.ID, abs)
	if err != nil {
		s.log.Warn("cannot build thumbnail", "path", abs, "error", err)
		writeError(w, http.StatusNotFound, "thumbnail not available")
		return
	}
	w.Header().Set("Cache-Control", "private, max-age=86400")
	http.ServeFile(w, r, path)
}

func (s *Server) handleRaw(w http.ResponseWriter, r *http.Request) {
	img, ok := s.lookup(w, r)
	if !ok {
		return
	}
	abs, ok := s.resolve(img)
	if !ok {
		writeError(w, http.StatusNotFound, "image file not found")
		return
	}
	w.Header().Set("Cache-Control", "private, max-age=86400")
	http.ServeFile(w, r, abs)
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	status, err := s.status(r)
	if err != nil {
		s.fail(w, "collect status", err)
		return
	}
	writeJSON(w, status)
}

// handleEvents は状態の変化を Server-Sent Events で流し続ける。
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	send := func() bool {
		status, err := s.status(r)
		if err != nil {
			return false
		}
		b, err := json.Marshal(status)
		if err != nil {
			return false
		}
		if _, err := fmt.Fprintf(w, "data: %s\n\n", b); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}
	if !send() {
		return
	}

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			if !send() {
				return
			}
		}
	}
}

func (s *Server) status(r *http.Request) (Status, error) {
	res, err := s.db.Search(r.Context(), index.Query{Limit: 1})
	if err != nil {
		return Status{}, err
	}
	status := Status{Total: res.Total, Roots: []string{}}
	for name := range s.roots {
		status.Roots = append(status.Roots, name)
	}
	slices.Sort(status.Roots)
	if s.scan != nil {
		status.Scan = s.scan.Stats()
	}
	if s.thumbs != nil {
		status.Thumbnails = s.thumbs.Generated()
	}
	status.WebUI = s.webui != nil

	counts, err := s.db.TrashCounts(r.Context())
	if err != nil {
		return Status{}, err
	}
	status.Trash = counts
	if status.Trash == nil {
		status.Trash = []index.TrashCount{}
	}
	return status, nil
}

// handleStatic は埋め込んだ画面を配信する。
// 見つからないパスは画面側のルーティングとみなして index.html を返す。
func (s *Server) handleStatic(w http.ResponseWriter, r *http.Request) {
	if s.static == nil {
		writeError(w, http.StatusNotFound, "not found")
		return
	}
	name := strings.TrimPrefix(r.URL.Path, "/")
	if name == "" {
		name = "index.html"
	}

	data, err := fs.ReadFile(s.static, name)
	if err != nil {
		name = "index.html"
		data, err = fs.ReadFile(s.static, name)
		if err != nil {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
	}
	http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(data))
}

// lookup はパスパラメータの ID から画像を取り出す。
// 応答を書き終えた場合は ok が false になる。
func (s *Server) lookup(w http.ResponseWriter, r *http.Request) (*index.Image, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid image id")
		return nil, false
	}
	img, err := s.db.Get(r.Context(), id)
	if errors.Is(err, index.ErrNotFound) {
		writeError(w, http.StatusNotFound, "image not found")
		return nil, false
	}
	if err != nil {
		s.fail(w, "get image", err)
		return nil, false
	}
	return img, true
}

// resolve は画像レコードから実ファイルの位置を求める。
func (s *Server) resolve(img *index.Image) (string, bool) {
	root, ok := s.roots[img.Root]
	if !ok {
		return "", false
	}
	abs := filepath.Join(root, filepath.FromSlash(img.Path))
	// ルートの外を指していないか確かめる。
	if !strings.HasPrefix(abs, root+string(filepath.Separator)) {
		return "", false
	}
	return abs, true
}

func (s *Server) fail(w http.ResponseWriter, what string, err error) {
	s.log.Error("request failed", "what", what, "error", err)
	writeError(w, http.StatusInternalServerError, what+" failed")
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Default().Error("cannot write response", "error", err)
	}
}

func writeError(w http.ResponseWriter, code int, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}
