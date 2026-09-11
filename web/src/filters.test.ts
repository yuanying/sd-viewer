import { describe, expect, it } from "vitest";
import {
  emptyFilters,
  filtersToSearch,
  hasAnyFilter,
  searchToFilters,
  toggleFacet,
  type Filters,
} from "./filters";

describe("filtersToSearch", () => {
  const tests: { name: string; filters: Partial<Filters>; want: string }[] = [
    {
      name: "条件がなければ空になる",
      filters: {},
      want: "",
    },
    {
      name: "検索語を渡す",
      filters: { q: "1girl smile" },
      want: "q=1girl+smile",
    },
    {
      name: "複数選択した値は同じ名前で並べる",
      filters: { model: ["modelA", "modelB"] },
      want: "model=modelA&model=modelB",
    },
    {
      name: "既定の並び順は省く",
      filters: { sort: "newest" },
      want: "",
    },
    {
      name: "既定でない並び順は残す",
      filters: { sort: "oldest" },
      want: "sort=oldest",
    },
    {
      name: "日付の範囲を渡す",
      filters: { from: "2026-08-01", to: "2026-08-14" },
      want: "from=2026-08-01&to=2026-08-14",
    },
    {
      name: "空文字は条件にしない",
      filters: { q: "  ", from: "" },
      want: "",
    },
    {
      name: "Fav のみを渡す",
      filters: { fav: true, root: ["out"] },
      want: "root=out&fav=1",
    },
    {
      name: "Fav に絞らないときは省く",
      filters: { fav: false },
      want: "",
    },
  ];

  for (const tt of tests) {
    it(tt.name, () => {
      expect(filtersToSearch({ ...emptyFilters(), ...tt.filters })).toBe(tt.want);
    });
  }
});

describe("searchToFilters", () => {
  const tests: { name: string; search: string; want: Partial<Filters> }[] = [
    {
      name: "空の文字列は既定の条件になる",
      search: "",
      want: {},
    },
    {
      name: "検索語を読み取る",
      search: "?q=1girl",
      want: { q: "1girl" },
    },
    {
      name: "同じ名前の値をまとめる",
      search: "?model=modelA&model=modelB",
      want: { model: ["modelA", "modelB"] },
    },
    {
      name: "並び順を読み取る",
      search: "?sort=name",
      want: { sort: "name" },
    },
    {
      name: "知らない並び順は既定に戻す",
      search: "?sort=unknown",
      want: {},
    },
    {
      name: "知らない名前は無視する",
      search: "?unknown=1&q=x",
      want: { q: "x" },
    },
    {
      name: "Fav のみを読み取る",
      search: "?fav=1",
      want: { fav: true },
    },
    {
      name: "true も Fav のみとして読む",
      search: "?fav=true",
      want: { fav: true },
    },
    {
      name: "解釈できない Fav の値は指定なしに倒す",
      search: "?fav=0",
      want: {},
    },
  ];

  for (const tt of tests) {
    it(tt.name, () => {
      expect(searchToFilters(tt.search)).toEqual({ ...emptyFilters(), ...tt.want });
    });
  }

  it("組み立てた文字列を読み戻せる", () => {
    const filters: Filters = {
      ...emptyFilters(),
      q: "1girl -angry",
      model: ["modelA"],
      lora: ["style_v3", "char_a"],
      tag: ["smile"],
      exclude_tag: ["watermark"],
      from: "2026-08-01",
      sort: "oldest",
      fav: true,
    };
    expect(searchToFilters("?" + filtersToSearch(filters))).toEqual(filters);
  });
});

describe("toggleFacet", () => {
  const tests: {
    name: string;
    before: string[];
    value: string;
    want: string[];
  }[] = [
    { name: "選ばれていない値を加える", before: [], value: "a", want: ["a"] },
    { name: "選ばれている値を外す", before: ["a", "b"], value: "a", want: ["b"] },
    { name: "他の値には触れない", before: ["a"], value: "b", want: ["a", "b"] },
  ];

  for (const tt of tests) {
    it(tt.name, () => {
      const filters = { ...emptyFilters(), model: tt.before };
      expect(toggleFacet(filters, "model", tt.value).model).toEqual(tt.want);
    });
  }
});

describe("hasAnyFilter", () => {
  const tests: { name: string; filters: Partial<Filters>; want: boolean }[] = [
    { name: "何も指定していなければ false", filters: {}, want: false },
    { name: "検索語があれば true", filters: { q: "x" }, want: true },
    { name: "ファセットを選んでいれば true", filters: { lora: ["a"] }, want: true },
    { name: "日付を指定していれば true", filters: { from: "2026-08-01" }, want: true },
    { name: "Fav のみに絞っていれば true", filters: { fav: true }, want: true },
    { name: "並び順だけの変更は条件とみなさない", filters: { sort: "oldest" }, want: false },
  ];

  for (const tt of tests) {
    it(tt.name, () => {
      expect(hasAnyFilter({ ...emptyFilters(), ...tt.filters })).toBe(tt.want);
    });
  }
});
