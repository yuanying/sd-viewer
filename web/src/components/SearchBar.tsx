import { useEffect, useRef, useState } from "react";
import { suggestTags } from "../api";
import { useDebounced } from "../hooks";
import type { Filters, SortOrder, TagCount } from "../types";

interface Props {
  filters: Filters;
  onChange: (next: Filters) => void;
}

/** SearchBar は全文検索・タグ補完・並び順・日付範囲をまとめて受け付ける。 */
export function SearchBar({ filters, onChange }: Props) {
  const [text, setText] = useState(filters.q);
  const [tagInput, setTagInput] = useState("");
  const [suggestions, setSuggestions] = useState<TagCount[]>([]);
  const [open, setOpen] = useState(false);
  const debouncedText = useDebounced(text, 300);
  const debouncedTag = useDebounced(tagInput, 200);
  const box = useRef<HTMLDivElement>(null);

  // 外から条件が変わったとき（戻る操作など）に入力欄を合わせる。
  useEffect(() => setText(filters.q), [filters.q]);

  useEffect(() => {
    if (debouncedText !== filters.q) {
      onChange({ ...filters, q: debouncedText });
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [debouncedText]);

  useEffect(() => {
    if (debouncedTag.trim() === "") {
      setSuggestions([]);
      return;
    }
    const controller = new AbortController();
    suggestTags(debouncedTag.trim(), 12, controller.signal)
      .then(setSuggestions)
      .catch(() => setSuggestions([]));
    return () => controller.abort();
  }, [debouncedTag]);

  useEffect(() => {
    const onClickOutside = (e: MouseEvent) => {
      if (box.current && !box.current.contains(e.target as Node)) {
        setOpen(false);
      }
    };
    document.addEventListener("mousedown", onClickOutside);
    return () => document.removeEventListener("mousedown", onClickOutside);
  }, []);

  const addTag = (tag: string) => {
    if (!filters.tag.includes(tag)) {
      onChange({ ...filters, tag: [...filters.tag, tag] });
    }
    setTagInput("");
    setSuggestions([]);
    setOpen(false);
  };

  return (
    <div className="searchbar">
      <input
        className="search-input"
        type="search"
        value={text}
        placeholder="プロンプトを検索（空白で AND、-語 で除外）"
        onChange={(e) => setText(e.target.value)}
      />

      <div className="tag-picker" ref={box}>
        <input
          className="tag-input"
          type="text"
          value={tagInput}
          placeholder="タグで絞り込む"
          onFocus={() => setOpen(true)}
          onChange={(e) => {
            setTagInput(e.target.value);
            setOpen(true);
          }}
          onKeyDown={(e) => {
            if (e.key === "Enter" && tagInput.trim() !== "") {
              e.preventDefault();
              addTag(suggestions[0]?.tag ?? tagInput.trim().toLowerCase());
            }
            if (e.key === "Escape") {
              setOpen(false);
            }
          }}
        />
        {open && suggestions.length > 0 && (
          <ul className="suggestions">
            {suggestions.map((s) => (
              <li key={s.tag}>
                <button type="button" onClick={() => addTag(s.tag)}>
                  <span>{s.tag}</span>
                  <span className="count">{s.count.toLocaleString()}</span>
                </button>
              </li>
            ))}
          </ul>
        )}
      </div>

      <label className="field">
        <span>期間</span>
        <input
          type="date"
          value={filters.from}
          onChange={(e) => onChange({ ...filters, from: e.target.value })}
        />
        <span aria-hidden>–</span>
        <input
          type="date"
          value={filters.to}
          onChange={(e) => onChange({ ...filters, to: e.target.value })}
        />
      </label>

      <label className="field">
        <span>並び</span>
        <select
          value={filters.sort}
          onChange={(e) => onChange({ ...filters, sort: e.target.value as SortOrder })}
        >
          <option value="newest">新しい順</option>
          <option value="oldest">古い順</option>
          <option value="name">パス順</option>
        </select>
      </label>
    </div>
  );
}
