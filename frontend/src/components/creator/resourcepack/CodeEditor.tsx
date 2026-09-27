/*
 * Copyright 2026 烟花
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */
/*
 * 轻量代码编辑器：透明 textarea 叠在高亮层上（同步滚动），左边行号，
 * JSON 提供令牌高亮与基于当前文档 + 静态词典的补全下拉。
 * 无第三方编辑器依赖，保持打包体积。
 */
import React, { useEffect, useMemo, useRef, useState } from "react";

import { t } from "../../../i18n";

import {
  TOKEN_CLASS,
  buildCompletions,
  caretLineCol,
  tokenize,
  wordAtCaret,
  type CodeLanguage,
} from "./code";

interface CodeEditorProps {
  value: string;
  /** 编辑语言决定高亮与补全：json / yaml / kv（properties 等键值对）/ plain */
  language: CodeLanguage;
  onChange: (value: string) => void;
  placeholder?: string;
}

const FONT_SIZE = 12;
const LINE_HEIGHT = 18;
const PADDING = 12;
const DROPDOWN_WIDTH = 224;
const DROPDOWN_MAX_HEIGHT = 160;

const CodeEditor: React.FC<CodeEditorProps> = ({
  value,
  language,
  onChange,
  placeholder,
}) => {
  const taRef = useRef<HTMLTextAreaElement>(null);
  const preRef = useRef<HTMLPreElement>(null);
  const gutterRef = useRef<HTMLDivElement>(null);
  const measureRef = useRef<HTMLSpanElement>(null);

  const [charWidth, setCharWidth] = useState(7.2);
  const [suggestions, setSuggestions] = useState<string[]>([]);
  const [active, setActive] = useState(0);
  const [anchor, setAnchor] = useState<{ x: number; y: number } | null>(null);
  const [open, setOpen] = useState(false);

  const tokens = useMemo(() => tokenize(value, language), [language, value]);
  const lineCount = useMemo(() => value.split("\n").length, [value]);
  const gutterDigits = String(Math.max(1, lineCount)).length;

  useEffect(() => {
    const measure = measureRef.current;

    if (!measure) return;
    const width = measure.getBoundingClientRect().width / 40;

    if (width > 0) setCharWidth(width);
  }, []);

  const syncScroll = () => {
    const ta = taRef.current;

    if (!ta) return;
    if (preRef.current) {
      preRef.current.scrollTop = ta.scrollTop;
      preRef.current.scrollLeft = ta.scrollLeft;
    }
    if (gutterRef.current) gutterRef.current.scrollTop = ta.scrollTop;
  };

  const refreshSuggestions = () => {
    const ta = taRef.current;

    if (!ta) return;
    const text = ta.value;
    const caret = ta.selectionStart ?? 0;
    const prefix = wordAtCaret(text, caret);
    const list = buildCompletions(text, language, prefix);

    if (list.length === 0) {
      setOpen(false);
      setSuggestions([]);

      return;
    }
    const { line, col } = caretLineCol(text, caret);
    const x = PADDING + col * charWidth - ta.scrollLeft;
    const y = PADDING + (line + 1) * LINE_HEIGHT - ta.scrollTop;

    setSuggestions(list.slice(0, 8));
    setActive(0);
    setAnchor({
      x: Math.min(Math.max(0, x), Math.max(0, ta.clientWidth - DROPDOWN_WIDTH)),
      y: Math.min(
        Math.max(0, y),
        Math.max(0, ta.clientHeight - DROPDOWN_MAX_HEIGHT),
      ),
    });
    setOpen(true);
  };

  const applySuggestion = (candidate: string) => {
    const ta = taRef.current;

    if (!ta) return;
    const text = ta.value;
    const caret = ta.selectionStart ?? 0;
    const prefix = wordAtCaret(text, caret);
    const start = caret - prefix.length;
    const next = text.slice(0, start) + candidate + text.slice(caret);

    onChange(next);
    setOpen(false);
    requestAnimationFrame(() => {
      const position = start + candidate.length;

      ta.setSelectionRange(position, position);
      ta.focus();
    });
  };

  const handleKeyDown = (event: React.KeyboardEvent<HTMLTextAreaElement>) => {
    if (open && suggestions.length > 0) {
      if (event.key === "ArrowDown") {
        event.preventDefault();
        setActive((index) => (index + 1) % suggestions.length);

        return;
      }
      if (event.key === "ArrowUp") {
        event.preventDefault();
        setActive(
          (index) => (index - 1 + suggestions.length) % suggestions.length,
        );

        return;
      }
      if (event.key === "Enter" || event.key === "Tab") {
        event.preventDefault();
        applySuggestion(suggestions[active]);

        return;
      }
      if (event.key === "Escape") {
        event.preventDefault();
        setOpen(false);

        return;
      }
    }

    if (event.key === "Tab") {
      // 没有补全时 Tab 插入两个空格，避免焦点跳出编辑器
      event.preventDefault();
      const ta = event.currentTarget;
      const caret = ta.selectionStart ?? 0;
      const end = ta.selectionEnd ?? caret;
      const next = `${ta.value.slice(0, caret)}  ${ta.value.slice(end)}`;

      onChange(next);
      requestAnimationFrame(() => {
        ta.setSelectionRange(caret + 2, caret + 2);
      });
    }
  };

  return (
    <div className="nya-border relative flex min-h-0 flex-1 overflow-hidden rounded-lg border bg-default-50/60 dark:bg-gray-900/50">
      <span
        ref={measureRef}
        aria-hidden
        className="pointer-events-none invisible absolute font-mono text-[12px] leading-[18px] whitespace-pre"
      >
        0000000000000000000000000000000000000000
      </span>

      {/* 行号 */}
      <div
        ref={gutterRef}
        aria-hidden
        className="nya-border flex-shrink-0 select-none overflow-hidden border-r border-default-200/60 py-3 text-right font-mono text-[12px] leading-[18px] text-gray-400"
        style={{ width: `${gutterDigits * charWidth + 22}px` }}
      >
        {Array.from({ length: lineCount }, (_, index) => (
          <div key={index} className="pr-2.5">
            {index + 1}
          </div>
        ))}
      </div>

      <div className="relative min-w-0 flex-1">
        {/* 高亮层 */}
        <pre
          ref={preRef}
          aria-hidden
          className="pointer-events-none absolute inset-0 m-0 overflow-hidden p-3 font-mono text-[12px] leading-[18px] whitespace-pre"
          style={{ fontSize: FONT_SIZE }}
        >
          {tokens.map((token, index) => (
            <span key={index} className={TOKEN_CLASS[token.type]}>
              {token.value}
            </span>
          ))}
          {"\n"}
        </pre>

        {/* 输入层：文字透明，露出下方高亮 */}
        <textarea
          ref={taRef}
          aria-label={t("文件内容")}
          className="absolute inset-0 h-full w-full resize-none overflow-auto bg-transparent p-3 font-mono text-[12px] leading-[18px] text-transparent outline-none caret-sky-500 selection:bg-sky-400/30"
          placeholder={placeholder}
          spellCheck={false}
          style={{ fontSize: FONT_SIZE }}
          value={value}
          wrap="off"
          onBlur={() => setOpen(false)}
          onChange={(event) => {
            onChange(event.target.value);
            refreshSuggestions();
          }}
          onClick={refreshSuggestions}
          onKeyDown={handleKeyDown}
          onKeyUp={(event) => {
            if (
              !["ArrowDown", "ArrowUp", "Enter", "Tab", "Escape"].includes(
                event.key,
              )
            ) {
              refreshSuggestions();
            }
          }}
          onScroll={syncScroll}
        />

        {/* 补全下拉 */}
        {open && anchor && suggestions.length > 0 ? (
          <div
            className="absolute z-20 overflow-y-auto rounded-lg border border-default-200 bg-default-50 py-1 shadow-lg"
            style={{
              left: anchor.x,
              top: anchor.y,
              width: DROPDOWN_WIDTH,
              maxHeight: DROPDOWN_MAX_HEIGHT,
            }}
          >
            {suggestions.map((item, index) => (
              <button
                key={item}
                className={`block w-full truncate px-3 py-1 text-left font-mono text-[12px] ${
                  index === active
                    ? "bg-primary/15 text-primary"
                    : "text-gray-600 dark:text-gray-300"
                }`}
                type="button"
                onMouseDown={(event) => {
                  event.preventDefault();
                  applySuggestion(item);
                }}
                onMouseEnter={() => setActive(index)}
              >
                {item}
              </button>
            ))}
          </div>
        ) : null}
      </div>
    </div>
  );
};

export default CodeEditor;
