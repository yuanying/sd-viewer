# CLAUDE.md

## 起動手順

ユーザーが「起動して」と言ったら、確認を取らずに以下を実行する。

1. `go build -o sd-viewer ./cmd/sd-viewer` でビルドする
2. 現在の Herdr ペインを**下方向**に分割する（`--cwd` はプロジェクトルート、`--no-focus`）
3. 新しいペインの名前を `sd-viewer` にする
4. そのペインで `./sd-viewer --dir <出力ディレクトリ> --addr :8189 --webui-url http://localhost:7860` を実行する
5. `listening` のログを待ち、http://127.0.0.1:8189 に疎通確認して結果を報告する

- **ポートは 8189 固定**（既定の `:8080` は使わない）
- `--dir` の既定は `/home/yuanying/src/github.com/Haoming02/sd-webui-forge-classic/output`
- WebUI は IPv6 だけで待ち受けているため、`--webui-url` は `127.0.0.1` ではなく `localhost` を使う
- すでに `sd-viewer` という名前のペインが生きている場合は、分割せずそのペインを使い回す
- 「停止して」と言われたら、そのペインで動いているプロセスを止める（ペイン自体は閉じない）

Herdr の CLI の使い方は `herdr --skill` と `herdr pane` を参照する。
