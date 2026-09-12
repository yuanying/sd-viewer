import { act, cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import App from "./App";
import type { FacetSet, Image, SearchResult, Status, TagCount } from "./types";

/** requests は画面が投げた API のパスを順に覚える。 */
let requests: string[] = [];

/** maxLimit はサーバが一度に返す件数の上限。 */
const maxLimit = 500;

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
  webui: true,
  trash: [],
};

/** live は一覧に出ている画像。ゴミ箱の出し入れで中身が動く。 */
let live: Image[] = [];

/** binned はゴミ箱の中の画像。 */
let binned: Image[] = [];

/** currentStatus はゴミ箱の件数まで写した、今の状態を返す。 */
function currentStatus(): Status {
  const counts = new Map<string, number>();
  for (const img of binned) {
    counts.set(img.root, (counts.get(img.root) ?? 0) + 1);
  }
  return {
    ...status,
    total: live.length,
    webui,
    trash: [...counts].map(([root, count]) => ({ root, count })),
  };
}

/** takeIDs は本文で指定された ID を取り出す。 */
function takeIDs(init?: RequestInit): number[] {
  return (JSON.parse(String(init?.body ?? "{}")) as { ids?: number[] }).ids ?? [];
}

/** webui は送信先の設定有無を切り替える。 */
let webui = true;

/** sent は画面が送った送信要求を覚える。 */
let sent: { url: string; body: unknown }[] = [];

/** facetResponse は /api/facets の応答。既定は facets。関数なら問い合わせのたびに作る。 */
let facetResponse: unknown = facets;

/** favRequests は画面が送った Fav の付け外しを覚える。 */
let favRequests: { url: string; ids: number[] }[] = [];

/** failFav が真なら、Fav の付け外しをすべて失敗させる。 */
let failFav = false;

/** failSearch が真なら、一覧の読み込みをすべて失敗させる。 */
let failSearch = false;

/** stubFetch は API の応答を差し替える。 */
function stubFetch() {
  vi.stubGlobal(
    "fetch",
    vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      requests.push(url);

      const respondJSON = (body: unknown) =>
        new Response(JSON.stringify(body), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });

      if (url === "/api/fav" || url === "/api/fav/remove") {
        const ids = takeIDs(init);
        favRequests.push({ url, ids });
        if (failFav) {
          return respondJSON({
            done: 0,
            failed: ids.map((id) => ({ id, reason: "Fav を変更できませんでした" })),
          });
        }
        const on = url === "/api/fav";
        const mark = (img: Image): Image =>
          ids.includes(img.id) ? { ...img, fav_at: on ? "2026-08-21T10:00:00Z" : undefined } : img;
        live = live.map(mark);
        binned = binned.map(mark);
        return respondJSON({ done: ids.length });
      }

      if (url === "/api/trash" && init?.method === "POST") {
        const ids = takeIDs(init);
        binned = [...binned, ...live.filter((img) => ids.includes(img.id))];
        live = live.filter((img) => !ids.includes(img.id));
        return respondJSON({ done: ids.length });
      }
      if (url === "/api/trash/restore") {
        const ids = takeIDs(init);
        live = [...live, ...binned.filter((img) => ids.includes(img.id))];
        binned = binned.filter((img) => !ids.includes(img.id));
        return respondJSON({ done: ids.length });
      }
      if (url === "/api/trash/purge") {
        const ids = takeIDs(init);
        binned = binned.filter((img) => !ids.includes(img.id));
        return respondJSON({ done: ids.length });
      }
      if (url === "/api/trash/empty") {
        const root = (JSON.parse(String(init?.body ?? "{}")) as { root?: string }).root ?? "";
        const done = binned.filter((img) => root === "" || img.root === root).length;
        binned = binned.filter((img) => root !== "" && img.root !== root);
        return respondJSON({ done });
      }
      if (url.startsWith("/api/trash")) {
        const params = new URLSearchParams(url.split("?")[1] ?? "");
        const root = params.get("root");
        const images = root ? binned.filter((img) => img.root === root) : binned;
        return respondJSON({ total: images.length, images } satisfies SearchResult);
      }

      if (url === "/api/send") {
        sent.push({ url, body: JSON.parse(String(init?.body ?? "{}")) });
        return new Response(JSON.stringify({ target: "txt2img" }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      }

      const respond = (body: unknown) =>
        new Response(JSON.stringify(body), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });

      if (url.startsWith("/api/images/")) {
        const id = Number(url.slice("/api/images/".length));
        const found = live.find((img) => img.id === id);
        return respond({
          ...image(id),
          ...found,
          positive_tags: ["1girl", "smile"],
          raw: "raw text",
        });
      }
      if (url.startsWith("/api/images")) {
        if (failSearch) {
          return new Response("読み込めません", { status: 500, statusText: "Server Error" });
        }
        const params = new URLSearchParams(url.split("?")[1] ?? "");
        const all = params.get("fav") === "1" ? live.filter((img) => img.fav_at) : live;
        const models = params.getAll("model");
        const images = models.length === 0 ? all : all.filter((i) => models.includes(i.model));
        // サーバと同じく、上限を超える件数を求められても maxLimit 件で打ち切る。
        const offset = Number(params.get("offset") ?? 0);
        const limit = Math.min(Number(params.get("limit") ?? 100), maxLimit);
        return respond({
          total: images.length,
          images: images.slice(offset, offset + limit),
        } satisfies SearchResult);
      }
      if (url.startsWith("/api/facets")) {
        const params = new URLSearchParams(url.split("?")[1] ?? "");
        return respond(
          typeof facetResponse === "function" ? facetResponse(params) : facetResponse,
        );
      }
      if (url.startsWith("/api/tags")) {
        return respond([{ tag: "smile", count: 2 }] satisfies TagCount[]);
      }
      if (url.startsWith("/api/status")) {
        return respond(currentStatus());
      }
      return new Response("not found", { status: 404 });
    }),
  );
}

/** sources は画面が今張っている購読。監視からの通知を後から配るために覚える。 */
let sources = new Set<StubEventSource>();

/** EventSource は happy-dom にないため、購読直後に状態を配る実装で置き換える。 */
class StubEventSource {
  onmessage: ((event: MessageEvent) => void) | null = null;
  constructor() {
    sources.add(this);
    queueMicrotask(() => this.send());
  }
  /** send は今の状態を配る。監視で枚数が変わったことにするために使う。 */
  send() {
    this.onmessage?.({ data: JSON.stringify(currentStatus()) } as MessageEvent);
  }
  close() {
    sources.delete(this);
  }
}

/** notifyStatus は購読している画面へ今の状態を配る。 */
function notifyStatus() {
  for (const source of sources) {
    source.send();
  }
}

beforeEach(() => {
  requests = [];
  sent = [];
  favRequests = [];
  failFav = false;
  failSearch = false;
  facetResponse = facets;
  webui = true;
  live = [image(1), image(2), image(3, { model: "modelB" })];
  binned = [];
  window.history.replaceState(null, "", "/");
  observers = new Set();
  sources = new Set();
  stubFetch();
  vi.stubGlobal("EventSource", StubEventSource);
  vi.stubGlobal("IntersectionObserver", ObserverStub);
});

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
});

/** observers は画面が今張っている IntersectionObserver。 */
let observers = new Set<ObserverStub>();

/** IntersectionObserver も happy-dom にないため差し替える。fire で下端に届いたことにする。 */
class ObserverStub {
  callback: (entries: { isIntersecting: boolean }[]) => void;
  constructor(callback: (entries: { isIntersecting: boolean }[]) => void) {
    this.callback = callback;
  }
  observe() {
    observers.add(this);
  }
  disconnect() {
    observers.delete(this);
  }
  fire() {
    this.callback([{ isIntersecting: true }]);
  }
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

  it("詳細から txt2img と img2img へ送れる", async () => {
    const user = userEvent.setup();
    render(<App />);
    await waitFor(() => expect(screen.getAllByRole("button", { name: /0000/ })).toHaveLength(3));
    await user.click(screen.getAllByRole("button", { name: /0000/ })[0]);
    await screen.findByText("00001.png");

    await user.click(screen.getByRole("button", { name: "txt2img へ送る" }));
    await waitFor(() => expect(sent).toHaveLength(1));
    expect(sent[0].body).toEqual({ id: 1, target: "txt2img" });

    await user.click(screen.getByRole("button", { name: "img2img へ送る" }));
    await waitFor(() => expect(sent).toHaveLength(2));
    expect(sent[1].body).toEqual({ id: 1, target: "img2img" });
  });

  it("送り先が設定されていなければ送信ボタンを出さない", async () => {
    webui = false;
    const user = userEvent.setup();
    render(<App />);
    await waitFor(() => expect(screen.getAllByRole("button", { name: /0000/ })).toHaveLength(3));
    await user.click(screen.getAllByRole("button", { name: /0000/ })[0]);
    await screen.findByText("00001.png");

    expect(screen.queryByRole("button", { name: "txt2img へ送る" })).toBeNull();
    expect(screen.queryByRole("button", { name: "img2img へ送る" })).toBeNull();
  });

  it("結果が 0 件でファセットが空配列でも落ちずに描画する", async () => {
    live = [];
    facetResponse = { models: [], loras: [], samplers: [], sizes: [], dirs: [], roots: [] };
    render(<App />);

    expect(await screen.findByText("条件に合う画像がありません。")).toBeTruthy();
    await waitFor(() => expect(requests.some((url) => url.startsWith("/api/facets"))).toBe(true));
    expect(screen.getByRole("button", { name: /Fav のみ/ })).toBeTruthy();
  });

  it("ファセットの項目が null でも落ちずに描画する", async () => {
    facetResponse = { models: null, loras: null, samplers: null, sizes: null, dirs: null, roots: null };
    render(<App />);

    await waitFor(() => expect(screen.getAllByRole("button", { name: /0000/ })).toHaveLength(3));
    await waitFor(() => expect(requests.some((url) => url.startsWith("/api/facets"))).toBe(true));
    // 応答を描き終えたあとも一覧が残っている。
    await new Promise((resolve) => setTimeout(resolve, 50));
    expect(screen.getAllByRole("button", { name: /0000/ })).toHaveLength(3);
  });

  it("絞り込み中の全件数と解除の操作はヘッダではなくサイドバーに出す", async () => {
    // ヘッダに現れると並びが押し出され、検索バーが折り返してしまう。
    window.history.replaceState(null, "", "/?model=modelB");
    render(<App />);
    await waitFor(() => expect(screen.getAllByRole("button", { name: /0000/ })).toHaveLength(1));

    const header = screen.getByRole("banner");
    expect(within(header).getByText("1 件")).toBeTruthy();
    expect(within(header).queryByText(/全 3 件/)).toBeNull();
    expect(within(header).queryByRole("button", { name: "条件をすべて解除" })).toBeNull();

    const sidebar = screen.getByRole("complementary");
    await waitFor(() => expect(within(sidebar).getByText(/全 3 件/)).toBeTruthy());
    expect(within(sidebar).getByRole("button", { name: "条件をすべて解除" })).toBeTruthy();
  });

  it("絞り込みが無ければ解除の操作を出さない", async () => {
    render(<App />);
    await waitFor(() => expect(screen.getAllByRole("button", { name: /0000/ })).toHaveLength(3));

    expect(screen.queryByRole("button", { name: "条件をすべて解除" })).toBeNull();
    expect(screen.queryByText(/全 3 件/)).toBeNull();
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

describe("ゴミ箱", () => {
  /** grid は一覧に出ているセルのボタンを返す。 */
  const grid = () => screen.getAllByRole("button", { name: /0000/ });

  /** boxes は選択用のチェックボックスを返す。 */
  const boxes = () => screen.getAllByRole("checkbox", { name: /を選択$/ });

  /** openTrash はゴミ箱ビューへ切り替える。 */
  async function openTrash(user: ReturnType<typeof userEvent.setup>) {
    await user.click(screen.getByRole("button", { name: /^ゴミ箱/ }));
    return screen.findByRole("region", { name: "ゴミ箱" });
  }

  it("選んだ画像をゴミ箱へ入れると一覧から消える", async () => {
    const user = userEvent.setup();
    render(<App />);
    await waitFor(() => expect(grid()).toHaveLength(3));

    await user.click(boxes()[0]);
    expect(screen.getByText("1 件選択中")).toBeTruthy();
    await user.click(screen.getByRole("button", { name: "ゴミ箱へ移動" }));

    await waitFor(() => expect(grid()).toHaveLength(2));
    expect(screen.queryByText("1 件選択中")).toBeNull();
    await waitFor(() =>
      expect(screen.getByRole("button", { name: /^ゴミ箱/ }).textContent).toContain("1"),
    );
  });

  it("Shift を押しながら選ぶと範囲をまとめて選べる", async () => {
    const user = userEvent.setup();
    render(<App />);
    await waitFor(() => expect(grid()).toHaveLength(3));

    await user.click(boxes()[0]);
    await user.keyboard("{Shift>}");
    await user.click(boxes()[2]);
    await user.keyboard("{/Shift}");

    expect(screen.getByText("3 件選択中")).toBeTruthy();
    await user.click(screen.getByRole("button", { name: "ゴミ箱へ移動" }));
    await waitFor(() => expect(screen.queryAllByRole("button", { name: /0000/ })).toHaveLength(0));
  });

  it("選択を解除できる", async () => {
    const user = userEvent.setup();
    render(<App />);
    await waitFor(() => expect(grid()).toHaveLength(3));

    await user.click(boxes()[0]);
    await user.click(screen.getByRole("button", { name: "選択を解除" }));

    expect(screen.queryByText("1 件選択中")).toBeNull();
  });

  it("詳細を開いた 1 枚をゴミ箱へ入れられる", async () => {
    const user = userEvent.setup();
    render(<App />);
    await waitFor(() => expect(grid()).toHaveLength(3));
    await user.click(grid()[0]);
    await screen.findByText("00001.png");

    await user.click(screen.getByRole("button", { name: "ゴミ箱へ移動" }));

    await waitFor(() => expect(screen.queryByText("00001.png")).toBeNull());
    await waitFor(() => expect(grid()).toHaveLength(2));
  });

  it("ゴミ箱の中身をルートごとに見せる", async () => {
    const user = userEvent.setup();
    render(<App />);
    await waitFor(() => expect(grid()).toHaveLength(3));
    await user.click(boxes()[0]);
    await user.click(screen.getByRole("button", { name: "ゴミ箱へ移動" }));
    await waitFor(() => expect(grid()).toHaveLength(2));

    const view = await openTrash(user);

    const section = await within(view).findByRole("group", { name: /out/ });
    expect(within(section).getByRole("button", { name: /00001\.png を元に戻す/ })).toBeTruthy();
    expect(within(section).getByRole("button", { name: "空にする" })).toBeTruthy();
  });

  it("ゴミ箱から元に戻すと一覧へ返ってくる", async () => {
    const user = userEvent.setup();
    render(<App />);
    await waitFor(() => expect(grid()).toHaveLength(3));
    await user.click(boxes()[0]);
    await user.click(screen.getByRole("button", { name: "ゴミ箱へ移動" }));
    await waitFor(() => expect(grid()).toHaveLength(2));
    const view = await openTrash(user);

    await user.click(await within(view).findByRole("button", { name: /00001\.png を元に戻す/ }));

    await waitFor(() => expect(within(view).queryByRole("group")).toBeNull());
    await user.click(screen.getByRole("button", { name: "一覧へ戻る" }));
    await waitFor(() => expect(grid()).toHaveLength(3));
  });

  it("完全に削除する前に確認する", async () => {
    const user = userEvent.setup();
    const confirm = vi.fn(() => false);
    vi.stubGlobal("confirm", confirm);
    render(<App />);
    await waitFor(() => expect(grid()).toHaveLength(3));
    await user.click(boxes()[0]);
    await user.click(screen.getByRole("button", { name: "ゴミ箱へ移動" }));
    await waitFor(() => expect(grid()).toHaveLength(2));
    const view = await openTrash(user);

    // 断ったときは消さない。
    await user.click(await within(view).findByRole("button", { name: /00001\.png を完全に削除/ }));
    expect(confirm).toHaveBeenCalled();
    expect(requests.some((url) => url === "/api/trash/purge")).toBe(false);

    // 承知したときだけ消す。
    confirm.mockReturnValue(true as never);
    await user.click(within(view).getByRole("button", { name: /00001\.png を完全に削除/ }));
    await waitFor(() => expect(within(view).queryByRole("group")).toBeNull());
  });

  it("ルートごとにゴミ箱を空にする", async () => {
    const user = userEvent.setup();
    vi.stubGlobal("confirm", vi.fn(() => true));
    render(<App />);
    await waitFor(() => expect(grid()).toHaveLength(3));
    await user.click(boxes()[0]);
    await user.click(boxes()[1]);
    await user.click(screen.getByRole("button", { name: "ゴミ箱へ移動" }));
    await waitFor(() => expect(grid()).toHaveLength(1));
    const view = await openTrash(user);

    const section = await within(view).findByRole("group", { name: /out/ });
    await user.click(within(section).getByRole("button", { name: "空にする" }));

    await waitFor(() => expect(within(view).queryByRole("group")).toBeNull());
    expect(within(view).getByText("ゴミ箱は空です。")).toBeTruthy();
  });

  it("ゴミ箱が空ならヘッダのボタンを出さない", async () => {
    render(<App />);
    await waitFor(() => expect(grid()).toHaveLength(3));

    expect(screen.queryByRole("button", { name: /^ゴミ箱/ })).toBeNull();
  });
});

describe("Fav", () => {
  /** grid は一覧に出ているセルのボタンを返す。 */
  const grid = () => screen.getAllByRole("button", { name: /0000/ });

  /** stars はセルごとの Fav の切り替えを返す。 */
  const stars = () => screen.getAllByRole("checkbox", { name: /を Fav$/ });

  /** boxes は選択用のチェックボックスを返す。 */
  const boxes = () => screen.getAllByRole("checkbox", { name: /を選択$/ });

  /** favOnly はヘッダの「Fav のみ表示」の星を返す。 */
  const favOnly = () => screen.getByRole("button", { name: "Fav のみ表示" });

  it("グリッドの星で Fav にすると Fav のみの表示に出てくる", async () => {
    const user = userEvent.setup();
    render(<App />);
    await waitFor(() => expect(grid()).toHaveLength(3));
    expect((stars()[0] as HTMLInputElement).checked).toBe(false);

    await user.click(stars()[0]);

    await waitFor(() => expect(favRequests).toEqual([{ url: "/api/fav", ids: [1] }]));
    await waitFor(() => expect((stars()[0] as HTMLInputElement).checked).toBe(true));

    // ヘッダの切り替えは文字を持たない星だけ。オフは ☆。
    expect(favOnly().textContent).toBe("☆");
    expect(favOnly().getAttribute("aria-pressed")).toBe("false");
    await user.click(favOnly());

    await waitFor(() => expect(grid()).toHaveLength(1));
    expect(window.location.search).toBe("?fav=1");
    expect(favOnly().getAttribute("aria-pressed")).toBe("true");
    expect(favOnly().textContent).toBe("★");

    // もう一度押すと Fav のみの表示が外れる。
    await user.click(favOnly());

    await waitFor(() => expect(grid()).toHaveLength(3));
    expect(window.location.search).toBe("");
    expect(favOnly().getAttribute("aria-pressed")).toBe("false");
  });

  it("Fav のみの表示で外すと一覧から消える", async () => {
    const user = userEvent.setup();
    live[0] = { ...live[0], fav_at: "2026-08-21T10:00:00Z" };
    window.history.replaceState(null, "", "/?fav=1");
    render(<App />);
    await waitFor(() => expect(grid()).toHaveLength(1));
    expect((stars()[0] as HTMLInputElement).checked).toBe(true);

    await user.click(stars()[0]);

    await waitFor(() => expect(favRequests).toEqual([{ url: "/api/fav/remove", ids: [1] }]));
    expect(await screen.findByText("条件に合う画像がありません。")).toBeTruthy();
  });

  it("詳細から Fav を付け外しできる", async () => {
    const user = userEvent.setup();
    render(<App />);
    await waitFor(() => expect(grid()).toHaveLength(3));
    await user.click(grid()[0]);
    await screen.findByText("00001.png");

    await user.click(screen.getByRole("button", { name: "Fav に追加" }));
    const remove = await screen.findByRole("button", { name: "Fav を外す" });
    expect(favRequests).toEqual([{ url: "/api/fav", ids: [1] }]);

    await user.click(remove);
    await screen.findByRole("button", { name: "Fav に追加" });
    expect(favRequests[1]).toEqual({ url: "/api/fav/remove", ids: [1] });
  });

  it("選んだ画像をまとめて Fav にできる", async () => {
    const user = userEvent.setup();
    render(<App />);
    await waitFor(() => expect(grid()).toHaveLength(3));

    await user.click(boxes()[0]);
    await user.click(boxes()[1]);
    await user.click(screen.getByRole("button", { name: "Fav に追加" }));

    await waitFor(() => expect(favRequests).toEqual([{ url: "/api/fav", ids: [1, 2] }]));
    await waitFor(() => expect(screen.queryByText("2 件選択中")).toBeNull());
    await waitFor(() =>
      expect(stars().map((s) => (s as HTMLInputElement).checked)).toEqual([true, true, false]),
    );
  });

  it("Fav のみの表示で外すと、取り直さずにその画像だけを除いて件数を減らす", async () => {
    const user = userEvent.setup();
    live = live.map((img) => ({ ...img, fav_at: "2026-08-21T10:00:00Z" }));
    window.history.replaceState(null, "", "/?fav=1");
    render(<App />);
    await waitFor(() => expect(grid()).toHaveLength(3));
    expect(within(screen.getByRole("banner")).getByText("3 件")).toBeTruthy();
    const fetched = requests.filter((url) => url.startsWith("/api/images?")).length;

    await user.click(stars()[1]);

    await waitFor(() => expect(favRequests).toEqual([{ url: "/api/fav/remove", ids: [2] }]));
    await waitFor(() => expect(grid().map((cell) => cell.title)).toEqual([live[0].path, live[2].path]));
    expect(within(screen.getByRole("banner")).getByText("2 件")).toBeTruthy();
    expect(requests.filter((url) => url.startsWith("/api/images?"))).toHaveLength(fetched);
  });

  it("条件をすべて解除すると Fav のみの表示も外れる", async () => {
    const user = userEvent.setup();
    live[0] = { ...live[0], fav_at: "2026-08-21T10:00:00Z" };
    window.history.replaceState(null, "", "/?fav=1");
    render(<App />);
    await waitFor(() => expect(grid()).toHaveLength(1));

    await user.click(screen.getByRole("button", { name: "条件をすべて解除" }));

    await waitFor(() => expect(grid()).toHaveLength(3));
    expect(window.location.search).toBe("");
    expect(favOnly().getAttribute("aria-pressed")).toBe("false");
  });
});

describe("Fav とサイドバーの件数", () => {
  /** grid は一覧に出ているセルのボタンを返す。 */
  const grid = () => screen.getAllByRole("button", { name: /0000/ });

  /** stars はセルごとの Fav の切り替えを返す。 */
  const stars = () => screen.getAllByRole("checkbox", { name: /を Fav$/ });

  /** modelCount はサイドバーに出ているモデルの件数を返す。 */
  const modelCount = (model: string) =>
    within(document.querySelector<HTMLElement>(".facets")!)
      .getByTitle(model)
      .querySelector(".count")?.textContent;

  /** facetRequests はファセットの読み込み要求。 */
  const facetRequests = () => requests.filter((url) => url.startsWith("/api/facets"));

  /** imageRequests は一覧の読み込み要求。 */
  const imageRequests = () => requests.filter((url) => url.startsWith("/api/images?"));

  beforeEach(() => {
    live = live.map((img) => ({ ...img, fav_at: "2026-08-21T10:00:00Z" }));
    // サーバと同じく、条件に合う画像からモデルの件数を数える。
    facetResponse = (params: URLSearchParams) => {
      const shown = params.get("fav") === "1" ? live.filter((img) => img.fav_at) : live;
      const counts = new Map<string, number>();
      for (const img of shown) {
        counts.set(img.model, (counts.get(img.model) ?? 0) + 1);
      }
      return {
        ...facets,
        models: [...counts].map(([value, count]) => ({ value, count })),
      } satisfies FacetSet;
    };
  });

  it("Fav のみの表示でグリッドの星から外すと、一覧は取り直さずにサイドバーの件数を更新する", async () => {
    const user = userEvent.setup();
    window.history.replaceState(null, "", "/?fav=1");
    render(<App />);
    await waitFor(() => expect(grid()).toHaveLength(3));
    await waitFor(() => expect(modelCount("modelA")).toBe("2"));
    const fetched = imageRequests().length;
    const facetsFetched = facetRequests().length;

    await user.click(stars()[0]);

    await waitFor(() => expect(favRequests).toEqual([{ url: "/api/fav/remove", ids: [1] }]));
    await waitFor(() => expect(modelCount("modelA")).toBe("1"));
    expect(modelCount("modelB")).toBe("1");
    expect(facetRequests().length).toBeGreaterThan(facetsFetched);
    expect(facetRequests().at(-1)).toContain("fav=1");
    expect(imageRequests()).toHaveLength(fetched);
  });

  it("Fav のみの表示で詳細から外しても、サイドバーの件数を更新する", async () => {
    const user = userEvent.setup();
    window.history.replaceState(null, "", "/?fav=1");
    render(<App />);
    await waitFor(() => expect(grid()).toHaveLength(3));
    await waitFor(() => expect(modelCount("modelA")).toBe("2"));
    await user.click(grid()[0]);
    await screen.findByText("00001.png");
    const fetched = imageRequests().length;

    await user.click(screen.getByRole("button", { name: "Fav を外す" }));

    await waitFor(() => expect(modelCount("modelA")).toBe("1"));
    expect(imageRequests()).toHaveLength(fetched);
  });

  it("Fav のみの表示でも、付け外しに失敗したらファセットを取り直さない", async () => {
    const user = userEvent.setup();
    window.history.replaceState(null, "", "/?fav=1");
    render(<App />);
    await waitFor(() => expect(grid()).toHaveLength(3));
    await waitFor(() => expect(modelCount("modelA")).toBe("2"));
    const facetsFetched = facetRequests().length;
    failFav = true;

    await user.click(stars()[0]);

    expect(await screen.findByText("Fav を変更できませんでした")).toBeTruthy();
    expect(grid()).toHaveLength(3);
    expect(modelCount("modelA")).toBe("2");
    expect(facetRequests()).toHaveLength(facetsFetched);
  });

  it("Fav のみの表示でなければ、Fav の付け外しでファセットを取り直さない", async () => {
    const user = userEvent.setup();
    render(<App />);
    await waitFor(() => expect(grid()).toHaveLength(3));
    await waitFor(() => expect(modelCount("modelA")).toBe("2"));
    const facetsFetched = facetRequests().length;

    await user.click(stars()[0]);

    await waitFor(() => expect((stars()[0] as HTMLInputElement).checked).toBe(false));
    expect(modelCount("modelA")).toBe("2");
    expect(facetRequests()).toHaveLength(facetsFetched);
  });
});

describe("大量の画像", () => {
  // 一覧が縮むと、ブラウザではページの高さが減ってスクロール位置が飛ぶ。
  // happy-dom ではスクロール位置を測れないため、一覧が縮まない・取り直さないことで確かめる。

  /** cells は一覧のセル。数が多いため役割ではなくクラスで引く。 */
  const cells = () => [...document.querySelectorAll<HTMLButtonElement>(".cell")];

  /** order は一覧に並んでいる画像のパス。 */
  const order = () => cells().map((cell) => cell.title);

  /** imageRequests は一覧の読み込み要求。 */
  const imageRequests = () => requests.filter((url) => url.startsWith("/api/images?"));

  /** loadAll は下端に届いたことにして、count 件になるまで続きを読み込ませる。 */
  async function loadAll(count: number) {
    await waitFor(() => expect(cells().length).toBeGreaterThan(0));
    while (cells().length < count) {
      const shown = cells().length;
      act(() => observers.forEach((observer) => observer.fire()));
      await waitFor(() => expect(cells().length).toBeGreaterThan(shown));
    }
  }

  beforeEach(() => {
    live = Array.from({ length: 650 }, (_, i) => image(i + 1));
  });

  it("500 件を超えて読み込んだあとで Fav を付け外ししても、取り直さず件数と並びを保つ", async () => {
    const user = userEvent.setup();
    render(<App />);
    await loadAll(650);
    const shown = order();
    const fetched = imageRequests().length;
    const star = () => screen.getByLabelText<HTMLInputElement>("0000600.png を Fav");

    await user.click(star());
    await waitFor(() => expect(favRequests).toEqual([{ url: "/api/fav", ids: [600] }]));
    await waitFor(() => expect(star().checked).toBe(true));

    await user.click(star());
    await waitFor(() => expect(favRequests[1]).toEqual({ url: "/api/fav/remove", ids: [600] }));
    await waitFor(() => expect(star().checked).toBe(false));

    expect(order()).toEqual(shown);
    expect(imageRequests()).toHaveLength(fetched);
  }, 30_000);

  it("取り直す件数が上限を超えるときは上限以下に分けて取り、読み込み済みの件数を保つ", async () => {
    const user = userEvent.setup();
    render(<App />);
    await loadAll(650);
    const fetched = imageRequests().length;

    // ゴミ箱へ入れると一覧を取り直す。
    await user.click(screen.getByLabelText("0000600.png を選択"));
    await user.click(screen.getByText("ゴミ箱へ移動", { selector: "button" }));

    await waitFor(() => expect(cells()).toHaveLength(649));
    expect(order()).not.toContain(image(600).path);
    const again = imageRequests().slice(fetched);
    expect(again.length).toBeGreaterThan(1);
    for (const url of again) {
      const limit = Number(new URLSearchParams(url.split("?")[1]).get("limit"));
      expect(limit).toBeLessThanOrEqual(maxLimit);
    }
  }, 30_000);
});

describe("一覧から消えた画像の選択", () => {
  /** grid は一覧に出ているセルのボタンを返す。 */
  const grid = () => screen.getAllByRole("button", { name: /0000/ });

  /** stars はセルごとの Fav の切り替えを返す。 */
  const stars = () => screen.getAllByRole("checkbox", { name: /を Fav$/ });

  /** boxes は選択用のチェックボックスを返す。 */
  const boxes = () => screen.getAllByRole("checkbox", { name: /を選択$/ });

  /** box は 1 枚の選択用チェックボックスを返す。 */
  const box = (name: string) => screen.getByLabelText<HTMLInputElement>(`${name} を選択`);

  it("Fav のみの表示で選択中の画像を外すと、選択からも外れて件数が減る", async () => {
    const user = userEvent.setup();
    live = live.map((img) => ({ ...img, fav_at: "2026-08-21T10:00:00Z" }));
    window.history.replaceState(null, "", "/?fav=1");
    render(<App />);
    await waitFor(() => expect(grid()).toHaveLength(3));

    await user.click(boxes()[0]);
    await user.click(boxes()[1]);
    expect(screen.getByText("2 件選択中")).toBeTruthy();

    await user.click(stars()[0]);

    await waitFor(() => expect(favRequests).toEqual([{ url: "/api/fav/remove", ids: [1] }]));
    await waitFor(() => expect(screen.getByText("1 件選択中")).toBeTruthy());
    // 一覧に残っている画像の選択はそのまま。
    expect(box("00002.png").checked).toBe(true);
  });

  it("絞り込みを変えて一覧から消えた画像は、選択から外れる", async () => {
    const user = userEvent.setup();
    render(<App />);
    await waitFor(() => expect(grid()).toHaveLength(3));

    await user.click(boxes()[0]);
    await user.click(boxes()[2]);
    expect(screen.getByText("2 件選択中")).toBeTruthy();

    await user.click(screen.getByRole("checkbox", { name: /modelB/ }));

    await waitFor(() => expect(grid()).toHaveLength(1));
    await waitFor(() => expect(screen.getByText("1 件選択中")).toBeTruthy());
    expect(box("00003.png").checked).toBe(true);
  });

  it("選択バーの操作は、一覧に残っている選択だけに効く", async () => {
    const user = userEvent.setup();
    render(<App />);
    await waitFor(() => expect(grid()).toHaveLength(3));
    await user.click(boxes()[0]);
    await user.click(boxes()[2]);
    await user.click(screen.getByRole("checkbox", { name: /modelB/ }));
    await waitFor(() => expect(screen.getByText("1 件選択中")).toBeTruthy());

    await user.click(screen.getByRole("button", { name: "ゴミ箱へ移動" }));

    // 一覧から消えていた 00001.png には効かない。
    await waitFor(() => expect(binned.map((img) => img.id)).toEqual([3]));
  });

  it("読み込みに失敗したときは選択を消さない", async () => {
    const user = userEvent.setup();
    render(<App />);
    await waitFor(() => expect(grid()).toHaveLength(3));
    await user.click(boxes()[0]);
    await user.click(boxes()[1]);
    expect(screen.getByText("2 件選択中")).toBeTruthy();

    failSearch = true;
    await user.click(screen.getByRole("checkbox", { name: /modelB/ }));

    expect(await screen.findByText(/読み込みに失敗しました/)).toBeTruthy();
    expect(screen.getByText("2 件選択中")).toBeTruthy();
  });

  it("消えた画像が起点でも、Shift で意図しない範囲を選ばない", async () => {
    const user = userEvent.setup();
    live = live.map((img) => ({ ...img, fav_at: "2026-08-21T10:00:00Z" }));
    window.history.replaceState(null, "", "/?fav=1");
    render(<App />);
    await waitFor(() => expect(grid()).toHaveLength(3));

    // 起点は 00001.png。Fav を外すと一覧から消え、選択からも外れる。
    await user.click(boxes()[0]);
    await user.click(stars()[0]);
    await waitFor(() => expect(grid()).toHaveLength(2));
    expect(screen.queryByText(/件選択中/)).toBeNull();

    await user.keyboard("{Shift>}");
    await user.click(box("00003.png"));
    await user.keyboard("{/Shift}");

    // 起点が消えているため、00002.png まで巻き込まない。
    expect(screen.getByText("1 件選択中")).toBeTruthy();
    expect(box("00002.png").checked).toBe(false);
  });
});

describe("ゴミ箱・監視とサイドバーの件数", () => {
  /** grid は一覧に出ているセルのボタンを返す。 */
  const grid = () => screen.getAllByRole("button", { name: /0000/ });

  /** boxes は選択用のチェックボックスを返す。 */
  const boxes = () => screen.getAllByRole("checkbox", { name: /を選択$/ });

  /** modelCount はサイドバーに出ているモデルの件数。 */
  const modelCount = (model: string) =>
    within(document.querySelector<HTMLElement>(".facets")!)
      .getByTitle(model)
      .querySelector(".count")?.textContent;

  /** facetRequests はファセットの読み込み要求。 */
  const facetRequests = () => requests.filter((url) => url.startsWith("/api/facets"));

  /** openTrash はゴミ箱ビューへ切り替える。 */
  async function openTrash(user: ReturnType<typeof userEvent.setup>) {
    await user.click(screen.getByRole("button", { name: /^ゴミ箱/ }));
    return screen.findByRole("region", { name: "ゴミ箱" });
  }

  beforeEach(() => {
    // サーバと同じく、一覧に出ている画像からモデルの件数を数える。
    facetResponse = () => {
      const counts = new Map<string, number>();
      for (const img of live) {
        counts.set(img.model, (counts.get(img.model) ?? 0) + 1);
      }
      return {
        ...facets,
        models: [...counts].map(([value, count]) => ({ value, count })),
      } satisfies FacetSet;
    };
  });

  it("ゴミ箱へ移すとサイドバーの件数が減る", async () => {
    const user = userEvent.setup();
    render(<App />);
    await waitFor(() => expect(grid()).toHaveLength(3));
    await waitFor(() => expect(modelCount("modelA")).toBe("2"));

    await user.click(boxes()[0]);
    await user.click(screen.getByRole("button", { name: "ゴミ箱へ移動" }));

    await waitFor(() => expect(grid()).toHaveLength(2));
    await waitFor(() => expect(modelCount("modelA")).toBe("1"));
  });

  it("ゴミ箱から戻すとサイドバーの件数も戻る", async () => {
    const user = userEvent.setup();
    render(<App />);
    await waitFor(() => expect(grid()).toHaveLength(3));
    await user.click(boxes()[0]);
    await user.click(screen.getByRole("button", { name: "ゴミ箱へ移動" }));
    await waitFor(() => expect(modelCount("modelA")).toBe("1"));

    const view = await openTrash(user);
    await user.click(await within(view).findByRole("button", { name: /00001\.png を元に戻す/ }));
    await waitFor(() => expect(binned).toHaveLength(0));
    await user.click(screen.getByRole("button", { name: "一覧へ戻る" }));

    await waitFor(() => expect(modelCount("modelA")).toBe("2"));
  });

  it("完全に削除するとサイドバーの候補を取り直す", async () => {
    const user = userEvent.setup();
    vi.stubGlobal("confirm", () => true);
    render(<App />);
    await waitFor(() => expect(grid()).toHaveLength(3));
    await user.click(boxes()[0]);
    await user.click(screen.getByRole("button", { name: "ゴミ箱へ移動" }));
    await waitFor(() => expect(modelCount("modelA")).toBe("1"));

    const view = await openTrash(user);
    const fetched = facetRequests().length;
    await user.click(await within(view).findByRole("button", { name: /00001\.png を完全に削除/ }));

    await waitFor(() => expect(binned).toHaveLength(0));
    await waitFor(() => expect(facetRequests().length).toBeGreaterThan(fetched));
  });

  it("ゴミ箱を空にするとサイドバーの候補を取り直す", async () => {
    const user = userEvent.setup();
    vi.stubGlobal("confirm", () => true);
    render(<App />);
    await waitFor(() => expect(grid()).toHaveLength(3));
    await user.click(boxes()[0]);
    await user.click(screen.getByRole("button", { name: "ゴミ箱へ移動" }));
    await waitFor(() => expect(modelCount("modelA")).toBe("1"));

    const view = await openTrash(user);
    const fetched = facetRequests().length;
    await user.click(await within(view).findByRole("button", { name: "空にする" }));

    await waitFor(() => expect(binned).toHaveLength(0));
    await waitFor(() => expect(facetRequests().length).toBeGreaterThan(fetched));
  });

  it("監視で枚数が変わって一覧を取り直すと、サイドバーの件数も追随する", async () => {
    render(<App />);
    await waitFor(() => expect(grid()).toHaveLength(3));
    await waitFor(() => expect(modelCount("modelB")).toBe("1"));

    // 監視が 1 枚見つけたことにして、状態を配る。
    live = [...live, image(4, { model: "modelB" })];
    await act(async () => {
      notifyStatus();
    });

    await waitFor(() => expect(grid()).toHaveLength(4));
    await waitFor(() => expect(modelCount("modelB")).toBe("2"));
  });
});

describe("詳細の前後送り", () => {
  /** grid は一覧に出ているセルのボタンを返す。 */
  const grid = () => screen.getAllByRole("button", { name: /0000/ });

  /** prev と next は詳細の ← → 。 */
  const prev = () => screen.getByRole("button", { name: "前の画像" });
  const next = () => screen.getByRole("button", { name: "次の画像" });

  /** openFavOnly は 3 枚すべてを Fav にした「Fav のみ」表示を開く。 */
  async function openFavOnly(user: ReturnType<typeof userEvent.setup>, name: string) {
    render(<App />);
    await waitFor(() => expect(grid()).toHaveLength(3));
    await user.click(grid()[1]);
    await screen.findByText(name);
  }

  beforeEach(() => {
    live = live.map((img) => ({ ...img, fav_at: "2026-08-21T10:00:00Z" }));
    window.history.replaceState(null, "", "/?fav=1");
  });

  it("一覧に居る画像では ← → が一覧の並びで動く", async () => {
    const user = userEvent.setup();
    await openFavOnly(user, "00002.png");

    await user.click(next());
    expect(await screen.findByText("00003.png")).toBeTruthy();

    await user.click(prev());
    expect(await screen.findByText("00002.png")).toBeTruthy();
  });

  it("端では ← → で動かない", async () => {
    const user = userEvent.setup();
    render(<App />);
    await waitFor(() => expect(grid()).toHaveLength(3));
    await user.click(grid()[0]);
    await screen.findByText("00001.png");

    await user.click(prev());
    await new Promise((resolve) => setTimeout(resolve, 20));
    expect(screen.getByText("00001.png")).toBeTruthy();

    await user.click(next());
    await user.click(next());
    await screen.findByText("00003.png");
    await user.click(next());
    await new Promise((resolve) => setTimeout(resolve, 20));
    expect(screen.getByText("00003.png")).toBeTruthy();
  });

  it("開いている画像が一覧から消えても、詳細は開いたままになる", async () => {
    const user = userEvent.setup();
    await openFavOnly(user, "00002.png");

    await user.click(screen.getByRole("button", { name: "Fav を外す" }));

    await waitFor(() => expect(grid()).toHaveLength(2));
    expect(screen.getByText("00002.png")).toBeTruthy();
  });

  it("一覧から消えた画像でも → で元の次の画像へ進む", async () => {
    const user = userEvent.setup();
    await openFavOnly(user, "00002.png");
    await user.click(screen.getByRole("button", { name: "Fav を外す" }));
    await waitFor(() => expect(grid()).toHaveLength(2));

    await user.click(next());

    // 先頭（00001.png）に飛ばず、消える直前の次へ進む。
    expect(await screen.findByText("00003.png")).toBeTruthy();
  });

  it("一覧から消えた画像でも ← で元の前の画像へ戻る", async () => {
    const user = userEvent.setup();
    await openFavOnly(user, "00002.png");
    await user.click(screen.getByRole("button", { name: "Fav を外す" }));
    await waitFor(() => expect(grid()).toHaveLength(2));

    await user.click(prev());

    expect(await screen.findByText("00001.png")).toBeTruthy();
  });

  it("末尾の画像を一覧から消したあとは、→ で動かない", async () => {
    const user = userEvent.setup();
    render(<App />);
    await waitFor(() => expect(grid()).toHaveLength(3));
    await user.click(grid()[2]);
    await screen.findByText("00003.png");

    await user.click(screen.getByRole("button", { name: "Fav を外す" }));
    await waitFor(() => expect(grid()).toHaveLength(2));

    await user.click(next());
    await new Promise((resolve) => setTimeout(resolve, 20));
    expect(screen.getByText("00003.png")).toBeTruthy();

    // ← は消える直前の前の画像へ戻れる。
    await user.click(prev());
    expect(await screen.findByText("00002.png")).toBeTruthy();
  });
});
