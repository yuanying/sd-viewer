package metadata

import (
	"reflect"
	"testing"
)

const fullParameters = `masterpiece, best quality, very aesthetic,
1girl, long hair, (blush:1.2), looking at viewer,

<lora:sample_style_v3:0.8>, sample style
Negative prompt: watermark, worst quality, low quality,
extra fingers
Steps: 32, Sampler: ER SDE, Schedule type: Beta, CFG scale: 4, Shift: 3, Seed: 647480969, Size: 896x1152, Model: anima-base-v1.0, Model hash: bd43b7cffe, Module 1: qwen_image_vae, Module 2: qwen_3_06b_base, RNG: CPU, Lora hashes: "sample_style_v3: f3897cb6732f, other_lora: aabbccdd1122", Denoising strength: 0.4, Emphasis: Original, Version: neo-2.27`

const fullPrompt = `masterpiece, best quality, very aesthetic,
1girl, long hair, (blush:1.2), looking at viewer,

<lora:sample_style_v3:0.8>, sample style`

func TestParse_parametersテキストを構造化する(t *testing.T) {
	tests := []struct {
		name string
		text string
		want *Params
	}{
		{
			name: "3 ブロックすべてを含むテキストを分解する",
			text: fullParameters,
			want: &Params{
				Prompt:         fullPrompt,
				NegativePrompt: "watermark, worst quality, low quality,\nextra fingers",
				Steps:          32,
				Sampler:        "ER SDE",
				ScheduleType:   "Beta",
				CFGScale:       4,
				Seed:           647480969,
				Width:          896,
				Height:         1152,
				Model:          "anima-base-v1.0",
				ModelHash:      "bd43b7cffe",
				Denoising:      0.4,
				Version:        "neo-2.27",
				Loras: []Lora{
					{Name: "sample_style_v3", Weight: 0.8, Hash: "f3897cb6732f"},
					{Name: "other_lora", Hash: "aabbccdd1122"},
				},
				PositiveTags: []string{
					"masterpiece", "best quality", "very aesthetic",
					"1girl", "long hair", "blush", "looking at viewer", "sample style",
				},
				NegativeTags: []string{"watermark", "worst quality", "low quality", "extra fingers"},
				Extras: map[string]string{
					"Shift":    "3",
					"Module 1": "qwen_image_vae",
					"Module 2": "qwen_3_06b_base",
					"RNG":      "CPU",
					"Emphasis": "Original",
				},
				Raw: fullParameters,
			},
		},
		{
			name: "ネガティブプロンプトがないテキストを扱える",
			text: "1girl, smile\nSteps: 20, Sampler: Euler a, Seed: 1",
			want: &Params{
				Prompt:       "1girl, smile",
				Steps:        20,
				Sampler:      "Euler a",
				Seed:         1,
				PositiveTags: []string{"1girl", "smile"},
				Extras:       map[string]string{},
				Raw:          "1girl, smile\nSteps: 20, Sampler: Euler a, Seed: 1",
			},
		},
		{
			name: "パラメータ行がなければ全体をプロンプトとして扱う",
			text: "just a prompt, no parameters\nsecond line",
			want: &Params{
				Prompt:       "just a prompt, no parameters\nsecond line",
				PositiveTags: []string{"just a prompt", "no parameters second line"},
				Extras:       map[string]string{},
				Raw:          "just a prompt, no parameters\nsecond line",
			},
		},
		{
			name: "プロンプトの LoRA 記法から名前と重みを取り出す",
			text: "1girl, <lora:foo:0.6>, <lora:bar>, <lyco:baz:0.5:0.2>\nSteps: 20, Sampler: Euler a",
			want: &Params{
				Prompt:  "1girl, <lora:foo:0.6>, <lora:bar>, <lyco:baz:0.5:0.2>",
				Steps:   20,
				Sampler: "Euler a",
				Loras: []Lora{
					{Name: "foo", Weight: 0.6},
					{Name: "bar", Weight: 1},
					{Name: "baz", Weight: 0.5},
				},
				PositiveTags: []string{"1girl"},
				Extras:       map[string]string{},
				Raw:          "1girl, <lora:foo:0.6>, <lora:bar>, <lyco:baz:0.5:0.2>\nSteps: 20, Sampler: Euler a",
			},
		},
		{
			name: "数値として解釈できないパラメータはその他へ回す",
			text: "1girl\nSteps: many, Sampler: Euler a, Seed: unknown",
			want: &Params{
				Prompt:       "1girl",
				Sampler:      "Euler a",
				PositiveTags: []string{"1girl"},
				Extras:       map[string]string{"Steps": "many", "Seed": "unknown"},
				Raw:          "1girl\nSteps: many, Sampler: Euler a, Seed: unknown",
			},
		},
		{
			name: "空文字は空の結果を返す",
			text: "",
			want: &Params{Extras: map[string]string{}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Given: parameters テキスト
			// When: 解析する
			got := Parse(tt.text)

			// Then: 期待どおりに構造化される
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("Parse() mismatch\n got = %#v\nwant = %#v", got, tt.want)
			}
		})
	}
}

func TestParse_タグを正規化して取り出す(t *testing.T) {
	tests := []struct {
		name   string
		prompt string
		want   []string
	}{
		{
			name:   "強調の括弧と重み指定を取り除く",
			prompt: "(blush:1.2), ((very important)), [weak], {curly}",
			want:   []string{"blush", "very important", "weak", "curly"},
		},
		{
			name:   "大文字小文字を揃え前後の空白を落とす",
			prompt: "  Long Hair ,\n1GIRL,",
			want:   []string{"long hair", "1girl"},
		},
		{
			name:   "LoRA 記法はタグに含めない",
			prompt: "1girl, <lora:foo:0.8>, smile",
			want:   []string{"1girl", "smile"},
		},
		{
			name:   "BREAK と空要素は除外する",
			prompt: "1girl, BREAK, , smile,,",
			want:   []string{"1girl", "smile"},
		},
		{
			name:   "エスケープされた括弧は文字として残す",
			prompt: `character \(series\), smile`,
			want:   []string{"character (series)", "smile"},
		},
		{
			name:   "同じタグは重複させない",
			prompt: "1girl, smile, 1girl",
			want:   []string{"1girl", "smile"},
		},
		{
			name:   "括弧を伴わない値はそのまま残す",
			prompt: "ratio 16:9, smile",
			want:   []string{"ratio 16:9", "smile"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Given: 正規化対象のプロンプト
			text := tt.prompt + "\nSteps: 20, Sampler: Euler a, Seed: 1"

			// When: 解析する
			got := Parse(text)

			// Then: タグが正規化される
			if !reflect.DeepEqual(got.PositiveTags, tt.want) {
				t.Errorf("PositiveTags = %#v, want %#v", got.PositiveTags, tt.want)
			}
		})
	}
}

func TestParse_パラメータ行の判定(t *testing.T) {
	tests := []struct {
		name           string
		text           string
		wantParamsLine bool
	}{
		{
			name:           "キーと値がカンマで並ぶ行はパラメータ行とみなす",
			text:           "1girl\nSteps: 20, Sampler: Euler a",
			wantParamsLine: true,
		},
		{
			name:           "キーと値が 1 組だけの行はプロンプトとみなす",
			text:           "1girl\nnote: this is a memo",
			wantParamsLine: false,
		},
		{
			name:           "コロンを含まない行はプロンプトとみなす",
			text:           "1girl\nsmile, happy",
			wantParamsLine: false,
		},
		{
			name:           "コロンを含む語が混ざる行はプロンプトとみなす",
			text:           "1girl\nsmile, ratio: 16:9, happy",
			wantParamsLine: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Given: 最終行の性質が異なるテキスト
			// When: 解析する
			got := Parse(tt.text)

			// Then: パラメータ行と判定されたときだけ最終行がプロンプトから外れる
			lastLineInPrompt := got.Prompt == tt.text
			if lastLineInPrompt == tt.wantParamsLine {
				t.Errorf("Prompt = %q, wantParamsLine = %v", got.Prompt, tt.wantParamsLine)
			}
		})
	}
}
