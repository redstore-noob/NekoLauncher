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
 * 渐变文字生成器（创作中心的二级界面）：输入文本，配置颜色停靠点，逐字生成
 * Minecraft 彩色字（&#RRGGBB / §x… / &x… / MiniMessage）。
 *
 * 布局：上方大预览（无背景），下方左侧文本 + 颜色、右侧输出。颜色停靠点支持
 * 长按/拖动手柄排序。
 */
import type { GradientStop, OutputFormat, TextFormats } from "./gradient";

import React, { useMemo, useRef, useState } from "react";
import {
  Button,
  Input,
  Modal,
  ModalContent,
  Select,
  SelectItem,
  Textarea,
} from "@heroui/react";
import {
  Add20Regular,
  Checkmark20Regular,
  ClipboardPaste20Regular,
  Copy20Regular,
  Delete20Regular,
  Eye20Regular,
  ReOrderDotsVertical20Regular,
  Save20Regular,
  TextBold20Regular,
  TextFont20Regular,
  TextItalic20Regular,
  TextStrikethrough20Regular,
  TextUnderline20Regular,
} from "@fluentui/react-icons";

import { ModalShell, modalBehaviorProps } from "../modal-shell";
import { notify } from "../overlay/dialog";
import { popoverMotionProps } from "../../lib/motion";
import {
  SaveFile,
  WriteTextFile,
} from "../../../wailsjs/go/bindings/SystemAPI";
import { t } from "../../i18n";

import {
  EMPTY_FORMATS,
  OUTPUT_FORMAT_OPTIONS,
  buildGradientOutput,
  computeCharColors,
  cssGradient,
  normalizeHex,
  parseColors,
} from "./gradient";

interface GradientTextDialogProps {
  isOpen: boolean;
  onClose: () => void;
}

const ADD_COLORS = [
  "#A855F7",
  "#22C55E",
  "#F97316",
  "#0EA5E9",
  "#EF4444",
  "#FACC15",
];

let stopSeq = 0;
const nextStopId = () => `stop-${Date.now().toString(36)}-${(stopSeq += 1)}`;

async function copyText(value: string): Promise<boolean> {
  try {
    await navigator.clipboard.writeText(value);

    return true;
  } catch {
    try {
      const area = document.createElement("textarea");

      area.value = value;
      area.style.position = "fixed";
      area.style.opacity = "0";
      document.body.appendChild(area);
      area.select();
      const ok = document.execCommand("copy");

      area.remove();

      return ok;
    } catch {
      return false;
    }
  }
}

const GradientTextDialog: React.FC<GradientTextDialogProps> = ({
  isOpen,
  onClose,
}) => {
  const [text, setText] = useState("Hello World");
  const [stops, setStops] = useState<GradientStop[]>([
    { id: nextStopId(), color: "#A855F7" },
  ]);
  const [format, setFormat] = useState<OutputFormat>("legacy");
  const [controlChar, setControlChar] = useState<"&" | "§">("&");
  const [formats, setFormats] = useState<TextFormats>(EMPTY_FORMATS);
  const [importText, setImportText] = useState("");
  const [copied, setCopied] = useState(false);
  const [exporting, setExporting] = useState(false);
  const [error, setError] = useState("");

  const stopsRef = useRef(stops);

  stopsRef.current = stops;
  const rowRefs = useRef<Map<string, HTMLDivElement>>(new Map());
  const dragRef = useRef<{
    id: string;
    startY: number;
    active: boolean;
    timer: number;
  } | null>(null);
  const [draggingId, setDraggingId] = useState<string | null>(null);

  const colors = useMemo(
    () => stops.map((stop) => normalizeHex(stop.color) ?? "#FFFFFF"),
    [stops],
  );
  const output = useMemo(
    () =>
      buildGradientOutput({
        text,
        stops: colors,
        format,
        controlChar,
        formats,
      }),
    [colors, controlChar, format, formats, text],
  );
  const charColors = useMemo(
    () => computeCharColors(text, colors),
    [colors, text],
  );

  const patchStop = (id: string, color: string) =>
    setStops((prev) =>
      prev.map((stop) => (stop.id === id ? { ...stop, color } : stop)),
    );

  const addStop = () =>
    setStops((prev) => [
      ...prev,
      { id: nextStopId(), color: ADD_COLORS[prev.length % ADD_COLORS.length] },
    ]);

  const removeStop = (id: string) =>
    setStops((prev) =>
      prev.length <= 1 ? prev : prev.filter((stop) => stop.id !== id),
    );

  const toggleFormat = (key: keyof TextFormats) =>
    setFormats((prev) => ({ ...prev, [key]: !prev[key] }));

  const importColors = () => {
    const parsed = parseColors(importText);

    if (parsed.length === 0) {
      setError(t("没解析出颜色（支持 HEX / RGB / CSS 渐变）"));

      return;
    }
    setStops(parsed.map((color) => ({ id: nextStopId(), color })));
    setImportText("");
    setError("");
  };

  // ---- 拖拽排序：以"被拖项之外"的各行中点判断插入位置，双向都稳定 ----
  const beginDrag = (id: string, event: React.PointerEvent) => {
    event.preventDefault();
    event.currentTarget.setPointerCapture(event.pointerId);
    const state = { id, startY: event.clientY, active: false, timer: 0 };

    state.timer = window.setTimeout(() => {
      state.active = true;
      setDraggingId(id);
    }, 140);
    dragRef.current = state;
  };

  const moveDrag = (event: React.PointerEvent) => {
    const drag = dragRef.current;

    if (!drag) return;
    if (!drag.active) {
      // 长按未触发前就移动：视为直接拖拽（不再取消，避免"拖不动"）
      if (Math.abs(event.clientY - drag.startY) < 4) return;
      drag.active = true;
      window.clearTimeout(drag.timer);
      setDraggingId(drag.id);
    }
    event.preventDefault();

    const list = stopsRef.current;
    const others = list.filter((stop) => stop.id !== drag.id);
    let insertAt = others.length;

    for (let index = 0; index < others.length; index += 1) {
      const element = rowRefs.current.get(others[index].id);

      if (!element) continue;
      const rect = element.getBoundingClientRect();

      if (event.clientY < rect.top + rect.height / 2) {
        insertAt = index;
        break;
      }
    }
    const current = list.findIndex((stop) => stop.id === drag.id);

    if (current === -1 || insertAt === current) return;
    setStops((prev) => {
      const from = prev.findIndex((stop) => stop.id === drag.id);

      if (from === -1) return prev;
      const next = [...prev];
      const [item] = next.splice(from, 1);

      next.splice(Math.max(0, Math.min(insertAt, next.length)), 0, item);

      return next;
    });
  };

  const endDrag = () => {
    if (dragRef.current) window.clearTimeout(dragRef.current.timer);
    dragRef.current = null;
    setDraggingId(null);
  };

  const copyOutput = async () => {
    if (!output) return;
    const ok = await copyText(output);

    if (ok) {
      setCopied(true);
      notify.success(t("已复制到剪贴板"));
      window.setTimeout(() => setCopied(false), 1500);
    } else {
      notify.error(t("复制失败"));
    }
  };

  const exportOutput = async () => {
    setExporting(true);
    try {
      const target = await SaveFile(
        t("导出输出"),
        "gradient-text.txt",
        t("文本文件"),
        "*.txt",
      );

      if (!target) return;
      await WriteTextFile(target, output);
      notify.success(t("已导出"));
    } catch (err) {
      notify.error(err instanceof Error ? err.message : String(err));
    } finally {
      setExporting(false);
    }
  };

  const previewStyle: React.CSSProperties = {
    fontWeight: formats.bold ? 700 : 400,
    fontStyle: formats.italic ? "italic" : "normal",
    textDecoration:
      [
        formats.underline ? "underline" : "",
        formats.strikethrough ? "line-through" : "",
      ]
        .filter(Boolean)
        .join(" ") || "none",
  };

  const toggleButton = (
    key: keyof TextFormats,
    label: string,
    icon: React.ReactNode,
  ) => (
    <Button
      isIconOnly
      aria-label={label}
      className={formats[key] ? "bg-primary/15 text-primary" : ""}
      size="sm"
      title={label}
      variant={formats[key] ? "flat" : "light"}
      onPress={() => toggleFormat(key)}
    >
      {icon}
    </Button>
  );

  const renderPreviewText = () =>
    text ? (
      text.split("").map((char, index) =>
        /\s/.test(char) ? (
          <span key={index}> </span>
        ) : (
          <span
            key={index}
            className={formats.obfuscated ? "nya-mc-obfuscated" : ""}
            style={{ color: charColors[index] ?? undefined }}
          >
            {char}
          </span>
        ),
      )
    ) : (
      <span className="text-gray-400">{t("暂无预览")}</span>
    );

  return (
    <Modal isOpen={isOpen} size="5xl" onClose={onClose} {...modalBehaviorProps}>
      <ModalContent className="h-[88vh] max-h-[88vh]">
        <ModalShell
          icon={<TextFont20Regular />}
          title={t("渐变文字生成器")}
          onClose={onClose}
        >
          <div className="flex h-full min-h-0 flex-col gap-4">
            {/* 上方：大预览（无背景） */}
            <section className="nya-panel-inner nya-border relative min-h-0 flex-1 overflow-hidden rounded-2xl border">
              <div className="absolute right-3 top-3 z-10 flex items-center gap-2">
                <Select
                  aria-label={t("输出格式")}
                  className="w-52 [&_*]:min-w-0"
                  classNames={{ trigger: "h-8 min-h-8" }}
                  popoverProps={{ motionProps: popoverMotionProps }}
                  selectedKeys={[format]}
                  size="sm"
                  onSelectionChange={(keys) =>
                    setFormat((Array.from(keys)[0] as OutputFormat) ?? "legacy")
                  }
                >
                  {OUTPUT_FORMAT_OPTIONS.map((option) => (
                    <SelectItem key={option.value}>{option.label}</SelectItem>
                  ))}
                </Select>
                <div className="flex overflow-hidden rounded-lg border border-default-300">
                  {(["&", "§"] as const).map((char) => (
                    <button
                      key={char}
                      aria-label={t("控制符 {0}", { "0": char })}
                      className={`h-8 w-8 cursor-pointer font-mono text-sm transition-colors ${
                        controlChar === char
                          ? "bg-primary/15 text-primary"
                          : "text-gray-500 hover:bg-default-100 dark:text-gray-300 dark:hover:bg-gray-800"
                      }`}
                      type="button"
                      onClick={() => setControlChar(char)}
                    >
                      {char}
                    </button>
                  ))}
                </div>
              </div>

              <div className="flex h-full items-center justify-center overflow-hidden p-8">
                <p
                  className="max-w-full break-all text-center font-mono text-3xl leading-relaxed tracking-wide"
                  style={previewStyle}
                >
                  {renderPreviewText()}
                </p>
              </div>
            </section>

            {/* 下方：控制区 */}
            <div className="grid h-[40%] min-h-0 flex-none grid-cols-[minmax(0,1.35fr)_minmax(0,1fr)] gap-4">
              {/* 左：文本样式 + 颜色 */}
              <div className="nya-panel-inner nya-border flex min-h-0 flex-col gap-3 overflow-y-auto rounded-2xl border p-3">
                <div className="flex items-center justify-between">
                  <span className="text-[12px] font-semibold text-gray-600 dark:text-gray-300">
                    {t("文本")}
                  </span>
                  <div className="flex gap-0.5">
                    {toggleButton("bold", t("加粗"), <TextBold20Regular />)}
                    {toggleButton("italic", t("斜体"), <TextItalic20Regular />)}
                    {toggleButton(
                      "underline",
                      t("下划线"),
                      <TextUnderline20Regular />,
                    )}
                    {toggleButton(
                      "strikethrough",
                      t("删除线"),
                      <TextStrikethrough20Regular />,
                    )}
                    {toggleButton("obfuscated", t("混淆"), <Eye20Regular />)}
                  </div>
                </div>
                <Textarea
                  aria-label={t("文本")}
                  classNames={{ input: "min-h-14" }}
                  placeholder={t("输入要上色的文字")}
                  size="sm"
                  value={text}
                  onValueChange={setText}
                />

                <div className="flex items-center justify-between">
                  <span className="text-[12px] font-semibold text-gray-600 dark:text-gray-300">
                    {t("颜色（")}
                    {stops.length} {t("· 长按手柄拖动排序）")}
                  </span>
                  <Button
                    size="sm"
                    startContent={<Add20Regular />}
                    variant="flat"
                    onPress={addStop}
                  >
                    {t("添加")}
                  </Button>
                </div>

                <div
                  className="nya-border h-2.5 rounded-full border"
                  style={{ background: cssGradient(colors) }}
                />

                <div className="flex flex-col gap-1.5">
                  {stops.map((stop, index) => (
                    <div
                      key={stop.id}
                      ref={(element) => {
                        if (element) rowRefs.current.set(stop.id, element);
                        else rowRefs.current.delete(stop.id);
                      }}
                      className={`flex items-center gap-2 rounded-lg px-1 py-0.5 transition-colors ${
                        draggingId === stop.id
                          ? "bg-primary/5 ring-2 ring-primary/40"
                          : ""
                      }`}
                    >
                      <button
                        aria-label={t("长按拖动排序：第 {0} 个颜色", {
                          "0": index + 1,
                        })}
                        className="flex size-7 flex-none cursor-grab touch-none items-center justify-center rounded text-gray-400 transition-colors hover:bg-default-100 active:cursor-grabbing dark:hover:bg-gray-800"
                        title={t("长按或拖动排序")}
                        type="button"
                        onPointerCancel={endDrag}
                        onPointerDown={(event) => beginDrag(stop.id, event)}
                        onPointerMove={moveDrag}
                        onPointerUp={endDrag}
                      >
                        <ReOrderDotsVertical20Regular />
                      </button>
                      <input
                        aria-label={t("停靠点 {0} 颜色", { "0": index + 1 })}
                        className="h-8 w-10 flex-none cursor-pointer rounded border border-default-300 bg-transparent"
                        type="color"
                        value={normalizeHex(stop.color) ?? "#ffffff"}
                        onChange={(event) =>
                          patchStop(stop.id, event.target.value)
                        }
                      />
                      <Input
                        aria-label={t("停靠点 {0} HEX", { "0": index + 1 })}
                        classNames={{ inputWrapper: "h-8" }}
                        placeholder="#RRGGBB"
                        size="sm"
                        value={stop.color}
                        onValueChange={(value) => patchStop(stop.id, value)}
                      />
                      <Button
                        isIconOnly
                        aria-label={t("删除该颜色")}
                        isDisabled={stops.length <= 1}
                        size="sm"
                        title={t("删除")}
                        variant="light"
                        onPress={() => removeStop(stop.id)}
                      >
                        <Delete20Regular />
                      </Button>
                    </div>
                  ))}
                </div>

                <div className="flex items-center gap-2">
                  <Input
                    aria-label={t("导入颜色")}
                    classNames={{ inputWrapper: "h-8" }}
                    placeholder={t("粘贴 HEX / RGB / CSS 渐变")}
                    size="sm"
                    value={importText}
                    onValueChange={setImportText}
                  />
                  <Button
                    size="sm"
                    startContent={<ClipboardPaste20Regular />}
                    variant="flat"
                    onPress={importColors}
                  >
                    {t("导入")}
                  </Button>
                </div>

                {error ? (
                  <div className="text-[11px] text-danger">{error}</div>
                ) : null}
              </div>

              <div className="nya-panel-inner nya-border flex min-h-0 flex-col gap-2 rounded-2xl border p-3">
                <div className="flex items-center justify-between">
                  <span className="text-[12px] font-semibold text-gray-600 dark:text-gray-300">
                    {t("输出")}
                  </span>
                  <span className="text-[10px] text-gray-400">
                    {
                      OUTPUT_FORMAT_OPTIONS.find((o) => o.value === format)
                        ?.hint
                    }
                  </span>
                </div>
                <Textarea
                  readOnly
                  aria-label={t("输出文本")}
                  classNames={{
                    base: "min-h-0 flex-1",
                    input: "h-full font-mono text-[12px]",
                  }}
                  size="sm"
                  value={output}
                />
                <div className="flex flex-none justify-end gap-2">
                  <Button
                    size="sm"
                    startContent={
                      copied ? <Checkmark20Regular /> : <Copy20Regular />
                    }
                    variant="flat"
                    onPress={() => void copyOutput()}
                  >
                    {t("复制")}
                  </Button>
                  <Button
                    color="primary"
                    isLoading={exporting}
                    size="sm"
                    startContent={<Save20Regular />}
                    onPress={() => void exportOutput()}
                  >
                    {t("导出")}
                  </Button>
                </div>
              </div>
            </div>
          </div>
        </ModalShell>
      </ModalContent>
    </Modal>
  );
};

export default GradientTextDialog;
