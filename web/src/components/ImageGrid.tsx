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
  /** onFav は画像を Fav にするか外すかを伝える。 */
  onFav: (image: Image, fav: boolean) => void;
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
  onFav,
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
            <label
              className={image.fav_at ? "cell-fav on" : "cell-fav"}
              title={image.fav_at ? "Fav から外す" : "Fav に追加"}
            >
              <input
                type="checkbox"
                checked={Boolean(image.fav_at)}
                aria-label={`${image.name} を Fav`}
                onChange={() => onFav(image, !image.fav_at)}
              />
              <span aria-hidden="true">★</span>
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
