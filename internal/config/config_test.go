package config

import (
	"errors"
	"flag"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/yuanying/sd-viewer/internal/scanner"
)

// write は設定ファイルを一時ディレクトリへ書き出し、その位置を返す。
func write(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoad_設定ファイルの値を読む(t *testing.T) {
	// Given
	path := write(t, `
addr = ":8189"
webui-url = "http://localhost:7860"
data-dir = "/var/tmp/sd-viewer"
thumb-size = 384
no-watch = true
verbose = true

[[dir]]
name = "forge"
path = "/srv/forge/output"

[[dir]]
path = "/srv/archive"
`)

	// When
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	// Then
	if cfg.Addr != ":8189" {
		t.Errorf("Addr = %q, want :8189", cfg.Addr)
	}
	if cfg.WebUIURL != "http://localhost:7860" {
		t.Errorf("WebUIURL = %q", cfg.WebUIURL)
	}
	if cfg.DataDir != "/var/tmp/sd-viewer" {
		t.Errorf("DataDir = %q", cfg.DataDir)
	}
	if cfg.ThumbSize != 384 {
		t.Errorf("ThumbSize = %d, want 384", cfg.ThumbSize)
	}
	if !cfg.NoWatch || !cfg.Verbose {
		t.Errorf("NoWatch = %v, Verbose = %v, want どちらも true", cfg.NoWatch, cfg.Verbose)
	}
	want := []Dir{{Name: "forge", Path: "/srv/forge/output"}, {Path: "/srv/archive"}}
	if !slices.Equal(cfg.Dirs, want) {
		t.Errorf("Dirs = %v, want %v", cfg.Dirs, want)
	}
}

func TestLoad_書かれていない項目は既定のままにする(t *testing.T) {
	// Given: ディレクトリだけを書いた設定
	path := write(t, `
[[dir]]
path = "/srv/forge/output"
`)

	// When
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	// Then
	base := Default()
	if cfg.Addr != base.Addr {
		t.Errorf("Addr = %q, want %q", cfg.Addr, base.Addr)
	}
	if cfg.ThumbSize != base.ThumbSize {
		t.Errorf("ThumbSize = %d, want %d", cfg.ThumbSize, base.ThumbSize)
	}
	if cfg.DataDir != base.DataDir {
		t.Errorf("DataDir = %q, want %q", cfg.DataDir, base.DataDir)
	}
}

func TestLoad_設定ファイルがなければ既定を返す(t *testing.T) {
	// Given: 置かれていない位置
	path := filepath.Join(t.TempDir(), "missing.toml")

	// When
	cfg, err := Load(path)

	// Then: 設定ファイルは要るものではないため、これは失敗ではない
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Addr != Default().Addr {
		t.Errorf("Addr = %q, want %q", cfg.Addr, Default().Addr)
	}
	if len(cfg.Dirs) != 0 {
		t.Errorf("Dirs = %v, want 空", cfg.Dirs)
	}
}

func TestLoad_壊れた設定ファイルは理由をつけて断る(t *testing.T) {
	// Given
	path := write(t, "addr = ")

	// When
	_, err := Load(path)

	// Then
	if err == nil {
		t.Fatal("Load() error = nil, want エラー")
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("エラーに位置が入っていない: %v", err)
	}
}

func TestLoad_知らない項目は綴り違いとして断る(t *testing.T) {
	// Given: addr の綴り違い
	path := write(t, `adr = ":8189"`)

	// When
	_, err := Load(path)

	// Then: 黙って既定で動くと気づけないため、はっきり断る
	if err == nil {
		t.Fatal("Load() error = nil, want エラー")
	}
	if !strings.Contains(err.Error(), "adr") {
		t.Errorf("エラーに項目名が入っていない: %v", err)
	}
}

func TestRoots_ディレクトリをルートへ変換する(t *testing.T) {
	// Given: 実在するディレクトリ 2 つ
	base := t.TempDir()
	forge := filepath.Join(base, "output")
	archive := filepath.Join(base, "archive")
	for _, dir := range []string{forge, archive} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	cfg := Config{Dirs: []Dir{{Name: "forge", Path: forge}, {Path: archive}}}

	// When
	roots, err := cfg.Roots()
	if err != nil {
		t.Fatalf("Roots() error = %v", err)
	}

	// Then: 名前の指定があればそれを、なければディレクトリ名を使う
	want := []scanner.Root{{Name: "forge", Path: forge}, {Name: "archive", Path: archive}}
	if !slices.Equal(roots, want) {
		t.Errorf("Roots() = %v, want %v", roots, want)
	}
}

func TestRoots_同じ名前になるときは連番で分ける(t *testing.T) {
	// Given: 末尾が同じ名前のディレクトリ 2 つ
	base := t.TempDir()
	first := filepath.Join(base, "a", "images")
	second := filepath.Join(base, "b", "images")
	for _, dir := range []string{first, second} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	cfg := Config{Dirs: []Dir{{Path: first}, {Path: second}}}

	// When
	roots, err := cfg.Roots()
	if err != nil {
		t.Fatalf("Roots() error = %v", err)
	}

	// Then
	if roots[0].Name != "images" || roots[1].Name != "images-2" {
		t.Errorf("名前 = %q, %q, want images, images-2", roots[0].Name, roots[1].Name)
	}
}

func TestRoots_名前がぶつかったら断る(t *testing.T) {
	// Given: 同じ名前を明示した 2 つ
	base := t.TempDir()
	first := filepath.Join(base, "a")
	second := filepath.Join(base, "b")
	for _, dir := range []string{first, second} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	cfg := Config{Dirs: []Dir{{Name: "same", Path: first}, {Name: "same", Path: second}}}

	// When
	_, err := cfg.Roots()

	// Then: 黙って片方を付け替えると検索条件の意味が変わるため断る
	if err == nil {
		t.Fatal("Roots() error = nil, want エラー")
	}
	if !strings.Contains(err.Error(), "same") {
		t.Errorf("エラーに名前が入っていない: %v", err)
	}
}

func TestRoots_ディレクトリがなければ理由をつけて断る(t *testing.T) {
	tests := []struct {
		name string
		dir  func(t *testing.T) string
	}{
		{
			name: "存在しない",
			dir:  func(t *testing.T) string { return filepath.Join(t.TempDir(), "missing") },
		},
		{
			name: "ディレクトリではない",
			dir: func(t *testing.T) string {
				path := filepath.Join(t.TempDir(), "file.txt")
				if err := os.WriteFile(path, nil, 0o644); err != nil {
					t.Fatal(err)
				}
				return path
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Given
			cfg := Config{Dirs: []Dir{{Path: tt.dir(t)}}}

			// When
			_, err := cfg.Roots()

			// Then
			if err == nil {
				t.Fatal("Roots() error = nil, want エラー")
			}
		})
	}
}

func TestExpand_先頭のチルダをホームディレクトリへ直す(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	tests := []struct {
		in   string
		want string
	}{
		{"~/sd/output", filepath.Join(home, "sd/output")},
		{"~", home},
		{"/srv/output", "/srv/output"},
		{"", ""},
		// 別の利用者を指す ~name はホームの位置を決められないため、そのまま渡す。
		{"~other/output", "~other/output"},
	}
	for _, tt := range tests {
		if got := expand(tt.in); got != tt.want {
			t.Errorf("expand(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestRoots_チルダを含むパスを開ける(t *testing.T) {
	// Given: ホームディレクトリの下の出力ディレクトリ
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, "sd", "output"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := Config{Dirs: []Dir{{Path: "~/sd/output"}}}

	// When
	roots, err := cfg.Roots()
	if err != nil {
		t.Fatalf("Roots() error = %v", err)
	}

	// Then
	if roots[0].Path != filepath.Join(home, "sd", "output") {
		t.Errorf("Path = %q, want %q", roots[0].Path, filepath.Join(home, "sd", "output"))
	}
}

func TestDefaultPath_XDGの置き場所を返す(t *testing.T) {
	// Given
	t.Setenv("XDG_CONFIG_HOME", "/xdg")

	// When / Then
	if got := DefaultPath(); got != filepath.Join("/xdg", "sd-viewer", "config.toml") {
		t.Errorf("DefaultPath() = %q", got)
	}

	// Given: XDG_CONFIG_HOME がなければホームの下を使う
	t.Setenv("XDG_CONFIG_HOME", "")
	home := t.TempDir()
	t.Setenv("HOME", home)

	// When / Then
	want := filepath.Join(home, ".config", "sd-viewer", "config.toml")
	if got := DefaultPath(); got != want {
		t.Errorf("DefaultPath() = %q, want %q", got, want)
	}
}

// withConfig は既定の位置へ設定ファイルを置き、その中身を使えるようにする。
func withConfig(t *testing.T, body string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", home)
	dir := filepath.Join(home, "sd-viewer")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// tempDirs は実在するディレクトリを人数分作って返す。
func tempDirs(t *testing.T, names ...string) []string {
	t.Helper()
	base := t.TempDir()
	paths := make([]string, 0, len(names))
	for _, name := range names {
		path := filepath.Join(base, name)
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, path)
	}
	return paths
}

func TestParse_設定ファイルの値で起動できる(t *testing.T) {
	// Given
	dirs := tempDirs(t, "output")
	withConfig(t, `
addr = ":8189"
webui-url = "http://localhost:7860"

[[dir]]
name = "forge"
path = "`+dirs[0]+`"
`)

	// When: 引数なしで起動する
	cfg, err := Parse(nil)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	// Then
	if cfg.Addr != ":8189" {
		t.Errorf("Addr = %q, want :8189", cfg.Addr)
	}
	if cfg.WebUIURL != "http://localhost:7860" {
		t.Errorf("WebUIURL = %q", cfg.WebUIURL)
	}
	if len(cfg.Dirs) != 1 || cfg.Dirs[0].Name != "forge" {
		t.Errorf("Dirs = %v", cfg.Dirs)
	}
}

func TestParse_指定したフラグだけが設定ファイルを上書きする(t *testing.T) {
	// Given
	dirs := tempDirs(t, "output")
	withConfig(t, `
addr = ":8189"
webui-url = "http://localhost:7860"
thumb-size = 384

[[dir]]
path = "`+dirs[0]+`"
`)

	// When: addr だけを渡す
	cfg, err := Parse([]string{"--addr", ":9999"})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	// Then: 渡した項目だけが変わり、残りは設定ファイルのまま
	if cfg.Addr != ":9999" {
		t.Errorf("Addr = %q, want :9999", cfg.Addr)
	}
	if cfg.WebUIURL != "http://localhost:7860" {
		t.Errorf("WebUIURL = %q, 上書きされている", cfg.WebUIURL)
	}
	if cfg.ThumbSize != 384 {
		t.Errorf("ThumbSize = %d, 上書きされている", cfg.ThumbSize)
	}
}

func TestParse_dirを渡すと設定ファイルのディレクトリを置き換える(t *testing.T) {
	// Given
	dirs := tempDirs(t, "output", "elsewhere")
	withConfig(t, `
[[dir]]
name = "forge"
path = "`+dirs[0]+`"
`)

	// When
	cfg, err := Parse([]string{"--dir", dirs[1]})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	// Then: 一時的に別の場所だけを見られる
	want := []Dir{{Path: dirs[1]}}
	if !slices.Equal(cfg.Dirs, want) {
		t.Errorf("Dirs = %v, want %v", cfg.Dirs, want)
	}
}

func TestParse_設定ファイルがなくてもフラグだけで起動できる(t *testing.T) {
	// Given: 設定ファイルを置かない
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dirs := tempDirs(t, "output")

	// When
	cfg, err := Parse([]string{"--dir", dirs[0], "--addr", ":8189"})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	// Then
	if cfg.Addr != ":8189" || len(cfg.Dirs) != 1 {
		t.Errorf("Parse() = %+v", cfg)
	}
}

func TestParse_configで設定ファイルの位置を変えられる(t *testing.T) {
	// Given: 既定の位置とは別に置いた設定ファイル
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dirs := tempDirs(t, "output")
	path := write(t, `
addr = ":7777"

[[dir]]
path = "`+dirs[0]+`"
`)

	// When
	cfg, err := Parse([]string{"--config", path})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	// Then
	if cfg.Addr != ":7777" {
		t.Errorf("Addr = %q, want :7777", cfg.Addr)
	}
}

func TestParse_足りない指定は理由をつけて断る(t *testing.T) {
	dirs := tempDirs(t, "output")

	tests := []struct {
		name string
		args []string
		want string
	}{
		{
			name: "監視するディレクトリがない",
			args: nil,
			want: "dir",
		},
		{
			name: "サムネイルの大きさが 0 以下",
			args: []string{"--dir", dirs[0], "--thumb-size", "0"},
			want: "thumb-size",
		},
		{
			name: "待ち受けアドレスが空",
			args: []string{"--dir", dirs[0], "--addr", ""},
			want: "addr",
		},
		{
			name: "指定した設定ファイルがない",
			args: []string{"--config", filepath.Join(t.TempDir(), "missing.toml")},
			want: "missing.toml",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Given: 設定ファイルを置かない
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())

			// When
			_, err := Parse(tt.args)

			// Then
			if err == nil {
				t.Fatal("Parse() error = nil, want エラー")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Errorf("Parse() error = %v, want %q を含む", err, tt.want)
			}
		})
	}
}

func TestParse_使い方の表示は失敗として扱わない(t *testing.T) {
	// Given
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	// When
	_, err := Parse([]string{"-h"})

	// Then: 呼び出し側が黙って終われるように見分けられる
	if !errors.Is(err, flag.ErrHelp) {
		t.Errorf("Parse() error = %v, want flag.ErrHelp", err)
	}
}

func TestParse_データの置き場所のチルダも展開する(t *testing.T) {
	// Given
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dirs := tempDirs(t, "output")

	// When
	cfg, err := Parse([]string{"--dir", dirs[0], "--data-dir", "~/.cache/sd-viewer"})
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	// Then
	want := filepath.Join(home, ".cache", "sd-viewer")
	if cfg.DataDir != want {
		t.Errorf("DataDir = %q, want %q", cfg.DataDir, want)
	}
}
