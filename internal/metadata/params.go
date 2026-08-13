// Package metadata は Stable Diffusion WebUI (AUTOMATIC1111 / Forge) が
// PNG に埋め込む生成パラメータの読み取りと解析を担う。
package metadata

import (
	"regexp"
	"strconv"
	"strings"
)

// Params は parameters テキストを解析した結果を表す。
//
// Width と Height は Size パラメータに書かれた「生成時に指定したサイズ」であり、
// Hires fix や img2img では実際の画像サイズと一致しない。
// 実サイズは Info の値を正とする。
type Params struct {
	Prompt         string            `json:"prompt"`
	NegativePrompt string            `json:"negative_prompt"`
	Steps          int               `json:"steps"`
	Sampler        string            `json:"sampler"`
	ScheduleType   string            `json:"schedule_type"`
	CFGScale       float64           `json:"cfg_scale"`
	Seed           int64             `json:"seed"`
	Width          int               `json:"width"`
	Height         int               `json:"height"`
	Model          string            `json:"model"`
	ModelHash      string            `json:"model_hash"`
	Denoising      float64           `json:"denoising"`
	Version        string            `json:"version"`
	Loras          []Lora            `json:"loras"`
	PositiveTags   []string          `json:"positive_tags"`
	NegativeTags   []string          `json:"negative_tags"`
	Extras         map[string]string `json:"extras"`
	Raw            string            `json:"raw"`
}

// Lora は 1 枚の画像に適用された LoRA を表す。
// Weight はプロンプトの記法から、Hash はパラメータ行の Lora hashes から得られる。
type Lora struct {
	Name   string  `json:"name"`
	Weight float64 `json:"weight,omitempty"`
	Hash   string  `json:"hash,omitempty"`
}

const negativePrefix = "Negative prompt:"

var (
	loraRe   = regexp.MustCompile(`<(?:lora|lyco):([^:>]+)(?::([^>]*))?>`)
	angleRe  = regexp.MustCompile(`<[^>]*>`)
	weightRe = regexp.MustCompile(`:\s*-?\d+(?:\.\d+)?\s*$`)
	keyRe    = regexp.MustCompile(`^\s*[A-Za-z][^:]*:`)
)

// Parse は parameters テキストを解析する。
// 解析できない要素があっても失敗とはせず、取り出せた範囲を返す。
func Parse(text string) *Params {
	p := &Params{Raw: text, Extras: map[string]string{}}
	if strings.TrimSpace(text) == "" {
		return p
	}

	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")

	// 最終行がパラメータ行であればプロンプトブロックから切り離す。
	if last := len(lines) - 1; last >= 0 && isParamLine(lines[last]) {
		p.applyParamLine(lines[last])
		lines = lines[:last]
	}

	// 残りを positive / negative に分ける。
	prompt := lines
	for i, line := range lines {
		if strings.HasPrefix(line, negativePrefix) {
			prompt = lines[:i]
			negative := append([]string{strings.TrimPrefix(line, negativePrefix)}, lines[i+1:]...)
			p.NegativePrompt = strings.TrimSpace(strings.Join(negative, "\n"))
			break
		}
	}
	p.Prompt = strings.TrimSpace(strings.Join(prompt, "\n"))

	p.PositiveTags = extractTags(p.Prompt)
	p.NegativeTags = extractTags(p.NegativePrompt)
	p.Loras = mergeLoras(extractPromptLoras(p.Prompt), p.Loras)

	return p
}

// isParamLine は行が「キー: 値」をカンマ区切りで並べたパラメータ行かどうかを判定する。
func isParamLine(line string) bool {
	if strings.TrimSpace(line) == "" {
		return false
	}
	fields := 0
	for _, part := range splitTopLevel(line) {
		if strings.TrimSpace(part) == "" {
			continue
		}
		if !keyRe.MatchString(part) {
			return false
		}
		fields++
	}
	return fields >= 2
}

// applyParamLine はパラメータ行を解析し、既知キーはフィールドへ、
// それ以外は Extras へ格納する。
func (p *Params) applyParamLine(line string) {
	for _, part := range splitTopLevel(line) {
		key, value, ok := strings.Cut(part, ":")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = unquote(strings.TrimSpace(value))
		if key == "" || value == "" {
			continue
		}
		if !p.applyKnown(key, value) {
			p.Extras[key] = value
		}
	}
}

// applyKnown は既知キーであればフィールドへ格納し、格納できたかを返す。
func (p *Params) applyKnown(key, value string) bool {
	switch key {
	case "Steps":
		n, err := strconv.Atoi(value)
		if err != nil {
			return false
		}
		p.Steps = n
	case "Sampler":
		p.Sampler = value
	case "Schedule type":
		p.ScheduleType = value
	case "CFG scale":
		f, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return false
		}
		p.CFGScale = f
	case "Seed":
		n, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return false
		}
		p.Seed = n
	case "Size":
		w, h, ok := parseSize(value)
		if !ok {
			return false
		}
		p.Width, p.Height = w, h
	case "Model":
		p.Model = value
	case "Model hash":
		p.ModelHash = value
	case "Denoising strength":
		f, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return false
		}
		p.Denoising = f
	case "Version":
		p.Version = value
	case "Lora hashes":
		p.Loras = append(p.Loras, parseLoraHashes(value)...)
	default:
		return false
	}
	return true
}

func parseSize(value string) (int, int, bool) {
	ws, hs, ok := strings.Cut(value, "x")
	if !ok {
		return 0, 0, false
	}
	w, err := strconv.Atoi(strings.TrimSpace(ws))
	if err != nil {
		return 0, 0, false
	}
	h, err := strconv.Atoi(strings.TrimSpace(hs))
	if err != nil {
		return 0, 0, false
	}
	return w, h, true
}

// parseLoraHashes は `名前: ハッシュ, 名前: ハッシュ` 形式の値を解析する。
func parseLoraHashes(value string) []Lora {
	var loras []Lora
	for part := range strings.SplitSeq(value, ",") {
		name, hash, ok := strings.Cut(part, ":")
		if !ok {
			continue
		}
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		loras = append(loras, Lora{Name: name, Hash: strings.TrimSpace(hash)})
	}
	return loras
}

// extractPromptLoras はプロンプト中の <lora:名前:重み> 記法を抽出する。
func extractPromptLoras(prompt string) []Lora {
	var loras []Lora
	for _, m := range loraRe.FindAllStringSubmatch(prompt, -1) {
		name := strings.TrimSpace(m[1])
		if name == "" {
			continue
		}
		weight := 1.0
		if m[2] != "" {
			// <lora:name:1:0.5> のように複数指定される場合は先頭を採用する。
			head, _, _ := strings.Cut(m[2], ":")
			if f, err := strconv.ParseFloat(strings.TrimSpace(head), 64); err == nil {
				weight = f
			}
		}
		loras = append(loras, Lora{Name: name, Weight: weight})
	}
	return loras
}

// mergeLoras はプロンプト由来とハッシュ由来の LoRA を、名前をキーに統合する。
// 並びはプロンプトでの出現順を先とする。
func mergeLoras(fromPrompt, fromHashes []Lora) []Lora {
	var merged []Lora
	index := map[string]int{}
	for _, l := range fromPrompt {
		if i, ok := index[l.Name]; ok {
			if merged[i].Weight == 0 {
				merged[i].Weight = l.Weight
			}
			continue
		}
		index[l.Name] = len(merged)
		merged = append(merged, l)
	}
	for _, l := range fromHashes {
		if i, ok := index[l.Name]; ok {
			if merged[i].Hash == "" {
				merged[i].Hash = l.Hash
			}
			continue
		}
		index[l.Name] = len(merged)
		merged = append(merged, l)
	}
	return merged
}

// extractTags はプロンプトをカンマで分割し、正規化したタグの一覧を返す。
func extractTags(prompt string) []string {
	if prompt == "" {
		return nil
	}
	prompt = angleRe.ReplaceAllString(prompt, "")

	var tags []string
	seen := map[string]bool{}
	for part := range strings.SplitSeq(prompt, ",") {
		tag := normalizeTag(part)
		if tag == "" || seen[tag] {
			continue
		}
		seen[tag] = true
		tags = append(tags, tag)
	}
	return tags
}

// normalizeTag は強調記法や重み指定を取り除き、比較可能な形へ整える。
func normalizeTag(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if s == "BREAK" {
		return ""
	}

	// 対応する強調括弧を剥がす。剥がした場合のみ末尾の重み指定を取り除く。
	emphasized := false
	for len(s) >= 2 && isOpenBracket(s[0]) && matchesClose(s[0], s[len(s)-1]) && !isEscaped(s, len(s)-1) {
		s = strings.TrimSpace(s[1 : len(s)-1])
		emphasized = true
	}
	if emphasized {
		s = strings.TrimSpace(weightRe.ReplaceAllString(s, ""))
	}

	s = unescapeBrackets(s)
	return strings.ToLower(s)
}

func isOpenBracket(c byte) bool {
	return c == '(' || c == '[' || c == '{'
}

func matchesClose(open, close byte) bool {
	switch open {
	case '(':
		return close == ')'
	case '[':
		return close == ']'
	case '{':
		return close == '}'
	}
	return false
}

// isEscaped は s[i] の直前にバックスラッシュが奇数個並ぶかを判定する。
func isEscaped(s string, i int) bool {
	n := 0
	for j := i - 1; j >= 0 && s[j] == '\\'; j-- {
		n++
	}
	return n%2 == 1
}

func unescapeBrackets(s string) string {
	return strings.NewReplacer(
		`\(`, `(`, `\)`, `)`,
		`\[`, `[`, `\]`, `]`,
		`\{`, `{`, `\}`, `}`,
	).Replace(s)
}

// splitTopLevel はダブルクォートの外側にあるカンマだけで分割する。
func splitTopLevel(line string) []string {
	var parts []string
	var cur strings.Builder
	inQuote := false
	for _, r := range line {
		switch {
		case r == '"':
			inQuote = !inQuote
			cur.WriteRune(r)
		case r == ',' && !inQuote:
			parts = append(parts, cur.String())
			cur.Reset()
		default:
			cur.WriteRune(r)
		}
	}
	return append(parts, cur.String())
}

func unquote(s string) string {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1]
	}
	return s
}
