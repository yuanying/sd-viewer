// Command sd-viewer は Stable Diffusion WebUI の出力ディレクトリを
// ブラウザから検索・閲覧できるようにする。
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/yuanying/sd-viewer/internal/bridge"
	"github.com/yuanying/sd-viewer/internal/index"
	"github.com/yuanying/sd-viewer/internal/scanner"
	"github.com/yuanying/sd-viewer/internal/server"
	"github.com/yuanying/sd-viewer/internal/server/webui"
	"github.com/yuanying/sd-viewer/internal/thumb"
	"github.com/yuanying/sd-viewer/internal/trash"
)

// config は起動時の設定。
type config struct {
	dirs      dirList
	addr      string
	webUIURL  string
	dataDir   string
	thumbSize int
	noWatch   bool
	verbose   bool
}

// dirList は繰り返し指定できるディレクトリの一覧。
type dirList []string

func (d *dirList) String() string { return strings.Join(*d, ", ") }

func (d *dirList) Set(v string) error {
	if v == "" {
		return errors.New("empty directory")
	}
	*d = append(*d, v)
	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "sd-viewer:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := parseFlags()
	if err != nil {
		return err
	}

	level := slog.LevelInfo
	if cfg.verbose {
		level = slog.LevelDebug
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
	slog.SetDefault(log)

	if err := os.MkdirAll(cfg.dataDir, 0o755); err != nil {
		return fmt.Errorf("cannot create data directory: %w", err)
	}

	db, err := index.Open(filepath.Join(cfg.dataDir, "index.db"))
	if err != nil {
		return err
	}
	defer db.Close()

	thumbs, err := thumb.New(filepath.Join(cfg.dataDir, "thumbs"), cfg.thumbSize)
	if err != nil {
		return err
	}

	roots, err := resolveRoots(cfg.dirs)
	if err != nil {
		return err
	}

	sc, err := scanner.New(db, scanner.Options{
		Roots:  roots,
		Logger: log,
		OnIndexed: func(_ context.Context, img *index.Image, absPath string) {
			if _, err := thumbs.Ensure(img.ID, absPath); err != nil {
				log.Debug("cannot build thumbnail", "path", absPath, "error", err)
			}
		},
		OnRemoved: func(_ context.Context, id int64) {
			if err := thumbs.Remove(id); err != nil {
				log.Debug("cannot remove thumbnail", "id", id, "error", err)
			}
		},
	})
	if err != nil {
		return err
	}
	defer sc.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		if cfg.noWatch {
			if err := sc.Scan(ctx); err != nil && ctx.Err() == nil {
				log.Error("scan failed", "error", err)
			}
			return
		}
		if err := sc.Run(ctx); err != nil && ctx.Err() == nil {
			log.Error("watch failed", "error", err)
		}
	}()

	// 送り先が設定されているときだけ送信機能を有効にする。
	var sender server.Sender
	if cfg.webUIURL != "" {
		client, err := bridge.New(cfg.webUIURL)
		if err != nil {
			return err
		}
		sender = client
		log.Info("WebUI へ送信できます", "url", client.URL())
	}

	// ゴミ箱はルート名から実ディレクトリを引く。
	rootDirs := make(map[string]string, len(roots))
	for _, r := range roots {
		rootDirs[r.Name] = r.Path
	}
	bin := trash.New(trash.Options{
		DB:    db,
		Roots: rootDirs,
		OnPurged: func(id int64) {
			if err := thumbs.Remove(id); err != nil {
				log.Debug("cannot remove thumbnail", "id", id, "error", err)
			}
		},
		Logger: log,
	})

	static := webui.FS()
	if static == nil {
		log.Warn("web assets are not built; serving API only (run: make build)")
	}
	handler := server.New(server.Options{
		DB:      db,
		Thumbs:  thumbs,
		Roots:   roots,
		Scanner: sc,
		Static:  static,
		WebUI:   sender,
		Trash:   bin,
		Logger:  log,
	})

	httpServer := &http.Server{
		Addr:              cfg.addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", cfg.addr, "roots", len(roots))
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		log.Info("shutting down")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return httpServer.Shutdown(shutdownCtx)
}

func parseFlags() (*config, error) {
	cfg := &config{}
	flag.Var(&cfg.dirs, "dir", "監視する出力ディレクトリ（複数指定可）")
	flag.StringVar(&cfg.addr, "addr", ":8080", "待ち受けアドレス")
	flag.StringVar(&cfg.webUIURL, "webui-url", "", "生成情報の送り先となる Stable Diffusion WebUI の URL（例: http://127.0.0.1:7860）")
	flag.StringVar(&cfg.dataDir, "data-dir", defaultDataDir(), "インデックスとサムネイルの保存先")
	flag.IntVar(&cfg.thumbSize, "thumb-size", 512, "サムネイルの長辺ピクセル数")
	flag.BoolVar(&cfg.noWatch, "no-watch", false, "ファイル監視を行わず、起動時のスキャンだけ行う")
	flag.BoolVar(&cfg.verbose, "v", false, "詳細なログを出力する")
	flag.Parse()

	if len(cfg.dirs) == 0 {
		flag.Usage()
		return nil, errors.New("--dir を 1 つ以上指定してください")
	}
	if cfg.thumbSize <= 0 {
		return nil, errors.New("--thumb-size は 1 以上を指定してください")
	}
	return cfg, nil
}

// resolveRoots は指定されたディレクトリを、名前の重複しないルートへ変換する。
func resolveRoots(dirs []string) ([]scanner.Root, error) {
	used := map[string]bool{}
	roots := make([]scanner.Root, 0, len(dirs))

	for _, dir := range dirs {
		abs, err := filepath.Abs(dir)
		if err != nil {
			return nil, fmt.Errorf("cannot resolve %s: %w", dir, err)
		}
		info, err := os.Stat(abs)
		if err != nil {
			return nil, fmt.Errorf("cannot open %s: %w", dir, err)
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("%s is not a directory", dir)
		}

		name := filepath.Base(abs)
		for i := 2; used[name]; i++ {
			name = fmt.Sprintf("%s-%d", filepath.Base(abs), i)
		}
		used[name] = true
		roots = append(roots, scanner.Root{Name: name, Path: abs})
	}
	return roots, nil
}

// defaultDataDir はインデックスとサムネイルの既定の置き場所を返す。
func defaultDataDir() string {
	base, err := os.UserCacheDir()
	if err != nil {
		return ".sd-viewer"
	}
	return filepath.Join(base, "sd-viewer")
}
