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
	"syscall"
	"time"

	"github.com/yuanying/sd-viewer/internal/bridge"
	"github.com/yuanying/sd-viewer/internal/config"
	"github.com/yuanying/sd-viewer/internal/index"
	"github.com/yuanying/sd-viewer/internal/scanner"
	"github.com/yuanying/sd-viewer/internal/server"
	"github.com/yuanying/sd-viewer/internal/server/webui"
	"github.com/yuanying/sd-viewer/internal/thumb"
	"github.com/yuanying/sd-viewer/internal/trash"
)

func main() {
	if err := run(); err != nil {
		// 使い方の表示はすでに済んでいるため、重ねて何も言わない。
		if errors.Is(err, flag.ErrHelp) {
			return
		}
		fmt.Fprintln(os.Stderr, "sd-viewer:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Parse(os.Args[1:])
	if err != nil {
		return err
	}

	level := slog.LevelInfo
	if cfg.Verbose {
		level = slog.LevelDebug
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
	slog.SetDefault(log)

	dataDir := cfg.DataDir
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return fmt.Errorf("cannot create data directory: %w", err)
	}

	db, err := index.Open(filepath.Join(dataDir, "index.db"))
	if err != nil {
		return err
	}
	defer db.Close()

	thumbs, err := thumb.New(filepath.Join(dataDir, "thumbs"), cfg.ThumbSize)
	if err != nil {
		return err
	}

	roots, err := cfg.Roots()
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
		if cfg.NoWatch {
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
	if cfg.WebUIURL != "" {
		client, err := bridge.New(cfg.WebUIURL)
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
		Addr:              cfg.Addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Info("listening", "addr", cfg.Addr, "roots", len(roots))
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
