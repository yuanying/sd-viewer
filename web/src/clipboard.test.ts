import { afterEach, describe, expect, it, vi } from "vitest";
import { copyText } from "./clipboard";

/** setClipboard は navigator.clipboard の有無を差し替える。 */
function setClipboard(value: unknown) {
  Object.defineProperty(navigator, "clipboard", {
    value,
    configurable: true,
    writable: true,
  });
}

afterEach(() => {
  setClipboard(undefined);
  Reflect.deleteProperty(document, "execCommand");
});

describe("copyText", () => {
  it("クリップボード API が使えるならそれで写す", async () => {
    const writeText = vi.fn(async () => {});
    setClipboard({ writeText });

    await copyText("hello");

    expect(writeText).toHaveBeenCalledWith("hello");
  });

  // http でホスト名を指定して開くと navigator.clipboard が生えない。
  it("クリップボード API がなくても選択と複製で写す", async () => {
    setClipboard(undefined);
    const execCommand = vi.fn(() => true);
    Object.defineProperty(document, "execCommand", { value: execCommand, configurable: true });

    await copyText("fallback text");

    expect(execCommand).toHaveBeenCalledWith("copy");
    // 後始末として差し込んだ要素が残らない。
    expect(document.querySelectorAll("textarea")).toHaveLength(0);
  });

  it("クリップボード API が拒まれたら複製へ切り替える", async () => {
    setClipboard({
      writeText: vi.fn(async () => {
        throw new Error("denied");
      }),
    });
    const execCommand = vi.fn(() => true);
    Object.defineProperty(document, "execCommand", { value: execCommand, configurable: true });

    await copyText("retry");

    expect(execCommand).toHaveBeenCalledWith("copy");
  });

  it("どちらも使えなければ失敗として伝える", async () => {
    setClipboard(undefined);
    Object.defineProperty(document, "execCommand", {
      value: vi.fn(() => false),
      configurable: true,
    });

    await expect(copyText("nope")).rejects.toThrow();
  });
});
