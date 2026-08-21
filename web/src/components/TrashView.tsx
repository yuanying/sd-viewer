import { thumbUrl } from "../api";
import type { Image, TrashCount } from "../types";

interface Props {
  images: Image[];
  /** counts はルートごとの件数。読み込んだ分より多いこともある。 */
  counts: TrashCount[];
  loading: boolean;
  error: string | null;
  hasMore: boolean;
  onLoadMore: () => void;
  onRestore: (ids: number[]) => void;
  onPurge: (ids: number[]) => void;
  onEmpty: (root: string) => void;
}

/** TrashView はゴミ箱の中身をルートごとに分けて見せる。 */
export function TrashView({
  images,
  counts,
  loading,
  error,
  hasMore,
  onLoadMore,
  onRestore,
  onPurge,
  onEmpty,
}: Props) {
  // 件数の多いルートから並べ、読み込んだ画像を割り当てる。
  const groups = counts
    .map((c) => ({ ...c, images: images.filter((img) => img.root === c.root) }))
    .filter((g) => g.count > 0);

  return (
    <section className="trash" aria-label="ゴミ箱">
      {error && <p className="error">読み込みに失敗しました: {error}</p>}
      {groups.length === 0 && !loading && <p className="notice">ゴミ箱は空です。</p>}

      {groups.map((group) => (
        <section key={group.root} className="trash-group" role="group" aria-label={group.root}>
          <h2>
            {group.root}
            <span className="count">{group.count.toLocaleString()} 件</span>
            <button type="button" className="action danger" onClick={() => onEmpty(group.root)}>
              空にする
            </button>
          </h2>

          <div className="grid">
            {group.images.map((image) => (
              <div key={image.id} className="cell-slot">
                <div className="cell trash-cell" title={image.orig_path ?? image.path}>
                  <img
                    src={thumbUrl(image.id)}
                    alt={image.name}
                    loading="lazy"
                    decoding="async"
                    style={{ aspectRatio: `${image.width || 1} / ${image.height || 1}` }}
                  />
                  <span className="cell-caption">{image.name}</span>
                  <div className="trash-actions">
                    <button
                      type="button"
                      className="action"
                      aria-label={`${image.name} を元に戻す`}
                      onClick={() => onRestore([image.id])}
                    >
                      元に戻す
                    </button>
                    <button
                      type="button"
                      className="action danger"
                      aria-label={`${image.name} を完全に削除`}
                      onClick={() => onPurge([image.id])}
                    >
                      完全に削除
                    </button>
                  </div>
                </div>
              </div>
            ))}
          </div>
        </section>
      ))}

      {hasMore && (
        <div className="sentinel">
          <button type="button" className="link" onClick={onLoadMore} disabled={loading}>
            さらに読み込む
          </button>
        </div>
      )}
    </section>
  );
}
