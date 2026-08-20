/**
 * sd-viewer から届いた生成情報を取りに行く。
 *
 * 拡張の Python 側が預かり番号を返すので、それが変わったときだけ
 * 隠しボタンを押して受け取りを進める。以降の反映は Gradio 側の
 * 連鎖で進むため、ここでは押すことしかしない。
 */

// 反映先のタブへ送るボタンを押す。Gradio の .then から呼ばれる。
function sdViewerBridgeDispatch(target) {
    if (!target) {
        return;
    }
    const button = gradioApp().querySelector(`#sd_viewer_bridge_send_${target}`);
    if (button) {
        button.click();
    }
}

(function () {
    const INTERVAL_MS = 1500;
    let lastSeq = 0;

    async function poll() {
        // 見ていないタブで受け取ると、どこへ反映されたか分からなくなる。
        if (document.hidden) {
            return;
        }
        let seq;
        try {
            const res = await fetch("/sd-viewer-bridge/pending", { cache: "no-store" });
            if (!res.ok) {
                return;
            }
            seq = (await res.json()).seq;
        } catch (e) {
            // WebUI の起動直後や再読み込み中は届かない。次の周期で試す。
            return;
        }

        if (!seq || seq === lastSeq) {
            return;
        }
        lastSeq = seq;
        gradioApp().querySelector("#sd_viewer_bridge_pull")?.click();
    }

    onUiLoaded(function () {
        setInterval(poll, INTERVAL_MS);
    });
})();
