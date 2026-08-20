export interface Lora {
  name: string;
  weight?: number;
  hash?: string;
}

export interface Image {
  id: number;
  root: string;
  path: string;
  dir: string;
  name: string;
  size: number;
  mod_time: string;
  width: number;
  height: number;
  created_at: string;
  has_params: boolean;
  prompt: string;
  negative: string;
  model: string;
  model_hash: string;
  sampler: string;
  schedule_type: string;
  steps: number;
  cfg_scale: number;
  seed: number;
  denoising: number;
  version: string;
  gen_width: number;
  gen_height: number;
  loras?: Lora[];
  /** 詳細取得でのみ入る。 */
  positive_tags?: string[];
  negative_tags?: string[];
  extras?: Record<string, string>;
  raw?: string;
}

export interface SearchResult {
  total: number;
  images: Image[];
}

export interface FacetValue {
  value: string;
  count: number;
}

export interface FacetSet {
  models: FacetValue[];
  loras: FacetValue[];
  samplers: FacetValue[];
  sizes: FacetValue[];
  dirs: FacetValue[];
  roots: FacetValue[];
}

export interface TagCount {
  tag: string;
  count: number;
}

export interface Status {
  total: number;
  roots: string[];
  scan: {
    scanning: boolean;
    walked: number;
    indexed: number;
    removed: number;
    failed: number;
  };
  thumbnails: number;
  /** WebUI へ生成情報を送れるかどうか。--webui-url を指定すると立つ。 */
  webui: boolean;
}

/** 生成情報の送り先となる WebUI のタブ。 */
export type SendTarget = "txt2img" | "img2img";

export type SortOrder = "newest" | "oldest" | "name";

/** ファセットとして複数の値を選べる項目。 */
export type FacetKey =
  | "model"
  | "lora"
  | "sampler"
  | "size"
  | "dir"
  | "root"
  | "tag"
  | "exclude_tag";

export interface Filters {
  q: string;
  model: string[];
  lora: string[];
  sampler: string[];
  size: string[];
  dir: string[];
  root: string[];
  tag: string[];
  exclude_tag: string[];
  from: string;
  to: string;
  sort: SortOrder;
}
