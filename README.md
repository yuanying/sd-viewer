# sd-viewer

Stable Diffusion WebUI (AUTOMATIC1111 / Forge) の出力ディレクトリを対象とした、ローカル画像ビューワー。

PNG に埋め込まれた生成パラメータを解析してインデックス化し、モデル名・LoRA・タグ（プロンプト内の語）・日付などで検索・絞り込みができる。

## 特徴

- **メタデータ解析** — PNG の `parameters` チャンク（A1111 / Forge 形式）から prompt / negative prompt / Steps / Sampler / CFG / Seed / Model / LoRA などを構造化して取り込む
- **高速検索** — SQLite (FTS5) にインデックスを構築。数万枚でも即座に絞り込める
- **ファセット絞り込み** — モデル・LoRA・Sampler・解像度・日付・フォルダを件数付きで一覧し、組み合わせて絞り込む
- **タグ補完** — プロンプト中の語を出現頻度付きでサジェスト
- **自動追従** — ファイルシステムイベントを監視し、画像の追加・移動・削除をリアルタイムにインデックスへ反映
- **WebUI へ送る** — 表示中の生成情報を WebUI の txt2img / img2img の入力欄へそのまま流し込む（別途 [拡張](extension/sd-viewer-bridge/) が要る）
- **ゴミ箱** — 要らない画像をまとめて退避し、あとで元に戻すか完全に削除するかを決められる。ゴミ箱はルートごと
- **Fav** — 気に入った画像に星を付けておき、ほかの絞り込みと組み合わせて見返せる
- **設定ファイル** — 監視対象や待ち受けアドレスを `~/.config/sd-viewer/config.toml` に書いておける
- **単一バイナリ** — フロントエンドを埋め込んだ 1 ファイルで動作

## ビルド

Go 1.26 以上と Node.js 20.19 以上（または 22.12 以上）が必要。

```console
$ make build
```

画面をビルドしてバイナリへ埋め込み、カレントディレクトリに `sd-viewer` を作る。

## 使い方

```console
$ ./sd-viewer --dir /path/to/stable-diffusion-webui/output
```

起動後、ブラウザで http://localhost:8080 を開く。

出力ディレクトリが複数ある場合は `--dir` を並べる。それぞれディレクトリ名がルート名になり、検索条件として選べる。

```console
$ ./sd-viewer --dir ~/sd/output --dir /mnt/nas/sd-archive
```

毎回同じ指定をするなら、設定ファイルに書いておくと引数なしで起動できる。

### 設定ファイル

`~/.config/sd-viewer/config.toml`（`XDG_CONFIG_HOME` があればその下）に置く。
すべての項目を省略でき、書いたものだけが既定を上書きする。

```toml
addr = ":9000"
webui-url = "http://localhost:7860"

# 監視する出力ディレクトリ。並べた数だけルートになる。
[[dir]]
name = "forge"                       # 省略するとディレクトリ名（この例では output）
path = "~/sd/stable-diffusion-webui/output"

[[dir]]
path = "/mnt/nas/sd-archive"
```

`name` を付けておくと、ディレクトリを引っ越しても検索条件の意味が変わらない。
省略した場合はディレクトリ名を使い、重なるときは `images`, `images-2` のように連番で分ける。

置き場所を変えたい場合は `--config` で渡す。

```console
$ ./sd-viewer --config /etc/sd-viewer.toml
```

### 主なオプション

コマンドラインは設定ファイルより優先される。実際に渡した項目だけが上書きされるので、
`--addr` だけを渡しても設定ファイルの他の項目はそのまま残る。

| オプション | 設定ファイルのキー | 既定値 | 説明 |
| --- | --- | --- | --- |
| `--dir` | `[[dir]]` の `path` | （必須） | 監視対象の出力ディレクトリ。複数指定可。渡すと設定ファイルの `dir` を置き換える |
| `--addr` | `addr` | `:8080` | 待ち受けアドレス |
| `--webui-url` | `webui-url` | （なし） | 生成情報の送り先となる WebUI の URL。指定すると送信ボタンが出る |
| `--data-dir` | `data-dir` | `~/.cache/sd-viewer` | インデックス DB とサムネイルの保存先 |
| `--thumb-size` | `thumb-size` | `512` | サムネイルの長辺ピクセル数 |
| `--no-watch` | `no-watch` | `false` | ファイル監視を無効にし、起動時スキャンのみ行う |
| `-v` | `verbose` | `false` | 詳細なログを出力する |
| `--config` | — | `~/.config/sd-viewer/config.toml` | 設定ファイルの位置 |

初回起動時に全画像を走査してインデックスとサムネイルを作る。手元の環境では 3,500 枚で 45 秒ほど、インデックスが 21MB、サムネイルが 109MB だった。2 回目以降は更新のあったファイルだけを読み直す。

### WebUI へ送る

詳細パネルの「txt2img へ送る」「img2img へ送る」で、表示中の生成情報を WebUI の入力欄へ
そのまま入れられる。img2img では元画像が初期画像として入る。

使うには WebUI 側へ [sd-viewer-bridge 拡張](extension/sd-viewer-bridge/) を入れ、
sd-viewer には送り先を渡して起動する。

```console
$ ln -s "$PWD/extension/sd-viewer-bridge" /path/to/stable-diffusion-webui/extensions/sd-viewer-bridge
$ ./sd-viewer --dir ~/sd/output --webui-url http://localhost:7860
```

設定ファイルに書く場合は `webui-url` を使う。

送り先が届かないときは、WebUI の待ち受けアドレスを確かめる。`--listen` なしの WebUI が
IPv6 だけで待ち受けている場合、`http://127.0.0.1:7860` では届かず `http://localhost:7860`
なら届く、といったことが起こる。

画像はファイルの位置で受け渡すため、sd-viewer と WebUI は同じホストで動かす必要がある。
ブラウザは sd-viewer としか通信しないので、別のホストから見ていても構わない。

### ゴミ箱

サムネイルの角のチェックボックスで画像を選び、上部の「ゴミ箱へ移動」で退避する。Shift を
押しながら選ぶと範囲をまとめて選べる。詳細パネルからも開いている 1 枚を入れられる。

ゴミ箱へ入れた画像は一覧・検索・ファセットのいずれにも出てこなくなる。実体はルート直下の
`.trash` へ元の相対パスのまま移すため、WebUI のギャラリーやファイルマネージャからも消える。

ヘッダの「ゴミ箱」を押すと中身をルートごとに一覧できる。画像ごとに「元に戻す」か「完全に
削除」を選べるほか、ルート単位で「空にする」とまとめて消せる。完全に削除したものは戻せない。

### Fav

サムネイルの右上の星を押すと Fav になり、もう一度押すと外れる。選択中の画像をまとめて
「Fav に追加」することも、詳細パネルから付け外しすることもできる。

ヘッダの件数の横にある ☆ を押すと ★ に変わり、Fav の画像だけに絞り込む。検索語・ファセット・ルートとも
組み合わせられる。Fav は sd-viewer のインデックスにだけ記録し、画像ファイルには手を触れない。
ゴミ箱へ入れた画像は Fav でも一覧に出ないが、元に戻せば Fav のまま戻ってくる。

### 待ち受けアドレス

既定の `:8080` は IPv4 と IPv6 の両方で待ち受ける。アドレスを絞りたい場合は `--addr` で指定する。

```console
$ ./sd-viewer --dir ~/sd/output --addr 127.0.0.1:8080          # 同じ端末からのみ
$ ./sd-viewer --dir ~/sd/output --addr "[::]:8080"             # IPv6 のすべてのアドレス
$ ./sd-viewer --dir ~/sd/output --addr "[2001:db8::1]:8080"    # 特定のアドレスのみ
```

**認証の仕組みは持たない。** 外部から届くアドレスで待ち受けると、URL を知っている人は誰でも画像とプロンプトを閲覧できる。共有したい相手が限られる場合は、ファイアウォールや前段のリバースプロキシで制限する。

## 開発

```console
$ make build     # 画面をビルドして単一バイナリを生成
$ make test      # Go と画面のテストを実行
$ make dev       # 開発時の起動手順を表示
```

開発中は API サーバと Vite dev server を別々に起動する。画面は http://localhost:5173 で、API へのリクエストは Vite が中継する。

```console
$ go run ./cmd/sd-viewer --dir ~/sd/output   # 1 つ目の端末
$ cd web && npm run dev                      # 2 つ目の端末
```

中継先は既定で `http://127.0.0.1:8080`。別のアドレスで動かしている場合は環境変数 `SD_VIEWER_API` で変える。

設計方針は [docs/design.md](docs/design.md) を参照。
