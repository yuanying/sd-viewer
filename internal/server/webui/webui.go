// Package webui はビルド済みの画面をバイナリへ埋め込む。
package webui

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var assets embed.FS

// FS は埋め込んだ画面を返す。まだビルドされていない場合は nil を返し、
// 呼び出し側は API だけを提供する。
func FS() fs.FS {
	sub, err := fs.Sub(assets, "dist")
	if err != nil {
		return nil
	}
	if _, err := fs.Stat(sub, "index.html"); err != nil {
		return nil
	}
	return sub
}
