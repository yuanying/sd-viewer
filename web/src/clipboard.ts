/**
 * copyText はテキストをクリップボードへ写す。
 *
 * navigator.clipboard は https か localhost でしか生えないため、
 * ホスト名や IP で開いているときは選択と複製の古い手順へ切り替える。
 * 写せなかった場合は呼び出し側が気づけるように失敗させる。
 */
export async function copyText(text: string): Promise<void> {
  if (navigator.clipboard?.writeText) {
    try {
      await navigator.clipboard.writeText(text);
      return;
    } catch {
      // 権限が下りない場合もあるので、下の手順へ落とす。
    }
  }
  copyBySelection(text);
}

/** copyBySelection は画面外のテキストエリアを選択して複製する。 */
function copyBySelection(text: string): void {
  const area = document.createElement("textarea");
  area.value = text;
  // 画面に見えず、選択したときに巻き戻らない位置へ置く。
  area.setAttribute("readonly", "");
  area.style.position = "fixed";
  area.style.top = "-1000px";
  area.style.opacity = "0";
  document.body.appendChild(area);

  try {
    area.select();
    area.setSelectionRange(0, text.length);
    if (!document.execCommand?.("copy")) {
      throw new Error("クリップボードへ写せませんでした");
    }
  } finally {
    area.remove();
  }
}
