import { useEffect, useState } from "react";
import { fetchImage, rawUrl, sendToWebUI } from "../api";
import { copyText } from "../clipboard";
import { errorMessage } from "../hooks";
import { toggleFacet } from "../filters";
import type { FacetKey, Filters, Image, SendTarget } from "../types";

interface Props {
  id: number;
  filters: Filters;
  onChange: (next: Filters) => void;
  onClose: () => void;
  onPrev: () => void;
  onNext: () => void;
  /** WebUI へ送れるかどうか。送れないときは送信ボタンを出さない。 */
  canSend: boolean;
  /** onTrash は開いている 1 枚をゴミ箱へ入れる。 */
  onTrash: (id: number) => void;
}

/** ImageDetail は 1 枚の生成情報を並べ、そこから絞り込めるようにする。 */
export function ImageDetail({
  id,
  filters,
  onChange,
  onClose,
  onPrev,
  onNext,
  canSend,
  onTrash,
}: Props) {
  const [image, setImage] = useState<Image | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    const controller = new AbortController();
    setImage(null);
    fetchImage(id, controller.signal)
      .then((img) => {
        setImage(img);
        setError(null);
      })
      .catch((err: unknown) => {
        if (!controller.signal.aborted) {
          setError(errorMessage(err));
        }
      });
    return () => controller.abort();
  }, [id]);

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
      if (e.key === "ArrowLeft") onPrev();
      if (e.key === "ArrowRight") onNext();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose, onPrev, onNext]);

  /** 絞り込みを掛け直して詳細を閉じる。 */
  const narrow = (key: FacetKey, value: string) => {
    onChange(toggleFacet(filters, key, value));
    onClose();
  };

  return (
    <div className="overlay" onClick={onClose}>
      <div className="detail" onClick={(e) => e.stopPropagation()}>
        <button type="button" className="close" onClick={onClose} aria-label="閉じる">
          ×
        </button>

        <div className="detail-image">
          {image && (
            <a href={rawUrl(image.id)} target="_blank" rel="noreferrer">
              <img src={rawUrl(image.id)} alt={image.name} />
            </a>
          )}
          <div className="detail-nav">
            <button type="button" onClick={onPrev} aria-label="前の画像">
              ←
            </button>
            <button type="button" onClick={onNext} aria-label="次の画像">
              →
            </button>
          </div>
        </div>

        <div className="detail-meta">
          {error && <p className="error">{error}</p>}
          {!image && !error && <p className="notice">読み込み中…</p>}
          {image && (
            <Meta image={image} narrow={narrow} canSend={canSend} onTrash={onTrash} />
          )}
        </div>
      </div>
    </div>
  );
}

function Meta({
  image,
  narrow,
  canSend,
  onTrash,
}: {
  image: Image;
  narrow: (key: FacetKey, value: string) => void;
  canSend: boolean;
  onTrash: (id: number) => void;
}) {
  return (
    <>
      <h2 title={image.path}>{image.name}</h2>
      <p className="path">
        {image.root} / {image.dir}
      </p>

      <Actions image={image} canSend={canSend} onTrash={onTrash} />

      <dl className="params">
        <Row label="生成日時">{new Date(image.created_at).toLocaleString()}</Row>
        <Row label="サイズ">
          {image.width}×{image.height}
          {image.gen_width > 0 && image.gen_width !== image.width && (
            <span className="muted">（指定 {image.gen_width}×{image.gen_height}）</span>
          )}
        </Row>
        {image.model && (
          <Row label="モデル">
            <button type="button" className="link" onClick={() => narrow("model", image.model)}>
              {image.model}
            </button>
            {image.model_hash && <span className="muted">{image.model_hash}</span>}
          </Row>
        )}
        {image.loras && image.loras.length > 0 && (
          <Row label="LoRA">
            <span className="chips">
              {image.loras.map((lora) => (
                <button
                  key={lora.name}
                  type="button"
                  className="chip"
                  onClick={() => narrow("lora", lora.name)}
                >
                  {lora.name}
                  {lora.weight ? ` : ${lora.weight}` : ""}
                </button>
              ))}
            </span>
          </Row>
        )}
        {image.sampler && (
          <Row label="サンプラー">
            <button type="button" className="link" onClick={() => narrow("sampler", image.sampler)}>
              {image.sampler}
            </button>
            {image.schedule_type && <span className="muted">{image.schedule_type}</span>}
          </Row>
        )}
        {image.has_params && (
          <Row label="生成設定">
            Steps {image.steps} / CFG {image.cfg_scale} / Seed {image.seed}
            {image.denoising > 0 && ` / Denoise ${image.denoising}`}
          </Row>
        )}
      </dl>

      <Prompt title="プロンプト" text={image.prompt} />
      <Prompt title="ネガティブ" text={image.negative} />

      {image.positive_tags && image.positive_tags.length > 0 && (
        <section>
          <h3>タグ</h3>
          <div className="chips">
            {image.positive_tags.map((tag) => (
              <button
                key={tag}
                type="button"
                className="chip"
                onClick={() => narrow("tag", tag)}
                title="このタグで絞り込む"
              >
                {tag}
              </button>
            ))}
          </div>
        </section>
      )}

      {image.extras && Object.keys(image.extras).length > 0 && (
        <section>
          <h3>その他のパラメータ</h3>
          <dl className="params">
            {Object.entries(image.extras)
              .sort(([a], [b]) => a.localeCompare(b))
              .map(([key, value]) => (
                <Row key={key} label={key}>
                  {value}
                </Row>
              ))}
          </dl>
        </section>
      )}

      {image.raw && (
        <details className="raw">
          <summary>元のテキスト</summary>
          <pre>{image.raw}</pre>
        </details>
      )}
    </>
  );
}

function Row({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <>
      <dt>{label}</dt>
      <dd>{children}</dd>
    </>
  );
}

function Prompt({ title, text }: { title: string; text: string }) {
  if (text === "") {
    return null;
  }
  return (
    <section>
      <h3>
        {title}
        <CopyButton text={text} label="コピー" />
      </h3>
      <p className="prompt">{text}</p>
    </section>
  );
}

/** Actions は 1 枚に対してできることを、詳細の先頭にまとめて並べる。 */
function Actions({
  image,
  canSend,
  onTrash,
}: {
  image: Image;
  canSend: boolean;
  onTrash: (id: number) => void;
}) {
  const [notice, setNotice] = useState<string | null>(null);
  const [failed, setFailed] = useState(false);

  /** report は結果をしばらく出してから消す。 */
  const report = (message: string, isError: boolean) => {
    setNotice(message);
    setFailed(isError);
    window.setTimeout(() => setNotice(null), 2500);
  };

  const send = (target: SendTarget) => {
    sendToWebUI(image.id, target).then(
      () => report(`${target} へ送りました`, false),
      (err: unknown) => report(errorMessage(err), true),
    );
  };

  const copy = (text: string, what: string) => {
    copyText(text).then(
      () => report(`${what}をコピーしました`, false),
      () => report("コピーできませんでした", true),
    );
  };

  return (
    <div className="actions">
      {canSend && (
        <>
          <button type="button" className="action" onClick={() => send("txt2img")}>
            txt2img へ送る
          </button>
          <button type="button" className="action" onClick={() => send("img2img")}>
            img2img へ送る
          </button>
        </>
      )}
      {image.raw && (
        <button type="button" className="link" onClick={() => copy(image.raw!, "生成情報")}>
          生成情報をコピー
        </button>
      )}
      <button type="button" className="action danger" onClick={() => onTrash(image.id)}>
        ゴミ箱へ移動
      </button>
      {notice && (
        <span className={failed ? "actions-notice failed" : "actions-notice"}>{notice}</span>
      )}
    </div>
  );
}

function CopyButton({ text, label }: { text: string; label: string }) {
  const [copied, setCopied] = useState(false);
  const [failed, setFailed] = useState(false);
  return (
    <button
      type="button"
      className={failed ? "link error" : "link"}
      onClick={() => {
        copyText(text).then(
          () => {
            setFailed(false);
            setCopied(true);
            window.setTimeout(() => setCopied(false), 1500);
          },
          () => {
            setCopied(false);
            setFailed(true);
          },
        );
      }}
    >
      {failed ? "コピーできません" : copied ? "コピーしました" : label}
    </button>
  );
}
