package index

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/yuanying/sd-viewer/internal/metadata"
)

// 検索の検証で使い回す画像。生成日時は新しい順に girl, angry, boy, manual となる。
const (
	pathGirl   = "txt2img/2026-08-13/00001.png"
	pathAngry  = "txt2img/2026-08-12/00002.png"
	pathBoy    = "img2img/2026-08-13/00003.png"
	pathManual = "misc/manual.png"
)

var fixtureBase = time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)

func fixtures() []*Image {
	return []*Image{
		{
			Root: "out", Path: pathGirl,
			Size: 1024, ModTime: fixtureBase, CreatedAt: fixtureBase,
			Width: 896, Height: 1152, HasParams: true,
			Prompt: "1girl, smile, long hair", Negative: "watermark",
			Model: "modelA", ModelHash: "aaaa", Sampler: "Euler a", Steps: 28, CFGScale: 5, Seed: 1,
			Loras:        []metadata.Lora{{Name: "style_v3", Weight: 0.8, Hash: "h1"}, {Name: "char_a", Weight: 1}},
			PositiveTags: []string{"1girl", "smile", "long hair"},
			NegativeTags: []string{"watermark"},
			Extras:       map[string]string{"RNG": "CPU"},
			Raw:          "1girl, smile, long hair\nSteps: 28",
		},
		{
			Root: "out", Path: pathAngry,
			Size: 2048, ModTime: fixtureBase.AddDate(0, 0, -1), CreatedAt: fixtureBase.AddDate(0, 0, -1),
			Width: 1024, Height: 1024, HasParams: true,
			Prompt: "1girl, angry", Negative: "watermark",
			Model: "modelA", ModelHash: "aaaa", Sampler: "DPM++ 2M", Steps: 20, CFGScale: 7, Seed: 2,
			Loras:        []metadata.Lora{{Name: "char_a", Weight: 1}},
			PositiveTags: []string{"1girl", "angry"},
			NegativeTags: []string{"watermark"},
		},
		{
			Root: "out", Path: pathBoy,
			Size: 4096, ModTime: fixtureBase.AddDate(0, 0, -2), CreatedAt: fixtureBase.AddDate(0, 0, -2),
			Width: 896, Height: 1152, HasParams: true,
			Prompt: "1boy, short hair", Negative: "",
			Model: "modelB", ModelHash: "bbbb", Sampler: "Euler a", Steps: 30, CFGScale: 4, Seed: 3,
			Loras:        []metadata.Lora{{Name: "style_v3", Weight: 0.5, Hash: "h1"}},
			PositiveTags: []string{"1boy", "short hair", "hair ornament"},
		},
		{
			Root: "out", Path: pathManual,
			Size: 512, ModTime: fixtureBase.AddDate(0, 0, -30), CreatedAt: fixtureBase.AddDate(0, 0, -30),
			Width: 512, Height: 512, HasParams: false,
		},
	}
}

func newTestDB(t *testing.T) *DB {
	t.Helper()
	db, err := Open(filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// newFixtureDB は fixtures を登録済みのインデックスを返す。
func newFixtureDB(t *testing.T) *DB {
	t.Helper()
	db := newTestDB(t)
	put(t, db, fixtures()...)
	return db
}

func put(t *testing.T, db *DB, imgs ...*Image) {
	t.Helper()
	for _, img := range imgs {
		if err := db.Put(context.Background(), img); err != nil {
			t.Fatalf("Put(%s) error = %v", img.Path, err)
		}
	}
}

func searchPaths(t *testing.T, db *DB, q Query) []string {
	t.Helper()
	res, err := db.Search(context.Background(), q)
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	paths := make([]string, 0, len(res.Images))
	for _, img := range res.Images {
		paths = append(paths, img.Path)
	}
	return paths
}

func TestSearch_条件に一致する画像を返す(t *testing.T) {
	tests := []struct {
		name  string
		query Query
		want  []string
	}{
		{
			name:  "条件を指定しなければ生成日時の新しい順に全件返す",
			query: Query{},
			want:  []string{pathGirl, pathAngry, pathBoy, pathManual},
		},
		{
			name:  "全文検索でプロンプトの語を含む画像を返す",
			query: Query{Text: "smile"},
			want:  []string{pathGirl},
		},
		{
			name:  "複数の語はすべて含む画像を返す",
			query: Query{Text: "1girl smile"},
			want:  []string{pathGirl},
		},
		{
			name:  "マイナス接頭辞の語を含む画像を除外する",
			query: Query{Text: "1girl -angry"},
			want:  []string{pathGirl},
		},
		{
			name:  "引用符で囲んだ語句はひとまとまりとして扱う",
			query: Query{Text: `"long hair"`},
			want:  []string{pathGirl},
		},
		{
			name:  "ネガティブプロンプトの語も全文検索の対象になる",
			query: Query{Text: "watermark"},
			want:  []string{pathGirl, pathAngry},
		},
		{
			name:  "一致しない語では何も返さない",
			query: Query{Text: "unicorn"},
			want:  nil,
		},
		{
			name:  "モデル名で絞り込む",
			query: Query{Models: []string{"modelA"}},
			want:  []string{pathGirl, pathAngry},
		},
		{
			name:  "同じ項目に複数の値を指定すると OR になる",
			query: Query{Models: []string{"modelA", "modelB"}},
			want:  []string{pathGirl, pathAngry, pathBoy},
		},
		{
			name:  "サンプラーで絞り込む",
			query: Query{Samplers: []string{"Euler a"}},
			want:  []string{pathGirl, pathBoy},
		},
		{
			name:  "LoRA で絞り込む",
			query: Query{Loras: []string{"style_v3"}},
			want:  []string{pathGirl, pathBoy},
		},
		{
			name:  "異なる項目どうしは AND になる",
			query: Query{Models: []string{"modelA"}, Loras: []string{"style_v3"}},
			want:  []string{pathGirl},
		},
		{
			name:  "タグは指定したすべてを含む画像を返す",
			query: Query{Tags: []string{"1girl", "smile"}},
			want:  []string{pathGirl},
		},
		{
			name:  "除外タグを含む画像を返さない",
			query: Query{Tags: []string{"1girl"}, ExcludeTags: []string{"angry"}},
			want:  []string{pathGirl},
		},
		{
			name:  "解像度で絞り込む",
			query: Query{Sizes: []string{"896x1152"}},
			want:  []string{pathGirl, pathBoy},
		},
		{
			name:  "ディレクトリを指定すると配下も含めて返す",
			query: Query{Dirs: []string{"txt2img"}},
			want:  []string{pathGirl, pathAngry},
		},
		{
			name:  "末端のディレクトリを指定するとその中だけ返す",
			query: Query{Dirs: []string{"txt2img/2026-08-13"}},
			want:  []string{pathGirl},
		},
		{
			name:  "生成日時の範囲で絞り込む",
			query: Query{From: fixtureBase.AddDate(0, 0, -1), To: fixtureBase.AddDate(0, 0, 1)},
			want:  []string{pathGirl, pathAngry},
		},
		{
			name:  "古い順に並べ替える",
			query: Query{Sort: SortOldest},
			want:  []string{pathManual, pathBoy, pathAngry, pathGirl},
		},
		{
			name:  "パス順に並べ替える",
			query: Query{Sort: SortName},
			want:  []string{pathBoy, pathManual, pathAngry, pathGirl},
		},
		{
			name:  "生成情報を持たない画像も一覧に含まれる",
			query: Query{Dirs: []string{"misc"}},
			want:  []string{pathManual},
		},
		{
			name:  "ルートで絞り込む",
			query: Query{Roots: []string{"other"}},
			want:  nil,
		},
	}

	db := newFixtureDB(t)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Given: 登録済みの画像群
			// When: 条件を指定して検索する
			got := searchPaths(t, db, tt.query)

			// Then: 期待した画像が期待した順で返る
			if len(got) == 0 && len(tt.want) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("paths = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSearch_全文検索の記号は構文エラーにならない(t *testing.T) {
	tests := []struct {
		name string
		text string
	}{
		{name: "閉じていない引用符", text: `"unclosed`},
		{name: "演算子だけが残る入力", text: "a AND"},
		{name: "開き括弧", text: "("},
		{name: "ワイルドカード", text: "*"},
		{name: "近傍検索の記号", text: "^ NEAR/"},
		{name: "記号だけの入力", text: "!!! ***"},
		{name: "空白だけの入力", text: "   "},
	}

	db := newFixtureDB(t)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Given: FTS の構文記号を含む検索語
			// When: 検索する
			_, err := db.Search(context.Background(), Query{Text: tt.text})

			// Then: エラーにならない
			if err != nil {
				t.Errorf("Search(%q) error = %v", tt.text, err)
			}
		})
	}
}

func TestSearch_ページングしても総件数は全体を示す(t *testing.T) {
	tests := []struct {
		name      string
		limit     int
		offset    int
		wantPaths []string
	}{
		{name: "1 ページ目", limit: 2, offset: 0, wantPaths: []string{pathGirl, pathAngry}},
		{name: "2 ページ目", limit: 2, offset: 2, wantPaths: []string{pathBoy, pathManual}},
		{name: "範囲外のページ", limit: 2, offset: 10, wantPaths: []string{}},
		{name: "上限を指定しなければ全件", limit: 0, offset: 0, wantPaths: []string{pathGirl, pathAngry, pathBoy, pathManual}},
	}

	db := newFixtureDB(t)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Given: 4 枚の画像
			// When: ページングして検索する
			res, err := db.Search(context.Background(), Query{Limit: tt.limit, Offset: tt.offset})
			if err != nil {
				t.Fatalf("Search() error = %v", err)
			}

			// Then: 総件数は絞り込み後の全体、返る件数はページ内に収まる
			if res.Total != 4 {
				t.Errorf("Total = %d, want 4", res.Total)
			}
			var paths []string
			for _, img := range res.Images {
				paths = append(paths, img.Path)
			}
			if len(paths) == 0 && len(tt.wantPaths) == 0 {
				return
			}
			if !reflect.DeepEqual(paths, tt.wantPaths) {
				t.Errorf("paths = %v, want %v", paths, tt.wantPaths)
			}
		})
	}
}

func TestFacets_絞り込み条件を反映した候補と件数を返す(t *testing.T) {
	tests := []struct {
		name  string
		query Query
		pick  func(*FacetSet) []FacetValue
		want  []FacetValue
	}{
		{
			name:  "モデルは件数の多い順に並ぶ",
			query: Query{},
			pick:  func(f *FacetSet) []FacetValue { return f.Models },
			want:  []FacetValue{{Value: "modelA", Count: 2}, {Value: "modelB", Count: 1}},
		},
		{
			name:  "LoRA は絞り込み後の集合で数える",
			query: Query{Models: []string{"modelA"}},
			pick:  func(f *FacetSet) []FacetValue { return f.Loras },
			want:  []FacetValue{{Value: "char_a", Count: 2}, {Value: "style_v3", Count: 1}},
		},
		{
			name:  "選択中の項目は切り替えられるよう他の候補も残す",
			query: Query{Models: []string{"modelA"}},
			pick:  func(f *FacetSet) []FacetValue { return f.Models },
			want:  []FacetValue{{Value: "modelA", Count: 2}, {Value: "modelB", Count: 1}},
		},
		{
			name:  "全文検索の結果もファセットに反映される",
			query: Query{Text: "1girl"},
			pick:  func(f *FacetSet) []FacetValue { return f.Models },
			want:  []FacetValue{{Value: "modelA", Count: 2}},
		},
		{
			name:  "解像度を候補として返す",
			query: Query{},
			pick:  func(f *FacetSet) []FacetValue { return f.Sizes },
			want: []FacetValue{
				{Value: "896x1152", Count: 2},
				{Value: "1024x1024", Count: 1},
				{Value: "512x512", Count: 1},
			},
		},
		{
			name:  "サンプラーを候補として返す",
			query: Query{},
			pick:  func(f *FacetSet) []FacetValue { return f.Samplers },
			want:  []FacetValue{{Value: "Euler a", Count: 2}, {Value: "DPM++ 2M", Count: 1}},
		},
		{
			name:  "ディレクトリを候補として返す",
			query: Query{},
			pick:  func(f *FacetSet) []FacetValue { return f.Dirs },
			want: []FacetValue{
				{Value: "img2img/2026-08-13", Count: 1},
				{Value: "misc", Count: 1},
				{Value: "txt2img/2026-08-12", Count: 1},
				{Value: "txt2img/2026-08-13", Count: 1},
			},
		},
	}

	db := newFixtureDB(t)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Given: 登録済みの画像群
			// When: 条件を指定してファセットを集計する
			got, err := db.Facets(context.Background(), tt.query)
			if err != nil {
				t.Fatalf("Facets() error = %v", err)
			}

			// Then: 候補と件数が期待どおりになる
			if !reflect.DeepEqual(tt.pick(got), tt.want) {
				t.Errorf("facet = %#v, want %#v", tt.pick(got), tt.want)
			}
		})
	}
}

func TestTagSuggest_入力に一致するタグを出現数の多い順に返す(t *testing.T) {
	tests := []struct {
		name   string
		prefix string
		limit  int
		want   []TagCount
	}{
		{
			name:   "前方一致するタグを出現数の多い順に返す",
			prefix: "1",
			limit:  10,
			want:   []TagCount{{Tag: "1girl", Count: 2}, {Tag: "1boy", Count: 1}},
		},
		{
			name:   "語の途中に一致するタグも候補に含め、前方一致を先に並べる",
			prefix: "hair",
			limit:  10,
			want: []TagCount{
				{Tag: "hair ornament", Count: 1},
				{Tag: "long hair", Count: 1},
				{Tag: "short hair", Count: 1},
			},
		},
		{
			name:   "件数の上限を指定できる",
			prefix: "1",
			limit:  1,
			want:   []TagCount{{Tag: "1girl", Count: 2}},
		},
		{
			name:   "一致しなければ候補を返さない",
			prefix: "unicorn",
			limit:  10,
			want:   nil,
		},
	}

	db := newFixtureDB(t)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Given: 登録済みの画像群
			// When: 入力中の文字列から候補を求める
			got, err := db.TagSuggest(context.Background(), tt.prefix, tt.limit)
			if err != nil {
				t.Fatalf("TagSuggest() error = %v", err)
			}

			// Then: 出現数の多い順に候補が返る
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestDB_インデックスの更新操作(t *testing.T) {
	tests := []struct {
		name      string
		op        func(ctx context.Context, db *DB) error
		wantErr   error
		wantPaths []string
	}{
		{
			name: "画像を削除すると一覧から消える",
			op: func(ctx context.Context, db *DB) error {
				return db.Delete(ctx, "out", pathGirl)
			},
			wantPaths: []string{pathAngry, pathBoy, pathManual},
		},
		{
			name: "ディレクトリ配下をまとめて削除できる",
			op: func(ctx context.Context, db *DB) error {
				return db.DeleteUnder(ctx, "out", "txt2img")
			},
			wantPaths: []string{pathBoy, pathManual},
		},
		{
			name: "存在しないパスの削除はエラーにならない",
			op: func(ctx context.Context, db *DB) error {
				return db.Delete(ctx, "out", "no/such.png")
			},
			wantPaths: []string{pathGirl, pathAngry, pathBoy, pathManual},
		},
		{
			name: "移動するとパスが差し替わる",
			op: func(ctx context.Context, db *DB) error {
				return db.Move(ctx, "out", pathGirl, "archive/moved.png")
			},
			wantPaths: []string{"archive/moved.png", pathAngry, pathBoy, pathManual},
		},
		{
			name: "存在しない画像の移動は見つからないことを伝える",
			op: func(ctx context.Context, db *DB) error {
				return db.Move(ctx, "out", "no/such.png", "archive/moved.png")
			},
			wantErr:   ErrNotFound,
			wantPaths: []string{pathGirl, pathAngry, pathBoy, pathManual},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Given: 登録済みの画像群
			db := newFixtureDB(t)

			// When: 更新操作を行う
			err := tt.op(context.Background(), db)

			// Then: 期待した結果とエラーになる
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("error = %v, want %v", err, tt.wantErr)
			}
			got := searchPaths(t, db, Query{})
			slices.Sort(got)
			want := slices.Clone(tt.wantPaths)
			slices.Sort(want)
			if !reflect.DeepEqual(got, want) {
				t.Errorf("paths = %v, want %v", got, want)
			}
		})
	}
}

func TestPut_登録した内容をそのまま取り出せる(t *testing.T) {
	// Given: LoRA・タグ・その他パラメータを持つ画像
	db := newTestDB(t)
	want := fixtures()[0]

	// When: 登録して ID で取得する
	put(t, db, want)
	got, err := db.Get(context.Background(), want.ID)

	// Then: 登録した内容がそのまま戻り、パスから階層とファイル名が導かれる
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got.Dir != "txt2img/2026-08-13" || got.Name != "00001.png" {
		t.Errorf("Dir = %q, Name = %q", got.Dir, got.Name)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got = %#v\nwant = %#v", got, want)
	}
}

func TestPut_同じパスへの再登録は行を置き換える(t *testing.T) {
	// Given: 登録済みの画像
	db := newTestDB(t)
	first := fixtures()[0]
	put(t, db, first)

	// When: 同じパスの内容を変えて再登録する
	second := fixtures()[0]
	second.Model = "other-model"
	second.Loras = []metadata.Lora{{Name: "new_lora", Weight: 1}}
	second.PositiveTags = []string{"2girls"}
	put(t, db, second)

	// Then: 行は増えず、同じ ID のまま内容が置き換わる
	res, err := db.Search(context.Background(), Query{})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if res.Total != 1 {
		t.Fatalf("Total = %d, want 1", res.Total)
	}
	if second.ID != first.ID {
		t.Errorf("ID = %d, want %d", second.ID, first.ID)
	}
	got, err := db.Get(context.Background(), first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Model != "other-model" {
		t.Errorf("Model = %q, want other-model", got.Model)
	}
	if !reflect.DeepEqual(got.Loras, second.Loras) {
		t.Errorf("Loras = %#v, want %#v", got.Loras, second.Loras)
	}
	if !reflect.DeepEqual(got.PositiveTags, []string{"2girls"}) {
		t.Errorf("PositiveTags = %#v", got.PositiveTags)
	}
}

func TestDelete_全文検索の索引からも取り除かれる(t *testing.T) {
	// Given: 登録済みの画像
	db := newFixtureDB(t)

	// When: 削除する
	if err := db.Delete(context.Background(), "out", pathGirl); err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	// Then: 全文検索でも見つからない
	if got := searchPaths(t, db, Query{Text: "smile"}); len(got) != 0 {
		t.Errorf("paths = %v, want empty", got)
	}
}

func TestMove_移動後も全文検索で見つかる(t *testing.T) {
	// Given: 登録済みの画像
	db := newFixtureDB(t)

	// When: 移動する
	if err := db.Move(context.Background(), "out", pathGirl, "archive/moved.png"); err != nil {
		t.Fatalf("Move() error = %v", err)
	}

	// Then: 新しいパスのまま全文検索で見つかる
	got := searchPaths(t, db, Query{Text: "smile"})
	if !reflect.DeepEqual(got, []string{"archive/moved.png"}) {
		t.Errorf("paths = %v", got)
	}
}

func TestStates_差分検出のためにルート配下の状態を返す(t *testing.T) {
	// Given: 別ルートの画像も含むインデックス
	db := newFixtureDB(t)
	other := fixtures()[0]
	other.Root = "other"
	other.Path = "elsewhere/00001.png"
	put(t, db, other)

	// When: ルートを指定して状態を取得する
	got, err := db.States(context.Background(), "out")
	if err != nil {
		t.Fatalf("States() error = %v", err)
	}

	// Then: そのルートの分だけが、サイズと更新日時つきで返る
	if len(got) != 4 {
		t.Fatalf("len = %d, want 4", len(got))
	}
	if got[pathGirl].Size != 1024 {
		t.Errorf("Size = %d, want 1024", got[pathGirl].Size)
	}
	if !got[pathGirl].ModTime.Equal(fixtureBase) {
		t.Errorf("ModTime = %v, want %v", got[pathGirl].ModTime, fixtureBase)
	}
	if got[pathGirl].ID == 0 {
		t.Error("ID = 0, want non-zero")
	}
}

func TestOpen_再度開いても登録内容が残っている(t *testing.T) {
	// Given: 画像を登録したデータベースファイル
	path := filepath.Join(t.TempDir(), "index.db")
	db, err := Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	put(t, db, fixtures()...)
	if err := db.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	// When: 同じファイルを開き直す
	reopened, err := Open(path)
	if err != nil {
		t.Fatalf("Open() error = %v", err)
	}
	defer reopened.Close()

	// Then: 登録内容が残り、全文検索も使える
	if got := searchPaths(t, reopened, Query{Text: "smile"}); !reflect.DeepEqual(got, []string{pathGirl}) {
		t.Errorf("paths = %v", got)
	}
}
