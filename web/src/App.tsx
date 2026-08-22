import { useCallback, useEffect, useState } from "react";
import { emptyTrash, moveToTrash, purgeFromTrash, restoreFromTrash } from "./api";
import { FacetPanel } from "./components/FacetPanel";
import { ImageDetail } from "./components/ImageDetail";
import { ImageGrid } from "./components/ImageGrid";
import { SearchBar } from "./components/SearchBar";
import { TrashView } from "./components/TrashView";
import { emptyFilters, hasAnyFilter } from "./filters";
import {
  errorMessage,
  useFacets,
  useFilters,
  useImages,
  useSelection,
  useStatus,
  useTrash,
} from "./hooks";
import type { TrashResult } from "./types";

/** View は画面が今どちらを見せているか。 */
type View = "images" | "trash";

export default function App() {
  const [filters, setFilters] = useFilters();
  const list = useImages(filters);
  const facets = useFacets(filters);
  const { status, refresh } = useStatus();
  const [selected, setSelected] = useState<number | null>(null);
  const [view, setView] = useState<View>("images");
  const [notice, setNotice] = useState<string | null>(null);
  const selection = useSelection(list.images);
  const bin = useTrash(view === "trash");

  // 監視によって枚数が変わったら一覧を取り直す。
  const total = status?.total;
  const reload = list.reload;
  useEffect(() => {
    if (total !== undefined && total !== list.total) {
      reload();
    }
    // 表示中の件数は毎回変わるため、監視側の枚数だけを見る。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [total]);

  const move = useCallback(
    (step: number) => {
      setSelected((current) => {
        if (current === null) {
          return current;
        }
        const at = list.images.findIndex((img) => img.id === current);
        const next = list.images[at + step];
        return next ? next.id : current;
      });
    },
    [list.images],
  );

  const trashCounts = status?.trash ?? [];
  const trashTotal = trashCounts.reduce((n, c) => n + c.count, 0);

  const clearSelection = selection.clear;
  const reloadBin = bin.reload;

  /** apply はゴミ箱の操作を投げ、終わったら一覧と件数を取り直す。 */
  const apply = useCallback(
    (run: () => Promise<TrashResult>) => {
      run()
        .then((res) => {
          setNotice(res.failed?.length ? res.failed[0].reason : null);
          clearSelection();
          reload();
          reloadBin();
          refresh();
        })
        .catch((err: unknown) => setNotice(errorMessage(err)));
    },
    [clearSelection, reload, reloadBin, refresh],
  );

  const trashSelected = () => apply(() => moveToTrash([...selection.ids]));

  const trashOne = (id: number) => {
    setSelected(null);
    apply(() => moveToTrash([id]));
  };

  const purge = (ids: number[]) => {
    if (!window.confirm(`${ids.length} 件を完全に削除します。元には戻せません。`)) {
      return;
    }
    apply(() => purgeFromTrash(ids));
  };

  const empty = (root: string) => {
    if (!window.confirm(`${root} のゴミ箱を空にします。元には戻せません。`)) {
      return;
    }
    apply(() => emptyTrash(root));
  };

  const scanning = status?.scan.scanning ?? false;
  const showingTrash = view === "trash";

  return (
    <div className={showingTrash ? "app app-trash" : "app"}>
      <header className="header">
        <div className="title">
          <h1>sd-viewer</h1>
          {showingTrash ? (
            <>
              <span className="summary">ゴミ箱 {trashTotal.toLocaleString()} 件</span>
              <button type="button" className="link" onClick={() => setView("images")}>
                一覧へ戻る
              </button>
            </>
          ) : (
            <>
              <span className="summary">
                {list.total.toLocaleString()} 件
                {hasAnyFilter(filters) && status && ` / 全 ${status.total.toLocaleString()} 件`}
              </span>
              {scanning && (
                <span className="scanning">
                  スキャン中… {status?.scan.indexed.toLocaleString()} 件
                </span>
              )}
              {hasAnyFilter(filters) && (
                <button type="button" className="link" onClick={() => setFilters(emptyFilters())}>
                  条件をすべて解除
                </button>
              )}
              {trashTotal > 0 && (
                <button type="button" className="action" onClick={() => setView("trash")}>
                  ゴミ箱<span className="count">{trashTotal.toLocaleString()}</span>
                </button>
              )}
            </>
          )}
        </div>
        {!showingTrash && <SearchBar filters={filters} onChange={setFilters} />}
      </header>

      {!showingTrash && <FacetPanel facets={facets} filters={filters} onChange={setFilters} />}

      <main className="main">
        {notice && <p className="error">{notice}</p>}

        {showingTrash ? (
          <TrashView
            images={bin.images}
            counts={trashCounts}
            loading={bin.loading}
            error={bin.error}
            hasMore={bin.hasMore}
            onLoadMore={bin.loadMore}
            onRestore={(ids) => apply(() => restoreFromTrash(ids))}
            onPurge={purge}
            onEmpty={empty}
          />
        ) : (
          <>
            {list.error && <p className="error">読み込みに失敗しました: {list.error}</p>}
            {selection.ids.size > 0 && (
              <div className="selection-bar">
                <span>{selection.ids.size.toLocaleString()} 件選択中</span>
                <button type="button" className="action danger" onClick={trashSelected}>
                  ゴミ箱へ移動
                </button>
                <button type="button" className="link" onClick={selection.clear}>
                  選択を解除
                </button>
              </div>
            )}
            <ImageGrid
              images={list.images}
              loading={list.loading}
              loadingMore={list.loadingMore}
              hasMore={list.hasMore}
              onLoadMore={list.loadMore}
              onSelect={(image) => setSelected(image.id)}
              selected={selection.ids}
              onToggle={selection.toggle}
            />
          </>
        )}
      </main>

      {selected !== null && (
        <ImageDetail
          id={selected}
          filters={filters}
          onChange={setFilters}
          onClose={() => setSelected(null)}
          onPrev={() => move(-1)}
          onNext={() => move(1)}
          canSend={status?.webui ?? false}
          onTrash={trashOne}
        />
      )}
    </div>
  );
}
