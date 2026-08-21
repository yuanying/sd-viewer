import { filtersToSearch, type Filters } from "./filters";
import type {
  FacetSet,
  Image,
  SearchResult,
  SendTarget,
  Status,
  TagCount,
  TrashResult,
} from "./types";

async function getJSON<T>(path: string, signal?: AbortSignal): Promise<T> {
  const res = await fetch(path, { signal });
  if (!res.ok) {
    const body = await res.text().catch(() => "");
    throw new Error(`${res.status} ${res.statusText}: ${body}`);
  }
  return (await res.json()) as T;
}

/** postJSON は指示を送り、応答を読む。失敗はサーバの理由つきで投げる。 */
async function postJSON<T>(path: string, body: unknown): Promise<T> {
  const res = await fetch(path, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  if (!res.ok) {
    // サーバは理由を JSON の error に入れて返す。
    const failed = (await res.json().catch(() => null)) as { error?: string } | null;
    throw new Error(failed?.error ?? `${res.status} ${res.statusText}`);
  }
  return (await res.json()) as T;
}

export function searchImages(
  filters: Filters,
  offset: number,
  limit: number,
  signal?: AbortSignal,
): Promise<SearchResult> {
  const params = new URLSearchParams(filtersToSearch(filters));
  params.set("offset", String(offset));
  params.set("limit", String(limit));
  return getJSON<SearchResult>(`/api/images?${params}`, signal);
}

export function fetchFacets(filters: Filters, signal?: AbortSignal): Promise<FacetSet> {
  return getJSON<FacetSet>(`/api/facets?${filtersToSearch(filters)}`, signal);
}

export function fetchImage(id: number, signal?: AbortSignal): Promise<Image> {
  return getJSON<Image>(`/api/images/${id}`, signal);
}

export function suggestTags(q: string, limit = 12, signal?: AbortSignal): Promise<TagCount[]> {
  const params = new URLSearchParams({ q, limit: String(limit) });
  return getJSON<TagCount[]>(`/api/tags?${params}`, signal);
}

export function fetchStatus(signal?: AbortSignal): Promise<Status> {
  return getJSON<Status>("/api/status", signal);
}

/** sendToWebUI は生成情報を WebUI の入力欄へ送り込む。 */
export async function sendToWebUI(id: number, target: SendTarget): Promise<void> {
  await postJSON<{ target: string }>("/api/send", { id, target });
}

/** fetchTrash はゴミ箱の中身を読み込む。 */
export function fetchTrash(offset: number, limit: number, signal?: AbortSignal): Promise<SearchResult> {
  const params = new URLSearchParams({ offset: String(offset), limit: String(limit) });
  return getJSON<SearchResult>(`/api/trash?${params}`, signal);
}

/** moveToTrash は画像をゴミ箱へ入れる。 */
export function moveToTrash(ids: number[]): Promise<TrashResult> {
  return postJSON<TrashResult>("/api/trash", { ids });
}

/** restoreFromTrash は画像を元の場所へ戻す。 */
export function restoreFromTrash(ids: number[]): Promise<TrashResult> {
  return postJSON<TrashResult>("/api/trash/restore", { ids });
}

/** purgeFromTrash は画像を完全に削除する。取り消せない。 */
export function purgeFromTrash(ids: number[]): Promise<TrashResult> {
  return postJSON<TrashResult>("/api/trash/purge", { ids });
}

/** emptyTrash は指定したルートのゴミ箱を空にする。取り消せない。 */
export function emptyTrash(root: string): Promise<TrashResult> {
  return postJSON<TrashResult>("/api/trash/empty", { root });
}

export function thumbUrl(id: number): string {
  return `/api/thumb/${id}`;
}

export function rawUrl(id: number): string {
  return `/api/raw/${id}`;
}

/** subscribeStatus は状態の変化を購読する。戻り値を呼ぶと購読をやめる。 */
export function subscribeStatus(onStatus: (status: Status) => void): () => void {
  const source = new EventSource("/api/events");
  source.onmessage = (event) => {
    try {
      onStatus(JSON.parse(event.data) as Status);
    } catch {
      // 壊れたイベントは読み飛ばす。
    }
  };
  return () => source.close();
}
