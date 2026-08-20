"""sd-viewer 本体との受け渡し口を、実際の FastAPI に載せて確かめる。

fastapi が入っていない環境では飛ばす。WebUI の venv で動かすと通る。
"""

from __future__ import annotations

import unittest

try:
    from fastapi import FastAPI
    from fastapi.testclient import TestClient

    HAS_FASTAPI = True
except ImportError:  # pragma: no cover - 環境依存
    HAS_FASTAPI = False

from test_sd_viewer_bridge import bridge


@unittest.skipUnless(HAS_FASTAPI, "fastapi が入っていない")
class RoutesTest(unittest.TestCase):
    def setUp(self) -> None:
        bridge.inbox = bridge.Inbox()
        app = FastAPI()
        bridge._register_routes(None, app)
        self.client = TestClient(app)

    def test_送られてきた内容を本文から受け取る(self):
        res = self.client.post(
            "/sd-viewer-bridge/send",
            json={"target": "img2img", "infotext": "1girl", "image_path": "/tmp/a.png"},
        )

        self.assertEqual(res.status_code, 200, res.text)
        self.assertEqual(res.json()["seq"], 1)

        pending = bridge.inbox.take()
        self.assertEqual(pending["target"], "img2img")
        self.assertEqual(pending["infotext"], "1girl")
        self.assertEqual(pending["image_path"], "/tmp/a.png")

    def test_生成情報と画像の位置は省いてもよい(self):
        res = self.client.post("/sd-viewer-bridge/send", json={"target": "txt2img"})

        self.assertEqual(res.status_code, 200, res.text)
        pending = bridge.inbox.take()
        self.assertEqual(pending["infotext"], "")
        self.assertEqual(pending["image_path"], "")

    def test_知らない送り先は受け取らない(self):
        res = self.client.post("/sd-viewer-bridge/send", json={"target": "extras"})

        self.assertEqual(res.status_code, 400)
        self.assertEqual(bridge.inbox.seq(), 0)

    def test_本文が壊れていれば受け取らない(self):
        res = self.client.post(
            "/sd-viewer-bridge/send",
            content=b"not json",
            headers={"Content-Type": "application/json"},
        )

        self.assertEqual(res.status_code, 400)
        self.assertEqual(bridge.inbox.seq(), 0)

    def test_預かり番号を問い合わせられる(self):
        self.assertEqual(self.client.get("/sd-viewer-bridge/pending").json()["seq"], 0)

        self.client.post("/sd-viewer-bridge/send", json={"target": "txt2img"})

        self.assertEqual(self.client.get("/sd-viewer-bridge/pending").json()["seq"], 1)


if __name__ == "__main__":
    unittest.main()
