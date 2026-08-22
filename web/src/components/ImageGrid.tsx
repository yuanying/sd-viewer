import { useEffect, useRef } from "react";
import { thumbUrl } from "../api";
import type { Image } from "../types";

interface Props {
  images: Image[];
  loading: boolean;
  loadingMore: boolean;
  hasMore: boolean;
  onLoadMore: () => void;
  onSelect: (image: Image) => void;
  /** 選ばれている画像の ID。 */
  selected: Set<number>;
  /** onToggle は選択の切り替えを伝える。shiftKey なら範囲選択。 */
  onToggle: (index: number, shiftKey: boolean) => void;
}

/** ImageGrid はサムネイルを並べ、下端に近づいたら続きを読み込む。 */
export function ImageGrid({
  images,
  loading,
  loadingMore,
  hasMore,
  onLoadMore,
  onSelect,
  selected,
  onToggle,
}: Props) {
  const sentinel = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const target = sentinel.current;
    if (!target || !hasMore) {
      return;
    }
    const observer = new IntersectionObserver(
      (entries) => {
        if (entries.some((e) => e.isIntersecting)) {
          onLoadMore();
        }
      },
      { rootMargin: "600px" },
    );
    observer.observe(target);
    return () => observer.disconnect();
  }, [hasMore, onLoadMore]);

  if (loading && images.length === 0) {
    return <p className="notice">読み込み中…</p>;
  }
  if (images.length === 0) {
    return <p className="notice">条件に合う画像がありません。</p>;
  }

  return (
    <>
      <div className="grid">
        {images.map((image, index) => (
          <div
            key={image.id}
            className={selected.has(image.id) ? "cell-slot selected" : "cell-slot"}
          >
            <label className="cell-check" title="選択する">
              <input
                type="checkbox"
                checked={selected.has(image.id)}
                aria-label={`${image.name} を選択`}
                // クリックの Shift はイベントからしか取れない。
                onClick={(e) => onToggle(index, e.shiftKey)}
                onChange={() => {}}
              />
            </label>
            <button
              type="button"
              className="cell"
              onClick={() => onSelect(image)}
              title={image.path}
            >
              <img
                src={thumbUrl(image.id)}
                alt={image.name}
                loading="lazy"
                decoding="async"
                style={{ aspectRatio: `${image.width || 1} / ${image.height || 1}` }}
              />
              <span className="cell-caption">{image.model || image.name}</span>
            </button>
          </div>
        ))}
      </div>
      <div ref={sentinel} className="sentinel">
        {loadingMore && "読み込み中…"}
      </div>
    </>
  );
}
