# CLAUDE.md

## 常駐と運用

boucherie では、sd-viewer は dotfiles の `devbox/apps` で `sd-viewer` コンテナとして常駐している。
コンテナは起動のたびにこのチェックアウトから `go build` し直すので、ソースの変更は
`docker restart sd-viewer` で反映される。起動・停止・ログの見方など運用の手順は
dotfiles の `devbox/apps/README.md` を見る。

- 監視するディレクトリ・待ち受けアドレス・WebUI の送り先は、いずれも
  `~/.config/sd-viewer/config.toml` に書いてある。**この環境に固有の値をこのリポジトリへ
  書き足さないこと。** 値を知りたいときは設定ファイルを読む
