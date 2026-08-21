import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { fetchFacets, fetchStatus, fetchTrash, searchImages, subscribeStatus } from "./api";
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

export interface StatusFeed {
  status: Status | null;
  /** refresh は購読の次の通知を待たずに状態を取り直す。 */
  refresh: () => void;
}

/** useStatus はインデックスの状態を購読する。 */
export function useStatus(): StatusFeed {
  const [status, setStatus] = useState<Status | null>(null);
  useEffect(() => subscribeStatus(setStatus), []);

  const refresh = useCallback(() => {
    fetchStatus()
      .then(setStatus)
      .catch(() => {
        // 次の通知で追いつくため、取れなくても黙って諦める。
      });
  }, []);

  return { status, refresh };
}

export interface TrashList {
  images: Image[];
  total: number;
  loading: boolean;
  error: string | null;
  hasMore: boolean;
  loadMore: () => void;
  reload: () => void;
}

/** useTrash はゴミ箱の中身を読み込む。enabled が偽の間は何もしない。 */
export function useTrash(enabled: boolean): TrashList {
  const [images, setImages] = useState<Image[]>([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [reloadKey, setReloadKey] = useState(0);

  useEffect(() => {
    if (!enabled) {
      return;
    }
    const controller = new AbortController();
    setLoading(true);
    fetchTrash(0, pageSize, controller.signal)
      .then((res) => {
        setImages(res.images);
        setTotal(res.total);
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
  }, [enabled, reloadKey]);

  const loadMore = useCallback(() => {
    if (loading || images.length >= total) {
      return;
    }
    setLoading(true);
    fetchTrash(images.length, pageSize)
      .then((res) => {
        setImages((prev) => mergeImages(prev, res.images));
        setTotal(res.total);
      })
      .catch((err: unknown) => setError(errorMessage(err)))
      .finally(() => setLoading(false));
  }, [images.length, total, loading]);

  const reload = useCallback(() => setReloadKey((n) => n + 1), []);

  return { images, total, loading, error, hasMore: images.length < total, loadMore, reload };
}

export interface Selection {
  /** ids は選ばれている画像。 */
  ids: Set<number>;
  /** toggle は 1 件の選択を切り替える。shift のときは直前の起点からの範囲を選ぶ。 */
  toggle: (index: number, shiftKey: boolean) => void;
  clear: () => void;
}

/** useSelection はグリッドの複数選択を預かる。 */
export function useSelection(images: Image[]): Selection {
  const [ids, setIDs] = useState<Set<number>>(() => new Set());
  // 範囲選択の起点。まだ何も触っていなければ null。
  const anchor = useRef<number | null>(null);

  const toggle = useCallback(
    (index: number, shiftKey: boolean) => {
      setIDs((prev) => {
        const next = new Set(prev);
        const from = anchor.current;
        if (shiftKey && from !== null) {
          for (let i = Math.min(from, index); i <= Math.max(from, index); i++) {
            const image = images[i];
            if (image) {
              next.add(image.id);
            }
          }
          return next;
        }
        const image = images[index];
        anchor.current = index;
        if (!image) {
          return next;
        }
        if (next.has(image.id)) {
          next.delete(image.id);
        } else {
          next.add(image.id);
        }
        return next;
      });
    },
    [images],
  );

  const clear = useCallback(() => {
    anchor.current = null;
    setIDs(new Set());
  }, []);

  return { ids, toggle, clear };
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
