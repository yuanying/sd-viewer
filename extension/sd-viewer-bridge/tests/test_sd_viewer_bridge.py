"""拡張の受け渡しの振る舞いを確かめる。

WebUI の中でしか動かない部分は差し替え、
預かりものの扱いと画像の読み取りだけを対象にする。
"""

from __future__ import annotations

import importlib.util
import sys
import tempfile
import types
import unittest
from pathlib import Path

from PIL import Image, PngImagePlugin

SCRIPT = Path(__file__).resolve().parents[1] / "scripts" / "sd_viewer_bridge.py"


def _install_stubs() -> None:
    """WebUI の中でしか揃わない依存を、呼べるだけの形に置き換える。"""
    sys.modules.setdefault("gradio", types.ModuleType("gradio"))

    # fastapi はここでは注釈として現れるだけなので、無い環境では形だけ用意する。
    try:
        import fastapi  # noqa: F401
    except ImportError:
        fastapi = types.ModuleType("fastapi")
        fastapi.Request = object
        responses = types.ModuleType("fastapi.responses")
        responses.JSONResponse = object
        fastapi.responses = responses
        sys.modules["fastapi"] = fastapi
        sys.modules["fastapi.responses"] = responses

    infotext_utils = types.ModuleType("modules.infotext_utils")
    infotext_utils.register_paste_params_button = lambda binding: None
    infotext_utils.ParamBinding = object

    images = types.ModuleType("modules.images")

    def read_info_from_image(image):
        return image.info.get("parameters"), None

    images.read_info_from_image = read_info_from_image

    script_callbacks = types.ModuleType("modules.script_callbacks")
    script_callbacks.on_app_started = lambda callback: None
    script_callbacks.on_ui_tabs = lambda callback: None

    modules = types.ModuleType("modules")
    modules.__path__ = []
    modules.images = images
    modules.script_callbacks = script_callbacks
    modules.infotext_utils = infotext_utils

    sys.modules["modules"] = modules
    sys.modules["modules.images"] = images
    sys.modules["modules.script_callbacks"] = script_callbacks
    sys.modules["modules.infotext_utils"] = infotext_utils


def _load_bridge():
    _install_stubs()
    spec = importlib.util.spec_from_file_location("sd_viewer_bridge", SCRIPT)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


bridge = _load_bridge()


def _write_png(path: Path, parameters: str | None = None) -> None:
    image = Image.new("RGB", (8, 12), (10, 20, 30))
    info = PngImagePlugin.PngInfo()
    if parameters is not None:
        info.add_text("parameters", parameters)
    image.save(path, pnginfo=info)


class InboxTest(unittest.TestCase):
    def setUp(self) -> None:
        self.inbox = bridge.Inbox()

    def test_何も届いていなければ番号は立たない(self):
        self.assertEqual(self.inbox.seq(), 0)
        self.assertIsNone(self.inbox.take())

    def test_預かると番号が進む(self):
        first = self.inbox.put("txt2img", "a", "/tmp/a.png")
        self.assertEqual(self.inbox.seq(), first)

        second = self.inbox.put("img2img", "b", "/tmp/b.png")
        self.assertGreater(second, first)

    def test_取りに来る前に届いたら新しい方だけ残る(self):
        self.inbox.put("txt2img", "古い", "/tmp/a.png")
        self.inbox.put("img2img", "新しい", "/tmp/b.png")

        pending = self.inbox.take()
        self.assertEqual(pending["target"], "img2img")
        self.assertEqual(pending["infotext"], "新しい")

    def test_一度取ったら残らない(self):
        self.inbox.put("txt2img", "a", "/tmp/a.png")

        self.assertIsNotNone(self.inbox.take())
        self.assertIsNone(self.inbox.take())
        self.assertEqual(self.inbox.seq(), 0)


class LoadImageTest(unittest.TestCase):
    def setUp(self) -> None:
        self._tmp = tempfile.TemporaryDirectory()
        self.dir = Path(self._tmp.name)
        self.addCleanup(self._tmp.cleanup)

    def test_画像なら読める(self):
        path = self.dir / "a.png"
        _write_png(path)

        image = bridge._load_image(str(path))
        self.assertIsNotNone(image)
        self.assertEqual(image.size, (8, 12))

    def test_受け取れない位置は諦める(self):
        cases = {
            "位置が空": "",
            "ない位置": str(self.dir / "missing.png"),
            "ディレクトリ": str(self.dir),
        }
        for name, path in cases.items():
            with self.subTest(name):
                self.assertIsNone(bridge._load_image(path))

    def test_画像でない拡張子は開かない(self):
        path = self.dir / "a.txt"
        path.write_text("not an image")

        self.assertIsNone(bridge._load_image(str(path)))


class TakePendingTest(unittest.TestCase):
    def setUp(self) -> None:
        self._tmp = tempfile.TemporaryDirectory()
        self.dir = Path(self._tmp.name)
        self.addCleanup(self._tmp.cleanup)
        bridge.inbox = bridge.Inbox()

    def test_届いた内容を各欄へ配れる形で返す(self):
        path = self.dir / "a.png"
        _write_png(path)
        bridge.inbox.put("img2img", "1girl\nSteps: 20", str(path))

        image, infotext, target = bridge._take_pending()

        self.assertIsNotNone(image)
        self.assertEqual(infotext, "1girl\nSteps: 20")
        self.assertEqual(target, "img2img")

    def test_生成情報が空なら画像に埋まっている分を拾う(self):
        path = self.dir / "a.png"
        _write_png(path, parameters="埋め込みの生成情報")
        bridge.inbox.put("txt2img", "", str(path))

        _, infotext, _ = bridge._take_pending()

        self.assertEqual(infotext, "埋め込みの生成情報")

    def test_何も届いていなければ空で返す(self):
        self.assertEqual(bridge._take_pending(), (None, "", ""))


if __name__ == "__main__":
    unittest.main()
