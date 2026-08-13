# sd-viewer

Stable Diffusion WebUI (AUTOMATIC1111 / Forge) の出力ディレクトリを対象とした、ローカル画像ビューワー。

PNG に埋め込まれた生成パラメータを解析してインデックス化し、モデル名・LoRA・タグ（プロンプト内の語）・日付などで検索・絞り込みができる。

## 特徴

- **メタデータ解析** — PNG の `parameters` チャンク（A1111 / Forge 形式）から prompt / negative prompt / Steps / Sampler / CFG / Seed / Model / LoRA などを構造化して取り込む
- **高速検索** — SQLite (FTS5) にインデックスを構築。数万枚でも即座に絞り込める
- **ファセット絞り込み** — モデル・LoRA・Sampler・解像度・日付・フォルダを件数付きで一覧し、組み合わせて絞り込む
- **タグ補完** — プロンプト中の語を出現頻度付きでサジェスト
- **自動追従** — ファイルシステムイベントを監視し、画像の追加・移動・削除をリアルタイムにインデックスへ反映
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

### 主なオプション

| オプション | 既定値 | 説明 |
| --- | --- | --- |
| `--dir` | （必須） | 監視対象の出力ディレクトリ。複数指定可 |
| `--addr` | `:8080` | 待ち受けアドレス |
| `--data-dir` | `~/.cache/sd-viewer` | インデックス DB とサムネイルの保存先 |
| `--thumb-size` | `512` | サムネイルの長辺ピクセル数 |
| `--no-watch` | `false` | ファイル監視を無効にし、起動時スキャンのみ行う |
| `-v` | `false` | 詳細なログを出力する |

初回起動時に全画像を走査してインデックスとサムネイルを作る。手元の環境では 3,500 枚で 45 秒ほど、インデックスが 21MB、サムネイルが 109MB だった。2 回目以降は更新のあったファイルだけを読み直す。

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
