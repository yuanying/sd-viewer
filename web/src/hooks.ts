import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { fetchFacets, searchImages, subscribeStatus } from "./api";
import { emptyFilters, filtersToSearch, searchToFilters, type Filters } from "./filters";
import type { FacetSet, Image, Status } from "./types";

/** 一度に読み込む件数。 */
export const pageSize = 100;

/** useFilters は検索条件をアドレスバーと同期させる。 */
export function useFilters(): [Filters, (next: Filters) => void] {
  const [filters, setFilters] = useState<Filters>(() =>
    searchToFilters(window.location.search),
  );

  useEffect(() => {
    const onPop = () => setFilters(searchToFilters(window.location.search));
    window.addEventListener("popstate", onPop);
    return () => window.removeEventListener("popstate", onPop);
  }, []);

  const update = useCallback((next: Filters) => {
    const search = filtersToSearch(next);
    const url = search === "" ? window.location.pathname : `?${search}`;
    window.history.pushState(null, "", url);
    setFilters(next);
  }, []);

  return [filters, update];
}

export interface ImageList {
  images: Image[];
  total: number;
  loading: boolean;
  loadingMore: boolean;
  error: string | null;
  hasMore: boolean;
  loadMore: () => void;
  reload: () => void;
}

/** useImages は条件に合う画像を読み込み、続きを継ぎ足せるようにする。 */
export function useImages(filters: Filters): ImageList {
  const [images, setImages] = useState<Image[]>([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(true);
  const [loadingMore, setLoadingMore] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [reloadKey, setReloadKey] = useState(0);

  const filterKey = useMemo(() => filtersToSearch(filters), [filters]);
  const previousKey = useRef(filterKey);
  const loaded = useRef(0);

  useEffect(() => {
    // 条件が変わったときは先頭から、再読み込みのときは表示中の件数を保って取り直す。
    const sameQuery = previousKey.current === filterKey;
    previousKey.current = filterKey;
    const want = sameQuery ? Math.max(pageSize, loaded.current) : pageSize;

    const controller = new AbortController();
    setLoading(true);
    searchImages(filters, 0, want, controller.signal)
      .then((res) => {
        setImages(res.images);
        setTotal(res.total);
        loaded.current = res.images.length;
        setError(null);
      })
      .catch((err: unknown) => {
        if (!controller.signal.aborted) {
          setError(errorMessage(err));
        }
      })
      .finally(() => {
        if (!controller.signal.aborted) {
          setLoading(false);
        }
      });
    return () => controller.abort();
    // filterKey が変わったときだけ読み直す。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [filterKey, reloadKey]);

  const hasMore = images.length < total;

  const loadMore = useCallback(() => {
    if (loading || loadingMore || images.length >= total) {
      return;
    }
    setLoadingMore(true);
    searchImages(filters, images.length, pageSize)
      .then((res) => {
        setImages((prev) => mergeImages(prev, res.images));
        setTotal(res.total);
        loaded.current += res.images.length;
      })
      .catch((err: unknown) => setError(errorMessage(err)))
      .finally(() => setLoadingMore(false));
  }, [filters, images.length, total, loading, loadingMore]);

  const reload = useCallback(() => setReloadKey((n) => n + 1), []);

  return { images, total, loading, loadingMore, error, hasMore, loadMore, reload };
}

/** mergeImages は続きを読み込んだ際の重複を取り除く。 */
function mergeImages(prev: Image[], next: Image[]): Image[] {
  const seen = new Set(prev.map((img) => img.id));
  return [...prev, ...next.filter((img) => !seen.has(img.id))];
}

/** useFacets は現在の条件に対する絞り込み候補を読み込む。 */
export function useFacets(filters: Filters): FacetSet | null {
  const [facets, setFacets] = useState<FacetSet | null>(null);
  const filterKey = useMemo(() => filtersToSearch(filters), [filters]);

  useEffect(() => {
    const controller = new AbortController();
    fetchFacets(filters, controller.signal)
      .then(setFacets)
      .catch(() => {
        // 候補が取れなくても一覧は使えるため、黙って諦める。
      });
    return () => controller.abort();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [filterKey]);

  return facets;
}

/** useStatus はインデックスの状態を購読する。 */
export function useStatus(): Status | null {
  const [status, setStatus] = useState<Status | null>(null);
  useEffect(() => subscribeStatus(setStatus), []);
  return status;
}

/** useDebounced は入力が落ち着くまで値の反映を遅らせる。 */
export function useDebounced<T>(value: T, delay: number): T {
  const [debounced, setDebounced] = useState(value);
  useEffect(() => {
    const timer = window.setTimeout(() => setDebounced(value), delay);
    return () => window.clearTimeout(timer);
  }, [value, delay]);
  return debounced;
}

export function errorMessage(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

export const initialFilters = emptyFilters();
