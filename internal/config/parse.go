package config

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
)

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

// Parse はコマンドライン引数を読み、設定ファイルへ重ねた設定を返す。
//
// 優先順位は「コマンドライン > 設定ファイル > 既定」とする。
// 実際に渡された項目だけを上書きするため、`--addr` だけを渡しても
// 設定ファイルのほかの項目はそのまま残る。
//
// 使い方の表示を求められた場合は flag.ErrHelp を返す。
func Parse(args []string) (Config, error) {
	var (
		flags    Config
		dirs     dirList
		confPath string
	)

	// フラグの既定値は空にしておく。実際に渡されたものだけを設定へ重ねるため、
	// 既定値は設定ファイルを読んだあとで決まる。案内にだけ書き添える。
	base := Default()

	fs := flag.NewFlagSet("sd-viewer", flag.ContinueOnError)
	fs.StringVar(&confPath, "config", "", "設定ファイルの位置（既定: "+DefaultPath()+"）")
	fs.Var(&dirs, "dir", "監視する出力ディレクトリ（複数指定可。設定ファイルの dir を置き換える）")
	fs.StringVar(&flags.Addr, "addr", "", "待ち受けアドレス（既定: "+base.Addr+"）")
	fs.StringVar(&flags.WebUIURL, "webui-url", "", "生成情報の送り先となる Stable Diffusion WebUI の URL（例: http://localhost:7860）")
	fs.StringVar(&flags.DataDir, "data-dir", "", "インデックスとサムネイルの保存先（既定: "+base.DataDir+"）")
	fs.IntVar(&flags.ThumbSize, "thumb-size", 0, fmt.Sprintf("サムネイルの長辺ピクセル数（既定: %d）", base.ThumbSize))
	fs.BoolVar(&flags.NoWatch, "no-watch", false, "ファイル監視を行わず、起動時のスキャンだけ行う")
	fs.BoolVar(&flags.Verbose, "v", false, "詳細なログを出力する")

	if err := fs.Parse(args); err != nil {
		return Config{}, err
	}

	// 位置を明示されたのに置かれていない場合は、黙って既定で動かさない。
	path := confPath
	if path == "" {
		path = DefaultPath()
	} else if _, err := os.Stat(path); err != nil {
		return Config{}, fmt.Errorf("設定ファイル %s を開けません: %w", path, err)
	}

	cfg, err := Load(path)
	if err != nil {
		return Config{}, err
	}

	// 実際に渡された項目だけを重ねる。
	fs.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "dir":
			cfg.Dirs = toDirs(dirs)
		case "addr":
			cfg.Addr = flags.Addr
		case "webui-url":
			cfg.WebUIURL = flags.WebUIURL
		case "data-dir":
			cfg.DataDir = flags.DataDir
		case "thumb-size":
			cfg.ThumbSize = flags.ThumbSize
		case "no-watch":
			cfg.NoWatch = flags.NoWatch
		case "v":
			cfg.Verbose = flags.Verbose
		}
	})

	// 設定ファイルには ~ を書けるため、使う前に実際の位置へ直しておく。
	cfg.DataDir = expand(cfg.DataDir)

	if err := cfg.validate(path); err != nil {
		fs.Usage()
		return Config{}, err
	}
	return cfg, nil
}

// toDirs はコマンドラインで並べたパスを、名前のないディレクトリへ直す。
func toDirs(paths []string) []Dir {
	dirs := make([]Dir, 0, len(paths))
	for _, p := range paths {
		dirs = append(dirs, Dir{Path: p})
	}
	return dirs
}

// validate は起動できる設定になっているかを確かめる。
// path は不足を直す先として案内するための設定ファイルの位置。
func (c Config) validate(path string) error {
	if len(c.Dirs) == 0 {
		return fmt.Errorf("監視するディレクトリがありません。--dir を指定するか、%s に dir を書いてください", path)
	}
	if c.Addr == "" {
		return errors.New("addr が空です。待ち受けアドレスを指定してください")
	}
	if c.ThumbSize <= 0 {
		return errors.New("thumb-size は 1 以上を指定してください")
	}
	return nil
}
