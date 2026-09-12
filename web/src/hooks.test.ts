import { act, renderHook } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { useSelection } from "./hooks";
import type { Image } from "./types";

function image(id: number): Image {
  return {
    id,
    root: "out",
    path: `txt2img/0000${id}.png`,
    dir: "txt2img",
    name: `0000${id}.png`,
    size: 1024,
    mod_time: "2026-08-13T12:00:00Z",
    width: 512,
    height: 768,
    created_at: "2026-08-13T12:00:00Z",
    has_params: true,
    prompt: "1girl",
    negative: "",
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
  };
}

/** ids は選ばれている ID を並び順を決めて返す。 */
const ids = (set: Set<number>) => [...set].sort((a, b) => a - b);

describe("useSelection", () => {
  it("一覧から消えた画像を選択から外し、残っている画像はそのまま残す", () => {
    const images = [image(1), image(2), image(3)];
    const { result, rerender } = renderHook(
      ({ list, ready }: { list: Image[]; ready: boolean }) => useSelection(list, ready),
      { initialProps: { list: images, ready: true } },
    );

    act(() => result.current.toggle(0, false));
    act(() => result.current.toggle(2, false));
    expect(ids(result.current.ids)).toEqual([1, 3]);

    rerender({ list: [images[1], images[2]], ready: true });

    expect(ids(result.current.ids)).toEqual([3]);
  });

  it("読み込み中で一覧が空になっても選択を消さない", () => {
    const images = [image(1), image(2)];
    const { result, rerender } = renderHook(
      ({ list, ready }: { list: Image[]; ready: boolean }) => useSelection(list, ready),
      { initialProps: { list: images, ready: true } },
    );

    act(() => result.current.toggle(0, false));
    expect(ids(result.current.ids)).toEqual([1]);

    // 読み込み中は一覧が当てにならないため、空でも選択はそのまま。
    rerender({ list: [], ready: false });

    expect(ids(result.current.ids)).toEqual([1]);

    // 読み終われば一覧に合わせて見直す。
    rerender({ list: images, ready: true });

    expect(ids(result.current.ids)).toEqual([1]);
  });

  it("読み込みに失敗して一覧が空のままでも選択を消さない", () => {
    const images = [image(1), image(2)];
    const { result, rerender } = renderHook(
      ({ list, ready }: { list: Image[]; ready: boolean }) => useSelection(list, ready),
      { initialProps: { list: images, ready: true } },
    );

    act(() => result.current.toggle(1, false));
    expect(ids(result.current.ids)).toEqual([2]);

    // 失敗したときも、巻き添えで選択を失わない。
    rerender({ list: [], ready: false });

    expect(ids(result.current.ids)).toEqual([2]);
  });

  it("範囲選択の起点が一覧から消えたら、Shift でも押した 1 件だけを選ぶ", () => {
    const images = [image(1), image(2), image(3)];
    const { result, rerender } = renderHook(
      ({ list, ready }: { list: Image[]; ready: boolean }) => useSelection(list, ready),
      { initialProps: { list: images, ready: true } },
    );

    // 起点は 00001.png。
    act(() => result.current.toggle(0, false));
    rerender({ list: [images[1], images[2]], ready: true });
    expect(ids(result.current.ids)).toEqual([]);

    // 起点が消えているため、範囲は作らない。
    act(() => result.current.toggle(1, true));

    expect(ids(result.current.ids)).toEqual([3]);
  });

  it("一覧が組み替わっても、残っている起点からの範囲を選ぶ", () => {
    const images = [image(1), image(2), image(3), image(4)];
    const { result, rerender } = renderHook(
      ({ list, ready }: { list: Image[]; ready: boolean }) => useSelection(list, ready),
      { initialProps: { list: images, ready: true } },
    );

    // 起点は 00002.png。
    act(() => result.current.toggle(1, false));
    // 先頭が消えて、起点の位置がひとつ前へずれる。
    rerender({ list: [images[1], images[2], images[3]], ready: true });

    act(() => result.current.toggle(2, true));

    expect(ids(result.current.ids)).toEqual([2, 3, 4]);
  });
});
