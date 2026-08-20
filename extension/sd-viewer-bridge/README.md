# sd-viewer-bridge

sd-viewer で見ている画像の生成情報を、Stable Diffusion WebUI（AUTOMATIC1111 / Forge）の
txt2img・img2img の入力欄へそのまま送り込むための拡張。

## 入れ方

WebUI の `extensions/` から、この中身へシンボリックリンクを張る。

```console
$ ln -s /path/to/sd-viewer/extension/sd-viewer-bridge \
        /path/to/stable-diffusion-webui/extensions/sd-viewer-bridge
```

WebUI を再起動すると `sd-viewer` タブが増える。

sd-viewer 側は WebUI の URL を渡して起動する。

```console
$ sd-viewer --dir ~/sd/output --webui-url http://127.0.0.1:7860
```

## 使い方

sd-viewer の詳細パネルにある「txt2img へ送る」「img2img へ送る」を押すと、
WebUI の該当タブへプロンプトと各設定が入り、タブが切り替わる。
img2img では元画像が初期画像として入る。

自動で反映されなかったときは、`sd-viewer` タブに受け取った内容が残っているので、
そこの「Send to txt2img」「Send to img2img」で送り直せる。

## 仕組み

sd-viewer 本体が `POST /sd-viewer-bridge/send` に生成情報と画像の位置を渡し、
この拡張が 1 件だけ預かる。WebUI の画面は `GET /sd-viewer-bridge/pending` を
定期的に見て、預かり番号が変わったときだけ受け取りに行く。

反映そのものは WebUI 標準の `infotext_utils` へ委ねているため、
パラメータの解釈は PNG Info タブの「Send to ...」と同じになる。

ブラウザは sd-viewer としか通信しない。WebUI へは sd-viewer 本体が中継するので、
別のホストから見ていても追加の設定は要らない。

## 前提

- sd-viewer と WebUI が同じホストで動いていること。画像はファイルの位置で受け渡す
- 認証の仕組みは持たない。受け渡し口は WebUI の待ち受けアドレスにそのまま生える
