import { filtersToSearch, type Filters } from "./filters";
import type { FacetSet, Image, SearchResult, Status, TagCount } from "./types";

async function getJSON<T>(path: string, signal?: AbortSignal): Promise<T> {
  const res = await fetch(path, { signal });
  if (!res.ok) {
    const body = await res.text().catch(() => "");
    throw new Error(`${res.status} ${res.statusText}: ${body}`);
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
