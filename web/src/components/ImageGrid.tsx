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
}

/** ImageGrid はサムネイルを並べ、下端に近づいたら続きを読み込む。 */
export function ImageGrid({
  images,
  loading,
  loadingMore,
  hasMore,
  onLoadMore,
  onSelect,
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
        {images.map((image) => (
          <button
            key={image.id}
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
        ))}
      </div>
      <div ref={sentinel} className="sentinel">
        {loadingMore && "読み込み中…"}
      </div>
    </>
  );
}
