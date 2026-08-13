import { cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import App from "./App";
import type { FacetSet, Image, SearchResult, Status, TagCount } from "./types";

/** requests は画面が投げた API のパスを順に覚える。 */
let requests: string[] = [];

function image(id: number, over: Partial<Image> = {}): Image {
  return {
    id,
    root: "out",
    path: `txt2img/2026-08-13/0000${id}.png`,
    dir: "txt2img/2026-08-13",
    name: `0000${id}.png`,
    size: 1024,
    mod_time: "2026-08-13T12:00:00Z",
    width: 512,
    height: 768,
    created_at: "2026-08-13T12:00:00Z",
    has_params: true,
    prompt: "1girl, smile",
    negative: "watermark",
    model: "modelA",
    model_hash: "aaaa",
    sampler: "Euler a",
    schedule_type: "Karras",
    steps: 28,
    cfg_scale: 5,
    seed: 1,
    denoising: 0,
    version: "demo",
    gen_width: 512,
    gen_height: 768,
    loras: [{ name: "style_v3", weight: 0.8 }],
    ...over,
  };
}

const facets: FacetSet = {
  models: [
    { value: "modelA", count: 2 },
    { value: "modelB", count: 1 },
  ],
  loras: [{ value: "style_v3", count: 2 }],
  samplers: [{ value: "Euler a", count: 3 }],
  sizes: [{ value: "512x768", count: 3 }],
  dirs: [{ value: "txt2img/2026-08-13", count: 3 }],
  roots: [{ value: "out", count: 3 }],
};

const status: Status = {
  total: 3,
  roots: ["out"],
  scan: { scanning: false, walked: 3, indexed: 3, removed: 0, failed: 0 },
  thumbnails: 3,
};

/** stubFetch は API の応答を差し替える。 */
function stubFetch() {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input);
      requests.push(url);

      const respond = (body: unknown) =>
        new Response(JSON.stringify(body), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });

      if (url.startsWith("/api/images/")) {
        const id = Number(url.slice("/api/images/".length));
        return respond(image(id, { positive_tags: ["1girl", "smile"], raw: "raw text" }));
      }
      if (url.startsWith("/api/images")) {
        const params = new URLSearchParams(url.split("?")[1] ?? "");
        const all = [image(1), image(2), image(3, { model: "modelB" })];
        const models = params.getAll("model");
        const images = models.length === 0 ? all : all.filter((i) => models.includes(i.model));
        return respond({ total: images.length, images } satisfies SearchResult);
      }
      if (url.startsWith("/api/facets")) {
        return respond(facets);
      }
      if (url.startsWith("/api/tags")) {
        return respond([{ tag: "smile", count: 2 }] satisfies TagCount[]);
      }
      if (url.startsWith("/api/status")) {
        return respond(status);
      }
      return new Response("not found", { status: 404 });
    }),
  );
}

/** EventSource は happy-dom にないため、何もしない実装で置き換える。 */
class StubEventSource {
  onmessage: ((event: MessageEvent) => void) | null = null;
  close() {}
}

beforeEach(() => {
  requests = [];
  window.history.replaceState(null, "", "/");
  stubFetch();
  vi.stubGlobal("EventSource", StubEventSource);
  vi.stubGlobal("IntersectionObserver", MutationObserverStub);
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

/** IntersectionObserver も happy-dom にないため差し替える。 */
class MutationObserverStub {
  observe() {}
  disconnect() {}
}

describe("App", () => {
  it("起動すると一覧と絞り込み候補を読み込む", async () => {
    render(<App />);

    await waitFor(() => expect(screen.getAllByRole("button", { name: /0000/ })).toHaveLength(3));
    expect(screen.getByText("3 件")).toBeTruthy();
    expect(requests.some((url) => url.startsWith("/api/images?"))).toBe(true);
    expect(requests.some((url) => url.startsWith("/api/facets"))).toBe(true);
  });

  it("ファセットを選ぶと条件つきで読み直しアドレスにも残す", async () => {
    const user = userEvent.setup();
    render(<App />);
    await waitFor(() => expect(screen.getAllByRole("button", { name: /0000/ })).toHaveLength(3));

    await user.click(screen.getByRole("checkbox", { name: /modelB/ }));

    await waitFor(() =>
      expect(requests.some((url) => url.includes("model=modelB"))).toBe(true),
    );
    expect(window.location.search).toBe("?model=modelB");
    await waitFor(() => expect(screen.getAllByRole("button", { name: /0000/ })).toHaveLength(1));
  });

  it("アドレスに条件があれば復元して読み込む", async () => {
    window.history.replaceState(null, "", "/?model=modelB");
    render(<App />);

    await waitFor(() => expect(screen.getAllByRole("button", { name: /0000/ })).toHaveLength(1));
    expect(screen.getByRole("checkbox", { name: /modelB/ })).toBeTruthy();
  });

  it("画像を選ぶと生成情報を表示する", async () => {
    const user = userEvent.setup();
    render(<App />);
    await waitFor(() => expect(screen.getAllByRole("button", { name: /0000/ })).toHaveLength(3));

    await user.click(screen.getAllByRole("button", { name: /0000/ })[0]);

    const detail = await screen.findByText("00001.png");
    const panel = detail.closest(".detail-meta") as HTMLElement;
    expect(within(panel).getByText("1girl, smile")).toBeTruthy();
    expect(within(panel).getByRole("button", { name: "modelA" })).toBeTruthy();
    expect(within(panel).getByRole("button", { name: /style_v3/ })).toBeTruthy();
  });

  it("詳細からタグで絞り込むと一覧へ戻る", async () => {
    const user = userEvent.setup();
    render(<App />);
    await waitFor(() => expect(screen.getAllByRole("button", { name: /0000/ })).toHaveLength(3));
    await user.click(screen.getAllByRole("button", { name: /0000/ })[0]);
    await screen.findByText("00001.png");

    await user.click(screen.getByRole("button", { name: "smile" }));

    await waitFor(() => expect(screen.queryByText("00001.png")).toBeNull());
    expect(window.location.search).toBe("?tag=smile");
  });

  it("条件をすべて解除すると元の一覧に戻る", async () => {
    const user = userEvent.setup();
    window.history.replaceState(null, "", "/?model=modelB");
    render(<App />);
    await waitFor(() => expect(screen.getAllByRole("button", { name: /0000/ })).toHaveLength(1));

    await user.click(screen.getByRole("button", { name: "条件をすべて解除" }));

    await waitFor(() => expect(screen.getAllByRole("button", { name: /0000/ })).toHaveLength(3));
    expect(window.location.search).toBe("");
  });
});
