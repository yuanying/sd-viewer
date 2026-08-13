import { useCallback, useEffect, useState } from "react";
import { FacetPanel } from "./components/FacetPanel";
import { ImageDetail } from "./components/ImageDetail";
import { ImageGrid } from "./components/ImageGrid";
import { SearchBar } from "./components/SearchBar";
import { emptyFilters, hasAnyFilter } from "./filters";
import { useFacets, useFilters, useImages, useStatus } from "./hooks";

export default function App() {
  const [filters, setFilters] = useFilters();
  const list = useImages(filters);
  const facets = useFacets(filters);
  const status = useStatus();
  const [selected, setSelected] = useState<number | null>(null);

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

  const scanning = status?.scan.scanning ?? false;

  return (
    <div className="app">
      <header className="header">
        <div className="title">
          <h1>sd-viewer</h1>
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
        </div>
        <SearchBar filters={filters} onChange={setFilters} />
      </header>

      <FacetPanel facets={facets} filters={filters} onChange={setFilters} />

      <main className="main">
        {list.error && <p className="error">読み込みに失敗しました: {list.error}</p>}
        <ImageGrid
          images={list.images}
          loading={list.loading}
          loadingMore={list.loadingMore}
          hasMore={list.hasMore}
          onLoadMore={list.loadMore}
          onSelect={(image) => setSelected(image.id)}
        />
      </main>

      {selected !== null && (
        <ImageDetail
          id={selected}
          filters={filters}
          onChange={setFilters}
          onClose={() => setSelected(null)}
          onPrev={() => move(-1)}
          onNext={() => move(1)}
        />
      )}
    </div>
  );
}
