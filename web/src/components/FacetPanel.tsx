import { useState } from "react";
import { facetLabels, toggleFacet } from "../filters";
import type { FacetKey, FacetSet, FacetValue, Filters } from "../types";

interface Props {
  facets: FacetSet | null;
  filters: Filters;
  onChange: (next: Filters) => void;
}

/** 初期表示で見せる候補の数。 */
const visibleCount = 12;

/** 候補が多いファセットには絞り込み用の入力を出す。 */
const searchableFrom = 12;

const groups: { key: FacetKey; pick: (f: FacetSet) => FacetValue[] }[] = [
  { key: "model", pick: (f) => f.models },
  { key: "lora", pick: (f) => f.loras },
  { key: "sampler", pick: (f) => f.samplers },
  { key: "size", pick: (f) => f.sizes },
  { key: "dir", pick: (f) => f.dirs },
  { key: "root", pick: (f) => f.roots },
];

export function FacetPanel({ facets, filters, onChange }: Props) {
  return (
    <aside className="facets">
      <SelectedTags filters={filters} onChange={onChange} />
      {groups.map(({ key, pick }) => (
        <FacetGroup
          key={key}
          facetKey={key}
          // 古いサーバは候補の無い項目を null で返すため、空として扱う。
          values={(facets && pick(facets)) ?? []}
          selected={filters[key]}
          onToggle={(value) => onChange(toggleFacet(filters, key, value))}
        />
      ))}
    </aside>
  );
}

/** SelectedTags は選択中のタグと除外タグをまとめて外せるようにする。 */
function SelectedTags({ filters, onChange }: Omit<Props, "facets">) {
  if (filters.tag.length === 0 && filters.exclude_tag.length === 0) {
    return null;
  }
  return (
    <section className="facet-group">
      <h2>選択中のタグ</h2>
      <div className="chips">
        {filters.tag.map((tag) => (
          <button
            key={tag}
            type="button"
            className="chip"
            onClick={() => onChange(toggleFacet(filters, "tag", tag))}
          >
            {tag} ×
          </button>
        ))}
        {filters.exclude_tag.map((tag) => (
          <button
            key={tag}
            type="button"
            className="chip chip-exclude"
            onClick={() => onChange(toggleFacet(filters, "exclude_tag", tag))}
          >
            -{tag} ×
          </button>
        ))}
      </div>
    </section>
  );
}

interface GroupProps {
  facetKey: FacetKey;
  values: FacetValue[];
  selected: string[];
  onToggle: (value: string) => void;
}

function FacetGroup({ facetKey, values, selected, onToggle }: GroupProps) {
  const [expanded, setExpanded] = useState(false);
  const [needle, setNeedle] = useState("");

  if (values.length === 0 && selected.length === 0) {
    return null;
  }

  const matched = needle.trim()
    ? values.filter((v) => v.value.toLowerCase().includes(needle.trim().toLowerCase()))
    : values;
  // 選択中の値は候補から外れても残す。
  const shown = expanded ? matched : matched.slice(0, visibleCount);
  const missing = selected.filter((s) => !shown.some((v) => v.value === s));

  return (
    <section className="facet-group">
      <h2>
        {facetLabels[facetKey]}
        <span className="count">{values.length}</span>
      </h2>

      {values.length >= searchableFrom && (
        <input
          className="facet-filter"
          type="search"
          value={needle}
          placeholder="候補を絞り込む"
          onChange={(e) => setNeedle(e.target.value)}
        />
      )}

      <ul className="facet-values">
        {missing.map((value) => (
          <FacetItem
            key={value}
            value={value}
            count={null}
            checked
            onToggle={() => onToggle(value)}
          />
        ))}
        {shown.map((v) => (
          <FacetItem
            key={v.value}
            value={v.value}
            count={v.count}
            checked={selected.includes(v.value)}
            onToggle={() => onToggle(v.value)}
          />
        ))}
      </ul>

      {matched.length > visibleCount && (
        <button type="button" className="link" onClick={() => setExpanded(!expanded)}>
          {expanded ? "折りたたむ" : `すべて表示（${matched.length}）`}
        </button>
      )}
    </section>
  );
}

function FacetItem({
  value,
  count,
  checked,
  onToggle,
}: {
  value: string;
  count: number | null;
  checked: boolean;
  onToggle: () => void;
}) {
  return (
    <li>
      <label className={checked ? "selected" : undefined} title={value}>
        <input type="checkbox" checked={checked} onChange={onToggle} />
        <span className="value">{value}</span>
        {count !== null && <span className="count">{count.toLocaleString()}</span>}
      </label>
    </li>
  );
}
