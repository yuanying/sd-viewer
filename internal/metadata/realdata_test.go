package metadata

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// TestParse_実際の出力ディレクトリを解析できる は、手元の WebUI 出力に対して
// 解析器を通し、想定どおり情報が取れているかを確認するための検証用テスト。
// 環境変数 SD_VIEWER_SAMPLE_DIR が指すディレクトリがある場合だけ実行する。
func TestParse_実際の出力ディレクトリを解析できる(t *testing.T) {
	dir := os.Getenv("SD_VIEWER_SAMPLE_DIR")
	if dir == "" {
		t.Skip("SD_VIEWER_SAMPLE_DIR is not set")
	}

	var (
		total     int
		withParam int
		withModel int
		withLora  int
		withTags  int
		resized   int
		noParams  []string
		unknown   = map[string]int{}
	)

	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.EqualFold(filepath.Ext(path), ".png") {
			return nil
		}
		total++

		info, err := ReadFile(path)
		if err != nil {
			t.Errorf("ReadFile(%s) error = %v", path, err)
			return nil
		}
		if info.Parameters == "" {
			if len(noParams) < 5 {
				noParams = append(noParams, path)
			}
			return nil
		}
		withParam++

		p := Parse(info.Parameters)
		if p.Model != "" {
			withModel++
		}
		if len(p.Loras) > 0 {
			withLora++
		}
		if len(p.PositiveTags) > 0 {
			withTags++
		}
		for k := range p.Extras {
			unknown[k]++
		}

		// Hires fix や img2img では Size パラメータと実サイズが食い違う。
		// 実サイズは IHDR を正とするため、ここでは件数だけ数える。
		if p.Width != 0 && info.Width != 0 && p.Width != info.Width {
			resized++
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WalkDir() error = %v", err)
	}

	keys := make([]string, 0, len(unknown))
	for k := range unknown {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return unknown[keys[i]] > unknown[keys[j]] })

	t.Logf("総数=%d parameters有=%d model有=%d lora有=%d tag有=%d 指定サイズと実サイズが異なる=%d",
		total, withParam, withModel, withLora, withTags, resized)
	t.Logf("parameters なし例: %v", noParams)
	for _, k := range keys {
		t.Logf("未知キー %-28s %d", k, unknown[k])
	}
}
