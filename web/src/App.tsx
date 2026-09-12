import { useCallback, useEffect, useState } from "react";
import { emptyTrash, moveToTrash, purgeFromTrash, restoreFromTrash, setFav } from "./api";
import { FacetPanel } from "./components/FacetPanel";
import { ImageDetail } from "./components/ImageDetail";
import { ImageGrid } from "./components/ImageGrid";
import { SearchBar } from "./components/SearchBar";
import { TrashView } from "./components/TrashView";
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
  const { facets, refresh: refreshFacets } = useFacets(filters);
  const { status, refresh } = useStatus();
  const [selected, setSelected] = useState<number | null>(null);
  const [view, setView] = useState<View>("images");
  const [notice, setNotice] = useState<string | null>(null);
  // 一覧が読み込み中・読み込みに失敗している間は、一覧を当てに選択を見直さない。
  const selection = useSelection(list.images, !list.loading && list.error === null);
  const bin = useTrash(view === "trash");

  // 監視によって枚数が変わったら一覧と絞り込み候補を取り直す。
  const total = status?.total;
  const reload = list.reload;
  useEffect(() => {
    if (total !== undefined && total !== list.total) {
      reload();
      refreshFacets();
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

  /**
   * apply はゴミ箱の操作を投げ、終わったら一覧と件数、絞り込み候補を取り直す。
   * ゴミ箱に出し入れした画像は一覧に出る範囲から必ず増減するため、候補は条件を問わず取り直す。
   */
  const apply = useCallback(
    (run: () => Promise<TrashResult>) => {
      run()
        .then((res) => {
          setNotice(res.failed?.length ? res.failed[0].reason : null);
          clearSelection();
          reload();
          reloadBin();
          refresh();
          refreshFacets();
        })
        .catch((err: unknown) => setNotice(errorMessage(err)));
    },
    [clearSelection, reload, reloadBin, refresh, refreshFacets],
  );

  /**
   * favorite は Fav を付け外しし、できた分だけを手元の一覧へ写す。すべてできたかを返す。
   * 一覧は取り直さない。取り直すとスクロール位置が動いてしまうため。
   * Fav のみの表示では条件に合う画像が変わるため、サイドバーの候補だけを取り直して件数を合わせる。
   * それ以外の表示では Fav は条件に入らず件数も変わらないため、取り直さない。
   */
  const markFav = list.markFav;
  const favOnly = filters.fav;
  const favorite = useCallback(
    (ids: number[], fav: boolean) =>
      setFav(ids, fav)
        .then((res) => {
          setNotice(res.failed?.length ? res.failed[0].reason : null);
          const failed = new Set(res.failed?.map((f) => f.id));
          const done = ids.filter((id) => !failed.has(id));
          markFav(done, fav ? new Date().toISOString() : undefined);
          if (favOnly && done.length > 0) {
            refreshFacets();
          }
          return !res.failed?.length;
        })
        .catch((err: unknown) => {
          setNotice(errorMessage(err));
          return false;
        }),
    [markFav, favOnly, refreshFacets],
  );

  const favSelected = () => {
    void favorite([...selection.ids], true).then((ok) => ok && clearSelection());
  };

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
              {/* 絞り込み中の全件数と解除の操作はサイドバーに置き、ヘッダの並びを動かさない。 */}
              <span className="summary">{list.total.toLocaleString()} 件</span>
              <button
                type="button"
                className={filters.fav ? "fav-toggle on" : "fav-toggle"}
                aria-pressed={filters.fav}
                aria-label="Fav のみ表示"
                title="Fav のみ表示"
                onClick={() => setFilters({ ...filters, fav: !filters.fav })}
              >
                {filters.fav ? "★" : "☆"}
              </button>
              {scanning && (
                <span className="scanning">
                  スキャン中… {status?.scan.indexed.toLocaleString()} 件
                </span>
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

      {!showingTrash && (
        <FacetPanel facets={facets} filters={filters} total={status?.total} onChange={setFilters} />
      )}

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
                <button type="button" className="action" onClick={favSelected}>
                  Fav に追加
                </button>
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
              onFav={(image, fav) => void favorite([image.id], fav)}
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
          onFav={(id, fav) => favorite([id], fav)}
        />
      )}
    </div>
  );
}
