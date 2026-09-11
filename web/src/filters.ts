import type { FacetKey, Filters, SortOrder } from "./types";

export type { Filters };

/** 複数の値を選べる項目。URL でも同じ名前を並べて表す。 */
export const facetKeys: FacetKey[] = [
  "model",
  "lora",
  "sampler",
  "size",
  "dir",
  "root",
  "tag",
  "exclude_tag",
];

const sortOrders: SortOrder[] = ["newest", "oldest", "name"];

export function emptyFilters(): Filters {
  return {
    q: "",
    model: [],
    lora: [],
    sampler: [],
    size: [],
    dir: [],
    root: [],
    tag: [],
    exclude_tag: [],
    from: "",
    to: "",
    sort: "newest",
    fav: false,
  };
}

/** filtersToSearch は検索条件をクエリ文字列へ変換する。既定値は省く。 */
export function filtersToSearch(filters: Filters): string {
  const params = new URLSearchParams();

  if (filters.q.trim() !== "") {
    params.set("q", filters.q.trim());
  }
  for (const key of facetKeys) {
    for (const value of filters[key]) {
      params.append(key, value);
    }
  }
  for (const key of ["from", "to"] as const) {
    if (filters[key] !== "") {
      params.set(key, filters[key]);
    }
  }
  if (filters.sort !== "newest") {
    params.set("sort", filters.sort);
  }
  if (filters.fav) {
    params.set("fav", "1");
  }
  return params.toString();
}

/** searchToFilters はクエリ文字列を検索条件へ戻す。知らない値は既定に倒す。 */
export function searchToFilters(search: string): Filters {
  const params = new URLSearchParams(search);
  const filters = emptyFilters();

  filters.q = params.get("q") ?? "";
  for (const key of facetKeys) {
    filters[key] = params.getAll(key).filter((v) => v !== "");
  }
  filters.from = params.get("from") ?? "";
  filters.to = params.get("to") ?? "";

  const sort = params.get("sort") as SortOrder | null;
  if (sort && sortOrders.includes(sort)) {
    filters.sort = sort;
  }
  // サーバと同じく、真と読めるものだけを Fav のみとする。
  filters.fav = ["1", "true"].includes(params.get("fav") ?? "");
  return filters;
}

/** toggleFacet は選択を切り替えた新しい条件を返す。 */
export function toggleFacet(filters: Filters, key: FacetKey, value: string): Filters {
  const current = filters[key];
  const next = current.includes(value)
    ? current.filter((v) => v !== value)
    : [...current, value];
  return { ...filters, [key]: next };
}

/** hasAnyFilter は絞り込みが 1 つでも掛かっているかを返す。 */
export function hasAnyFilter(filters: Filters): boolean {
  if (filters.q.trim() !== "" || filters.from !== "" || filters.to !== "" || filters.fav) {
    return true;
  }
  return facetKeys.some((key) => filters[key].length > 0);
}

/** 表示用のラベル。 */
export const facetLabels: Record<FacetKey, string> = {
  model: "モデル",
  lora: "LoRA",
  sampler: "サンプラー",
  size: "解像度",
  dir: "フォルダ",
  root: "ルート",
  tag: "タグ",
  exclude_tag: "除外タグ",
};
