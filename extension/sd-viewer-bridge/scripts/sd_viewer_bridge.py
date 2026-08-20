"""sd-viewer から届いた生成情報を txt2img / img2img の入力欄へ流し込む。

sd-viewer 本体が POST してきた内容をいったん預かり、
画面側の JavaScript が取りに来たときに WebUI 標準の
「Send to ...」と同じ経路へ乗せる。パラメータの解釈は
WebUI 側の実装（infotext_utils）にそのまま任せる。
"""

from __future__ import annotations

import threading
from pathlib import Path

import gradio as gr
from fastapi import Request
from fastapi.responses import JSONResponse
from PIL import Image

import modules.infotext_utils as parameters_copypaste
from modules import images, script_callbacks

# 反映先として受け付けるタブ。
TARGETS = ("txt2img", "img2img")

# 画像として受け取る拡張子。これ以外は開かない。
IMAGE_SUFFIXES = {".png", ".jpg", ".jpeg", ".webp", ".avif"}


class Inbox:
    """届いた生成情報を 1 件だけ預かる。

    続けて送られた場合は新しい方だけを残す。
    画面が取りに来る前に上書きされても、利用者の意図は最後の 1 件にある。
    """

    def __init__(self) -> None:
        self._lock = threading.Lock()
        self._pending: dict | None = None
        self._seq = 0

    def put(self, target: str, infotext: str, image_path: str) -> int:
        with self._lock:
            self._seq += 1
            self._pending = {
                "target": target,
                "infotext": infotext,
                "image_path": image_path,
                "seq": self._seq,
            }
            return self._seq

    def seq(self) -> int:
        """取りに来ていない預かりものの番号を返す。何もなければ 0。"""
        with self._lock:
            return self._pending["seq"] if self._pending else 0

    def take(self) -> dict | None:
        with self._lock:
            pending, self._pending = self._pending, None
            return pending


inbox = Inbox()


def _load_image(path: str) -> Image.Image | None:
    """受け取った位置から画像を読む。読めないものは黙って諦める。"""
    if not path:
        return None
    file = Path(path)
    if file.suffix.lower() not in IMAGE_SUFFIXES or not file.is_file():
        return None
    try:
        with Image.open(file) as opened:
            return opened.copy()
    except Exception:
        return None


def _take_pending() -> tuple[Image.Image | None, str, str]:
    """預かっている 1 件を画面の各欄へ配れる形にして返す。"""
    pending = inbox.take()
    if pending is None:
        return None, "", ""

    image = _load_image(pending["image_path"])
    infotext = pending["infotext"]
    # sd-viewer 側で生成情報が取れていなくても、画像に埋まっていれば拾う。
    if not infotext and image is not None:
        embedded, _ = images.read_info_from_image(image)
        infotext = embedded or ""

    return image, infotext, pending["target"]


def _register_routes(_demo, app) -> None:
    """sd-viewer 本体との受け渡し口を生やす。

    このファイルは注釈を文字列として持つため、FastAPI が型を解けるよう
    引数の型はモジュールの直下に取り込んだものだけを使う。
    """

    @app.post("/sd-viewer-bridge/send")
    async def send(request: Request):
        try:
            body = await request.json()
        except Exception:
            return JSONResponse({"detail": "invalid json"}, status_code=400)

        target = str(body.get("target") or "")
        if target not in TARGETS:
            return JSONResponse({"detail": f"unknown target: {target}"}, status_code=400)

        seq = inbox.put(
            target,
            str(body.get("infotext") or ""),
            str(body.get("image_path") or ""),
        )
        return {"seq": seq}

    @app.get("/sd-viewer-bridge/pending")
    async def pending():
        # 画面はこの番号の変化だけを見て、取りに行くかどうかを決める。
        return {"seq": inbox.seq()}


def _build_tab():
    """受け取った内容の確認と、手動で送り直すための画面を組む。"""
    with gr.Blocks(analytics_enabled=False) as tab:
        gr.HTML(
            '<p style="margin:8px 0 4px">sd-viewer から送られた生成情報がここに届き、'
            "自動で txt2img / img2img へ反映される。"
            "うまく反映されなかったときは下のボタンで送り直せる。</p>"
        )

        with gr.Row():
            image = gr.Image(label="受け取った画像", type="pil", interactive=False)
            with gr.Column():
                infotext = gr.Textbox(label="生成情報", lines=10, interactive=False)
                with gr.Row():
                    buttons = {
                        target: gr.Button(
                            f"Send to {target}",
                            elem_id=f"sd_viewer_bridge_send_{target}",
                        )
                        for target in TARGETS
                    }

        # 画面側の JavaScript がこの 2 つを操作して受け渡しを進める。
        target_box = gr.Textbox(visible=False, elem_id="sd_viewer_bridge_target")
        pull = gr.Button(visible=False, elem_id="sd_viewer_bridge_pull")

        pull.click(
            fn=_take_pending,
            inputs=[],
            outputs=[image, infotext, target_box],
            show_progress=False,
        ).then(fn=None, inputs=[target_box], outputs=[], js="sdViewerBridgeDispatch")

        # 実際の反映は WebUI 標準の仕組みに任せる。
        # 結線は全タブの構築後に行われるため、ここでは登録だけしておく。
        for target, button in buttons.items():
            parameters_copypaste.register_paste_params_button(
                parameters_copypaste.ParamBinding(
                    paste_button=button,
                    tabname=target,
                    source_text_component=infotext,
                    source_image_component=image,
                )
            )

    return [(tab, "sd-viewer", "sd_viewer_bridge")]


script_callbacks.on_app_started(_register_routes)
script_callbacks.on_ui_tabs(_build_tab)
