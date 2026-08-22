// Package config は起動時の設定を、設定ファイルから読む。
//
// 設定ファイルは要るものではない。置かれていなければ既定の値で動く。
// 書かれていない項目も既定のままとし、書かれた項目だけを上書きする。
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/yuanying/sd-viewer/internal/scanner"
)

// appName は設定とキャッシュの置き場所に使う名前。
const appName = "sd-viewer"

// Dir は監視する出力ディレクトリ 1 つ分。
//
// Name は検索条件やファセットに出るルート名で、省略するとディレクトリ名になる。
// 明示しておくと、ディレクトリを引っ越しても検索条件が変わらない。
type Dir struct {
	Name string `toml:"name"`
	Path string `toml:"path"`
}

// Config は起動時の設定。
type Config struct {
	Dirs      []Dir  `toml:"dir"`
	Addr      string `toml:"addr"`
	WebUIURL  string `toml:"webui-url"`
	DataDir   string `toml:"data-dir"`
	ThumbSize int    `toml:"thumb-size"`
	NoWatch   bool   `toml:"no-watch"`
	Verbose   bool   `toml:"verbose"`
}

// Default は設定ファイルも指定もないときの値を返す。
func Default() Config {
	return Config{
		Addr:      ":8080",
		DataDir:   defaultDataDir(),
		ThumbSize: 512,
	}
}

// DefaultPath は設定ファイルの既定の置き場所を返す。
func DefaultPath() string {
	return filepath.Join(configHome(), appName, "config.toml")
}

// Load は設定ファイルを既定の値へ重ねて読む。
// 置かれていない場合は既定の値をそのまま返し、エラーにはしない。
func Load(path string) (Config, error) {
	cfg := Default()

	body, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("設定ファイルを読めません: %w", err)
	}

	meta, err := toml.Decode(string(body), &cfg)
	if err != nil {
		return Config{}, fmt.Errorf("%s の書式が正しくありません: %w", path, err)
	}
	// 綴り違いを黙って既定で動かすと、なぜ効かないのか分からなくなる。
	if undecoded := meta.Undecoded(); len(undecoded) > 0 {
		names := make([]string, 0, len(undecoded))
		for _, key := range undecoded {
			names = append(names, key.String())
		}
		return Config{}, fmt.Errorf("%s に知らない項目があります: %s", path, strings.Join(names, ", "))
	}
	return cfg, nil
}

// Roots は設定のディレクトリを、名前の重複しないルートへ変換する。
// パスは絶対パスへ直し、先頭のチルダはホームディレクトリへ展開する。
func (c Config) Roots() ([]scanner.Root, error) {
	// 明示された名前は譲らない。付け替えると検索条件の意味が変わってしまう。
	named := map[string]string{}
	roots := make([]scanner.Root, 0, len(c.Dirs))

	for _, dir := range c.Dirs {
		abs, err := filepath.Abs(expand(dir.Path))
		if err != nil {
			return nil, fmt.Errorf("%s の位置を決められません: %w", dir.Path, err)
		}
		info, err := os.Stat(abs)
		if err != nil {
			return nil, fmt.Errorf("%s を開けません: %w", dir.Path, err)
		}
		if !info.IsDir() {
			return nil, fmt.Errorf("%s はディレクトリではありません", dir.Path)
		}

		name := dir.Name
		if name == "" {
			name = uniqueName(filepath.Base(abs), named)
		} else if other, taken := named[name]; taken {
			return nil, fmt.Errorf("ルート名 %s が %s と重なっています", name, other)
		}
		named[name] = abs
		roots = append(roots, scanner.Root{Name: name, Path: abs})
	}
	return roots, nil
}

// uniqueName はまだ使われていない名前を、必要なら連番を足して作る。
func uniqueName(base string, taken map[string]string) string {
	name := base
	for i := 2; ; i++ {
		if _, used := taken[name]; !used {
			return name
		}
		name = fmt.Sprintf("%s-%d", base, i)
	}
}

// expand は先頭の ~ をホームディレクトリへ直す。
// 別の利用者を指す ~name は位置を決められないため、そのまま返す。
func expand(path string) string {
	if path != "~" && !strings.HasPrefix(path, "~"+string(filepath.Separator)) {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	if path == "~" {
		return home
	}
	return filepath.Join(home, path[2:])
}

// configHome は設定の置き場所の親を返す。
func configHome() string {
	if dir := os.Getenv("XDG_CONFIG_HOME"); dir != "" {
		return dir
	}
	if dir, err := os.UserConfigDir(); err == nil {
		return dir
	}
	return "."
}

// defaultDataDir はインデックスとサムネイルの既定の置き場所を返す。
func defaultDataDir() string {
	base, err := os.UserCacheDir()
	if err != nil {
		return "." + appName
	}
	return filepath.Join(base, appName)
}
