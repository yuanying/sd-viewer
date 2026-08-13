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

## 使い方

```console
$ sd-viewer --dir /path/to/stable-diffusion-webui/output
```

起動後、ブラウザで http://localhost:8080 を開く。

### 主なオプション

| オプション | 既定値 | 説明 |
| --- | --- | --- |
| `--dir` | （必須） | 監視対象の出力ディレクトリ。複数指定可 |
| `--addr` | `:8080` | 待ち受けアドレス |
| `--data-dir` | `~/.cache/sd-viewer` | インデックス DB とサムネイルの保存先 |
| `--thumb-size` | `512` | サムネイルの長辺ピクセル数 |
| `--no-watch` | `false` | ファイル監視を無効にし、起動時スキャンのみ行う |

## 開発

```console
$ make test      # Go のテストを実行
$ make dev       # API サーバと Vite dev server を起動
$ make build     # フロントエンドをビルドして単一バイナリを生成
```

設計方針は [docs/design.md](docs/design.md) を参照。
