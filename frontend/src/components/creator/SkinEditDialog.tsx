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
 * 皮肤编辑（创作中心的二级界面）：2D 贴图画布 + 3D 实时预览，两边都能画。
 *
 * - 2D：64x64（兼容 64x32）贴图画布，画笔/橡皮/吸管、撤销、区域参考线；
 * - 3D：绘画模式下直接在模型上画（raycast 命中面 UV 反解贴图像素，所见即
 *   所得），点「移动」才切换为拖动/旋转人偶；披风与皮肤内外层可开关；
 * - 源：当前账号皮肤（后端 data URI，canvas 不被污染）或本地 PNG（同源
 *   /localfile 路由）；导出经 WritePngFile 落盘，离线账号可一键应用。
 * 3D 预览组件（SkinPreview3D，含 skinview3d+three 大依赖）经 React.lazy
 * 动态加载，保持独立分包。
 */
import React, {
  Suspense,
  lazy,
  useCallback,
  useEffect,
  useRef,
  useState,
} from "react";
import { Button, Modal, ModalContent, Switch, Tooltip } from "@heroui/react";
import {
  ArrowMove20Regular,
  ArrowUndo20Regular,
  Color20Regular,
  Eraser20Regular,
  Grid20Regular,
  PaintBrush20Regular,
  Person20Regular,
} from "@fluentui/react-icons";

import {
  GetAccountStableKey,
  GetSelectedAccount,
  GetSkinTexture,
  SetOfflineSkin,
} from "../../../wailsjs/go/bindings/AccountAPI";
import {
  OpenInExplorer,
  SaveFile,
  SelectFile,
  WritePngFile,
} from "../../../wailsjs/go/bindings/SystemAPI";
import { ModalShell, modalBehaviorProps } from "../modal-shell";
import { tooltipMotionProps } from "../../lib/motion";
import { t } from "../../i18n";

import CreatorToolShell from "./CreatorToolShell";

/** 与 SkinPreview3D（懒加载块）共享的工具语义 */
type Tool = "pencil" | "eraser" | "eyedropper";
type SkinModel = "classic" | "slim";

// skinview3d + three 体积大：3D 预览单独分包，弹窗打开且已有贴图时才加载
const SkinPreview3D = lazy(() => import("./SkinPreview3D"));

function asMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

/** 常用色板 */
const PALETTE = [
  "#ffffff",
  "#000000",
  "#94a3b8",
  "#7f1d1d",
  "#ef4444",
  "#f97316",
  "#facc15",
  "#22c55e",
  "#0ea5e9",
  "#6366f1",
  "#a855f7",
  "#8b5cf6",
];

/** 64x64 皮肤的标准区域（含第二层），画参考线用 */
const SKIN_REGIONS_64: Array<[number, number, number, number]> = [
  [0, 0, 32, 16],
  [32, 0, 32, 16],
  [0, 16, 16, 16],
  [16, 16, 24, 16],
  [40, 16, 16, 16],
  [0, 32, 16, 16],
  [16, 32, 24, 16],
  [40, 32, 16, 16],
  [0, 48, 16, 16],
  [48, 48, 16, 16],
];

const SkinEditDialog: React.FC<{
  isOpen: boolean;
  onClose: () => void;
  embedded?: boolean;
}> = ({ isOpen, onClose, embedded }) => {
  const [sourceLabel, setSourceLabel] = useState("");
  const [mode, setMode] = useState<"paint" | "move">("paint");
  const [tool, setTool] = useState<Tool>("pencil");
  const [color, setColor] = useState("#ef4444");
  const [model, setModel] = useState<SkinModel>("classic");
  const [showGuides, setShowGuides] = useState(true);
  const [showPalette, setShowPalette] = useState(false);
  const [capeUri, setCapeUri] = useState<string | null>(null);
  const [showCape, setShowCape] = useState(true);
  const [showInner, setShowInner] = useState(true);
  const [showOuter, setShowOuter] = useState(true);
  const [hasImage, setHasImage] = useState(false);
  const [previewUri, setPreviewUri] = useState("");
  const [undoCount, setUndoCount] = useState(0);
  const [savedPath, setSavedPath] = useState("");
  const [applied, setApplied] = useState("");
  const [error, setError] = useState("");
  const [exporting, setExporting] = useState(false);
  const [accountOffline, setAccountOffline] = useState(false);
  const [accountName, setAccountName] = useState("");

  // 贴图画布（真实像素 64xN）不进 DOM；展示画布按倍数放大渲染
  const texCanvasRef = useRef<HTMLCanvasElement | null>(null);
  const viewCanvasRef = useRef<HTMLCanvasElement>(null);
  const undoStackRef = useRef<string[]>([]);
  const drawingRef = useRef(false);
  const previewTimerRef = useRef<number | null>(null);
  // 3D 笔画与 2D 不同：开始时机由首个 texel 回调决定，撤销快照按笔画惰性入栈
  const strokeUndoPushedRef = useRef(false);

  const ensureTexCanvas = useCallback((width: number, height: number) => {
    const canvas = texCanvasRef.current ?? document.createElement("canvas");

    canvas.width = width;
    canvas.height = height;
    texCanvasRef.current = canvas;

    return canvas;
  }, []);

  /** 把贴图画布渲染到展示画布：透明棋盘 + 贴图 + 区域参考线 */
  const redraw = useCallback(() => {
    const tex = texCanvasRef.current;
    const view = viewCanvasRef.current;

    if (!tex || !view) return;
    const scale = 5;
    const viewWidth = tex.width * scale;
    const viewHeight = tex.height * scale;

    if (view.width !== viewWidth || view.height !== viewHeight) {
      view.width = viewWidth;
      view.height = viewHeight;
    }
    const ctx = view.getContext("2d");

    if (!ctx) return;
    for (let y = 0; y < tex.height; y += 1) {
      for (let x = 0; x < tex.width; x += 1) {
        ctx.fillStyle = (x + y) % 2 === 0 ? "#e5e7eb" : "#f3f4f6";
        ctx.fillRect(x * scale, y * scale, scale, scale);
      }
    }
    ctx.imageSmoothingEnabled = false;
    ctx.drawImage(tex, 0, 0, viewWidth, viewHeight);
    if (showGuides) {
      ctx.strokeStyle = "rgba(59,130,246,0.55)";
      ctx.setLineDash([scale, scale]);
      const regions =
        tex.height >= 64 ? SKIN_REGIONS_64 : SKIN_REGIONS_64.slice(0, 5);

      for (const [x, y, width, height] of regions) {
        ctx.strokeRect(
          x * scale + 0.5,
          y * scale + 0.5,
          width * scale - 1,
          height * scale - 1,
        );
      }
      ctx.setLineDash([]);
    }
  }, [showGuides]);

  useEffect(() => {
    if (hasImage) redraw();
  }, [hasImage, redraw]);

  /** 3D 预览节流刷新：笔画结束后同步，避免每个像素都重建贴图 */
  const schedulePreview = useCallback(() => {
    const tex = texCanvasRef.current;

    if (!tex) return;
    if (previewTimerRef.current) window.clearTimeout(previewTimerRef.current);
    previewTimerRef.current = window.setTimeout(() => {
      setPreviewUri(tex.toDataURL("image/png"));
    }, 250);
  }, []);

  const loadSourceImage = useCallback(
    async (
      source: string,
      label: string,
      sourceModel: SkinModel,
      cape: string | null,
    ) => {
      const image = new Image();

      image.src = source;
      await new Promise<void>((resolve, reject) => {
        image.onload = () => resolve();
        image.onerror = () => reject(new Error(t("贴图加载失败")));
      });
      const canvas = ensureTexCanvas(image.width, image.height);
      const ctx = canvas.getContext("2d");

      if (!ctx) throw new Error(t("画布不可用"));
      ctx.clearRect(0, 0, canvas.width, canvas.height);
      ctx.drawImage(image, 0, 0);
      undoStackRef.current = [];
      setUndoCount(0);
      setHasImage(true);
      setSourceLabel(label);
      setModel(sourceModel);
      setCapeUri(cape);
      setError("");
      setSavedPath("");
      setApplied("");
      window.setTimeout(() => schedulePreview(), 0);
    },
    [ensureTexCanvas, schedulePreview],
  );

  const loadFromAccount = async () => {
    try {
      setError("");
      const account = await GetSelectedAccount();

      if (!account?.Type) throw new Error(t("未选择账号"));
      const key = await GetAccountStableKey(account);
      const texture = await GetSkinTexture(key);

      if (!texture?.skinUri) throw new Error(t("该账号没有可用的皮肤贴图"));
      await loadSourceImage(
        texture.skinUri,
        t("当前账号 · {0}", {
          "0": texture.displayName || account.DisplayName,
        }),
        texture.model === "slim" ? "slim" : "classic",
        texture.capeUri || null,
      );
    } catch (err) {
      setError(asMessage(err));
    }
  };

  const loadFromFile = async () => {
    try {
      const path = await SelectFile(t("选择皮肤图片"), t("PNG 图片"), "*.png");

      if (!path) return;
      await loadSourceImage(
        `/localfile?path=${encodeURIComponent(path)}`,
        t("本地文件 · {0}", {
          "0": path.replace(/\\/g, "/").split("/").pop() ?? "",
        }),
        "classic",
        null,
      );
    } catch (err) {
      setError(asMessage(err));
    }
  };

  // 打开时探测当前账号（决定"设为离线账号皮肤"按钮是否可用）
  useEffect(() => {
    if (!isOpen) return;
    setError("");
    setAccountOffline(false);
    setAccountName("");
    void (async () => {
      try {
        const account = await GetSelectedAccount();

        if (!account?.Type) return;
        setAccountName(account.DisplayName);
        setAccountOffline(account.Type === "offline");
      } catch {
        /* 未登录等场景：仅隐藏应用按钮 */
      }
    })();
  }, [isOpen]);

  /** 在贴图像素坐标落笔（2D 与 3D 共用；tool 决定画/擦） */
  const paintAt = useCallback(
    (x: number, y: number, activeTool: Tool) => {
      const tex = texCanvasRef.current;

      if (!tex || x < 0 || y < 0 || x >= tex.width || y >= tex.height) return;
      if (activeTool === "eyedropper") return;
      const ctx = tex.getContext("2d");

      if (!ctx) return;
      if (activeTool === "pencil") {
        ctx.fillStyle = color;
        ctx.fillRect(x, y, 1, 1);
      } else {
        ctx.clearRect(x, y, 1, 1);
      }
      redraw();
    },
    [color, redraw],
  );

  /** 3D 笔画的撤销快照：每笔入栈一次 */
  const paintFrom3D = useCallback(
    (x: number, y: number) => {
      const tex = texCanvasRef.current;

      if (!tex) return;
      if (!strokeUndoPushedRef.current) {
        undoStackRef.current.push(tex.toDataURL("image/png"));
        if (undoStackRef.current.length > 50) undoStackRef.current.shift();
        setUndoCount(undoStackRef.current.length);
        strokeUndoPushedRef.current = true;
      }
      paintAt(x, y, tool === "eyedropper" ? "pencil" : tool);
    },
    [paintAt, tool],
  );

  const pickColorAt = useCallback((x: number, y: number) => {
    const tex = texCanvasRef.current;

    if (!tex) return;
    const pixel = tex.getContext("2d")?.getImageData(x, y, 1, 1).data;

    if (!pixel || pixel[3] === 0) return;
    setColor(
      `#${[pixel[0], pixel[1], pixel[2]]
        .map((v) => v.toString(16).padStart(2, "0"))
        .join("")}`,
    );
    setTool("pencil");
  }, []);

  const pointerToTex = (event: React.PointerEvent<HTMLCanvasElement>) => {
    const tex = texCanvasRef.current;
    const view = viewCanvasRef.current;

    if (!tex || !view) return null;
    const rect = view.getBoundingClientRect();
    const x = Math.floor(
      ((event.clientX - rect.left) / rect.width) * tex.width,
    );
    const y = Math.floor(
      ((event.clientY - rect.top) / rect.height) * tex.height,
    );

    if (x < 0 || y < 0 || x >= tex.width || y >= tex.height) return null;

    return { x, y };
  };

  const handlePointerDown = (event: React.PointerEvent<HTMLCanvasElement>) => {
    const tex = texCanvasRef.current;

    if (!tex || !hasImage) return;
    const point = pointerToTex(event);

    if (!point) return;
    event.currentTarget.setPointerCapture(event.pointerId);
    if (tool === "eyedropper") {
      pickColorAt(point.x, point.y);

      return;
    }
    undoStackRef.current.push(tex.toDataURL("image/png"));
    if (undoStackRef.current.length > 50) undoStackRef.current.shift();
    setUndoCount(undoStackRef.current.length);
    drawingRef.current = true;
    paintAt(point.x, point.y, tool);
  };

  const handlePointerMove = (event: React.PointerEvent<HTMLCanvasElement>) => {
    if (!drawingRef.current) return;
    const point = pointerToTex(event);

    if (point) paintAt(point.x, point.y, tool);
  };

  const handlePointerUp = () => {
    if (drawingRef.current) {
      drawingRef.current = false;
      schedulePreview();
    }
  };

  const undo = () => {
    const tex = texCanvasRef.current;
    const snapshot = undoStackRef.current.pop();

    if (!tex || !snapshot) return;
    const image = new Image();

    image.onload = () => {
      const ctx = tex.getContext("2d");

      if (!ctx) return;
      ctx.clearRect(0, 0, tex.width, tex.height);
      ctx.drawImage(image, 0, 0);
      redraw();
      schedulePreview();
    };
    image.src = snapshot;
    setUndoCount(undoStackRef.current.length);
  };

  const exportSkin = async () => {
    const tex = texCanvasRef.current;

    if (!tex || !hasImage) return;
    setExporting(true);
    setError("");
    try {
      const output = await SaveFile(
        t("导出皮肤"),
        "my-skin.png",
        t("PNG 图片"),
        "*.png",
      );

      if (!output) return; // 用户取消
      await WritePngFile(output, tex.toDataURL("image/png"));
      setSavedPath(output);
    } catch (err) {
      setError(asMessage(err));
    } finally {
      setExporting(false);
    }
  };

  const applyToAccount = async () => {
    if (!savedPath) return;
    try {
      setError("");
      const account = await GetSelectedAccount();

      if (!account?.Type) throw new Error(t("未选择账号"));
      const key = await GetAccountStableKey(account);

      await SetOfflineSkin(key, savedPath);
      setApplied(
        t("已设为 {0} 的皮肤，重新启动游戏生效", { "0": account.DisplayName }),
      );
    } catch (err) {
      setError(asMessage(err));
    }
  };

  const toolButton = (value: Tool, label: string, icon: React.ReactNode) => (
    <Tooltip content={label} delay={300} motionProps={tooltipMotionProps}>
      <Button
        isIconOnly
        aria-label={label}
        className={tool === value ? "bg-primary/15 text-primary" : ""}
        isDisabled={!hasImage}
        size="sm"
        variant={tool === value ? "flat" : "light"}
        onPress={() => setTool(value)}
      >
        {icon}
      </Button>
    </Tooltip>
  );

  const iconToggle = (
    label: string,
    active: boolean,
    onPress: () => void,
    icon: React.ReactNode,
    disabled = false,
  ) => (
    <Tooltip content={label} delay={300} motionProps={tooltipMotionProps}>
      <Button
        isIconOnly
        aria-label={label}
        className={active ? "bg-primary/15 text-primary" : ""}
        isDisabled={disabled}
        size="sm"
        variant={active ? "flat" : "light"}
        onPress={onPress}
      >
        {icon}
      </Button>
    </Tooltip>
  );

  const layerSwitch = (
    label: string,
    value: boolean,
    onChange: (next: boolean) => void,
    disabled = false,
  ) => (
    <span className="flex items-center gap-1.5 text-[11px] text-gray-500 dark:text-gray-400">
      {label}
      <Switch
        aria-label={label}
        color="primary"
        isDisabled={disabled}
        isSelected={value}
        size="sm"
        onValueChange={onChange}
      />
    </span>
  );

  const renderBody = () => (
    <div className="flex h-full min-h-0 flex-col gap-3">
      {/* 工具栏：模式 / 工具 / 颜色 / 撤销 / 参考线 */}
      <div className="nya-panel-inner nya-border flex flex-shrink-0 flex-wrap items-center gap-x-2 gap-y-2 rounded-medium border px-2.5 py-2">
        <div className="flex overflow-hidden rounded-lg border nya-border">
          {(
            [
              ["paint", t("绘画"), <PaintBrush20Regular key="p" />],
              ["move", t("移动"), <ArrowMove20Regular key="m" />],
            ] as const
          ).map(([value, label, icon]) => (
            <button
              key={value}
              aria-pressed={mode === value}
              className={`flex cursor-pointer items-center gap-1 px-2.5 py-1 text-[12px] transition-colors ${
                mode === value
                  ? "bg-primary/15 font-semibold text-primary"
                  : "text-gray-600 hover:bg-default-100 dark:text-gray-300 dark:hover:bg-gray-800"
              }`}
              type="button"
              onClick={() => setMode(value)}
            >
              {icon}
              {label}
            </button>
          ))}
        </div>

        <span className="h-5 w-px bg-default-200" />

        <div className="flex items-center gap-0.5">
          {toolButton("pencil", t("画笔"), <PaintBrush20Regular />)}
          {toolButton("eraser", t("橡皮"), <Eraser20Regular />)}
          {toolButton("eyedropper", t("吸管"), <Color20Regular />)}
        </div>

        <span className="h-5 w-px bg-default-200" />

        <div className="flex items-center gap-1.5">
          <input
            aria-label={t("画笔颜色")}
            className="h-7 w-9 flex-none cursor-pointer rounded border nya-border bg-transparent"
            type="color"
            value={color}
            onChange={(event) => setColor(event.target.value)}
          />
          <button
            aria-expanded={showPalette}
            className={`cursor-pointer rounded px-1.5 py-1 text-[11px] transition-colors ${
              showPalette
                ? "bg-primary/15 text-primary"
                : "text-gray-500 hover:bg-default-100 dark:text-gray-400 dark:hover:bg-gray-800"
            }`}
            type="button"
            onClick={() => setShowPalette((value) => !value)}
          >
            {t("色板")}
          </button>
          {showPalette ? (
            <div className="flex flex-wrap items-center gap-0.5">
              {PALETTE.map((preset) => (
                <button
                  key={preset}
                  aria-label={t("选择颜色 {0}", { "0": preset })}
                  className={`size-5 cursor-pointer rounded border transition-transform hover:scale-110 ${
                    color === preset
                      ? "border-gray-900 ring-2 ring-primary/40 dark:border-white"
                      : "border-transparent"
                  }`}
                  style={{ backgroundColor: preset }}
                  type="button"
                  onClick={() => setColor(preset)}
                />
              ))}
            </div>
          ) : null}
        </div>

        <span className="h-5 w-px bg-default-200" />

        {iconToggle(
          t("撤销"),
          false,
          undo,
          <ArrowUndo20Regular />,
          !hasImage || undoCount === 0,
        )}
        {iconToggle(
          t("区域参考线"),
          showGuides,
          () => setShowGuides((value) => !value),
          <Grid20Regular />,
        )}
      </div>

      <div className="flex min-h-0 flex-1 flex-wrap gap-3">
        {/* 左：2D 贴图画布 */}
        <div className="nya-panel-inner nya-border flex min-h-0 min-w-[300px] flex-1 flex-col rounded-medium border p-3">
          <div className="mb-2 flex flex-shrink-0 items-center justify-between">
            <span className="text-[12px] font-semibold text-gray-600 dark:text-gray-300">
              {t("2D 贴图")}
            </span>
          </div>
          <div className="flex min-h-0 flex-1 items-center justify-center overflow-auto">
            <canvas
              ref={viewCanvasRef}
              aria-label={t("皮肤贴图画布")}
              className={`h-auto max-w-full cursor-crosshair border nya-border bg-gray-50 [image-rendering:pixelated] dark:bg-gray-900 ${
                hasImage ? "" : "opacity-40"
              }`}
              onPointerDown={handlePointerDown}
              onPointerMove={handlePointerMove}
              onPointerUp={handlePointerUp}
            />
          </div>
        </div>

        {/* 右：3D 预览 + 显示开关 + 来源/导出 */}
        <div className="flex min-h-0 w-[320px] flex-none flex-col gap-2.5 overflow-y-auto">
          <div className="nya-panel-inner nya-border flex min-h-[300px] flex-1 flex-col overflow-hidden rounded-medium border p-3">
            <div className="mb-2 flex flex-shrink-0 items-center justify-between">
              <span className="text-[12px] font-semibold text-gray-600 dark:text-gray-300">
                {t("3D 预览")}
              </span>
            </div>
            <div className="min-h-0 flex-1">
              {previewUri ? (
                <Suspense
                  fallback={
                    <div className="flex h-full min-h-[240px] w-full items-center justify-center text-xs text-gray-400">
                      {t("正在加载 3D 渲染器…")}
                    </div>
                  }
                >
                  <SkinPreview3D
                    capeUri={capeUri}
                    model={model}
                    moveMode={true}
                    showCape={showCape}
                    showInner={showInner}
                    showOuter={showOuter}
                    skinUri={previewUri}
                    tool={tool}
                    onStrokeEnd={() => {
                      strokeUndoPushedRef.current = false;
                      schedulePreview();
                    }}
                    onTexelPaint={paintFrom3D}
                    onTexelPick={pickColorAt}
                  />
                </Suspense>
              ) : (
                <div className="flex h-full min-h-[240px] w-full items-center justify-center text-xs text-gray-400">
                  {t("加载皮肤后显示 3D 预览")}
                </div>
              )}
            </div>
          </div>

          <div className="nya-panel-inner nya-border flex flex-col gap-2 rounded-medium border p-3">
            <div className="flex flex-wrap items-center gap-x-4 gap-y-1.5">
              {layerSwitch(t("内层"), showInner, setShowInner, !hasImage)}
              {layerSwitch(t("外层"), showOuter, setShowOuter, !hasImage)}
              {capeUri ? layerSwitch(t("披风"), showCape, setShowCape) : null}
            </div>
            <div className="flex items-center justify-between text-[11px] text-gray-500 dark:text-gray-400">
              <span>{t("模型")}</span>
              <div className="flex overflow-hidden rounded-lg border nya-border">
                {(["classic", "slim"] as const).map((value) => (
                  <button
                    key={value}
                    aria-pressed={model === value}
                    className={`cursor-pointer px-2.5 py-1 transition-colors ${
                      model === value
                        ? "bg-primary/15 font-semibold text-primary"
                        : "text-gray-600 hover:bg-default-100 dark:text-gray-300 dark:hover:bg-gray-800"
                    }`}
                    type="button"
                    onClick={() => setModel(value)}
                  >
                    {value === "classic" ? t("宽臂") : t("细臂")}
                  </button>
                ))}
              </div>
            </div>
          </div>

          <div className="nya-panel-inner nya-border flex flex-col gap-2 rounded-medium border p-3">
            <div className="grid grid-cols-2 gap-1.5">
              <Button
                isDisabled={exporting}
                size="sm"
                variant="flat"
                onPress={() => void loadFromAccount()}
              >
                {t("从当前账号")}
              </Button>
              <Button
                isDisabled={exporting}
                size="sm"
                variant="flat"
                onPress={() => void loadFromFile()}
              >
                {t("从本地文件")}
              </Button>
            </div>

            {error && <div className="text-xs text-danger">{error}</div>}
            {savedPath && !applied && (
              <div className="break-all text-xs text-success">
                {t("已导出：")}
                {savedPath}
              </div>
            )}
            {applied && <div className="text-xs text-success">{applied}</div>}

            <Button
              color="primary"
              isDisabled={!hasImage}
              isLoading={exporting}
              size="sm"
              onPress={() => void exportSkin()}
            >
              {t("导出皮肤 PNG")}
            </Button>
            {savedPath && accountOffline && (
              <Button
                size="sm"
                variant="flat"
                onPress={() => void applyToAccount()}
              >
                {t("设为")} {accountName || t("离线账号")} {t("的皮肤")}
              </Button>
            )}
            {savedPath && !accountOffline && (
              <Button
                size="sm"
                variant="light"
                onPress={() => void OpenInExplorer(savedPath)}
              >
                {t("打开所在文件夹")}
              </Button>
            )}
          </div>
        </div>
      </div>
    </div>
  );

  if (embedded) {
    return (
      <CreatorToolShell
        icon={<Person20Regular />}
        subtitle={sourceLabel || t("2D 贴图与 3D 模型两边都能直接画")}
        title={t("皮肤编辑")}
        onBack={onClose}
      >
        {renderBody()}
      </CreatorToolShell>
    );
  }

  return (
    <Modal isOpen={isOpen} size="4xl" onClose={onClose} {...modalBehaviorProps}>
      <ModalContent className="h-[88vh] max-h-[88vh]">
        <ModalShell
          icon={<Person20Regular />}
          subtitle={sourceLabel || t("2D 贴图与 3D 模型两边都能直接画")}
          title={t("皮肤编辑")}
          onClose={onClose}
        >
          {renderBody()}
        </ModalShell>
      </ModalContent>
    </Modal>
  );
};

export default SkinEditDialog;
