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
 * 资源包贴图编辑器：隐藏 canvas 是唯一数据源，展示 canvas 手绘透明棋盘 + 网格 +
 * 选区蚂蚁线 + 图形预览。
 *
 * 工具：画笔 / 橡皮 / 油漆桶 / 吸管 / 直线 / 矩形（描边 / 填充）/ 椭圆 / 选区。
 * 选区可整体拖动、方向键微移、删除 / 填充 / 翻转，并支持整图水平 / 垂直镜像。
 * 每张贴图独立撤销 / 重做栈（由上层按 id 持有，切换文件不丢历史）。
 */
import type { PackFile, TextureHistory } from "./types";

import React, {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { Button, Select, SelectItem } from "@heroui/react";
import {
  Add20Regular,
  ArrowRedo20Regular,
  ArrowUndo20Regular,
  Broom20Regular,
  Color20Regular,
  Eraser20Regular,
  FlipHorizontal20Regular,
  FlipVertical20Regular,
  Grid20Regular,
  LineHorizontal120Regular,
  Oval20Regular,
  PaintBrush20Regular,
  PaintBucket20Regular,
  RectangleLandscape20Regular,
  SelectObject20Regular,
  Subtract20Regular,
} from "@fluentui/react-icons";

import { selectPopoverProps } from "../../../lib/motion";
import { t } from "../../../i18n";

import { hexToRgb, makeCanvas, TEXTURE_SIZE_OPTIONS } from "./types";

export type TextureTool =
  | "pencil"
  | "eraser"
  | "fill"
  | "eyedropper"
  | "line"
  | "rect"
  | "rectFill"
  | "ellipse"
  | "select";

interface Rect {
  x: number;
  y: number;
  w: number;
  h: number;
}

interface Point {
  x: number;
  y: number;
}

interface Shape {
  type: "line" | "rect" | "rectFill" | "ellipse";
  start: Point;
  current: Point;
}

interface TextureEditorProps {
  file: PackFile;
  history: TextureHistory;
  color: string;
  tool: TextureTool;
  recentColors: string[];
  onColorChange: (color: string) => void;
  onToolChange: (tool: TextureTool) => void;
  /** 画布内容发生变化（用于上层触发自动保存草稿） */
  onContentChange?: () => void;
}

/** 常用色板（按行星度分组，像素画足够用） */
const PALETTE = [
  "#000000",
  "#3f3f46",
  "#71717a",
  "#a1a1aa",
  "#d4d4d8",
  "#ffffff",
  "#7f1d1d",
  "#dc2626",
  "#f97316",
  "#f59e0b",
  "#facc15",
  "#fde68a",
  "#14532d",
  "#16a34a",
  "#22c55e",
  "#4ade80",
  "#a3e635",
  "#d9f99d",
  "#0c4a6e",
  "#0284c7",
  "#0ea5e9",
  "#38bdf8",
  "#67e8f9",
  "#a5f3fc",
  "#1e1b4b",
  "#4f46e5",
  "#6366f1",
  "#8b5cf6",
  "#a855f7",
  "#d946ef",
  "#831843",
  "#db2777",
  "#f472b6",
  "#fb7185",
  "#fda4af",
  "#fecdd3",
  "#451a03",
  "#92400e",
  "#b45309",
  "#d97706",
  "#fbbf24",
  "#fef3c7",
];

const BRUSH_SIZES = [1, 2, 3, 4, 5, 6, 8];

/** Bresenham 直线上的整数点 */
function linePoints(x0: number, y0: number, x1: number, y1: number): Point[] {
  const points: Point[] = [];
  const dx = Math.abs(x1 - x0);
  const dy = -Math.abs(y1 - y0);
  const sx = x0 < x1 ? 1 : -1;
  const sy = y0 < y1 ? 1 : -1;
  let error = dx + dy;
  let x = x0;
  let y = y0;

  for (;;) {
    points.push({ x, y });
    if (x === x1 && y === y1) break;
    const doubled = 2 * error;

    if (doubled >= dy) {
      error += dy;
      x += sx;
    }
    if (doubled <= dx) {
      error += dx;
      y += sy;
    }
  }

  return points;
}

/** 椭圆描边采样点 */
function ellipsePoints(
  cx: number,
  cy: number,
  rx: number,
  ry: number,
): Point[] {
  const points: Point[] = [];
  const steps = Math.max(12, Math.ceil((rx + ry) * 3));

  for (let index = 0; index < steps; index += 1) {
    const angle = (index / steps) * Math.PI * 2;

    points.push({
      x: Math.round(cx + rx * Math.cos(angle)),
      y: Math.round(cy + ry * Math.sin(angle)),
    });
  }

  return points;
}

function normalizeRect(start: Point, end: Point): Rect {
  const x = Math.min(start.x, end.x);
  const y = Math.min(start.y, end.y);

  return {
    x,
    y,
    w: Math.abs(end.x - start.x) + 1,
    h: Math.abs(end.y - start.y) + 1,
  };
}

function extractRegion(
  source: HTMLCanvasElement,
  rect: Rect,
): HTMLCanvasElement {
  const canvas = makeCanvas(rect.w, rect.h);

  canvas
    .getContext("2d")
    ?.drawImage(source, rect.x, rect.y, rect.w, rect.h, 0, 0, rect.w, rect.h);

  return canvas;
}

const TextureEditor: React.FC<TextureEditorProps> = ({
  file,
  history,
  color,
  tool,
  recentColors,
  onColorChange,
  onToolChange,
  onContentChange,
}) => {
  const tex = file.canvas;
  const viewRef = useRef<HTMLCanvasElement>(null);
  const drawingRef = useRef(false);
  const shapeRef = useRef<Shape | null>(null);
  const selectStartRef = useRef<Point | null>(null);
  const movingRef = useRef<{
    canvas: HTMLCanvasElement;
    x: number;
    y: number;
    pointerX: number;
    pointerY: number;
  } | null>(null);

  const [brush, setBrush] = useState(1);
  const [opacity, setOpacity] = useState(100);
  const [showGrid, setShowGrid] = useState(true);
  const [zoom, setZoom] = useState(() =>
    tex ? Math.max(2, Math.min(16, Math.floor(384 / tex.width))) : 8,
  );
  const [selection, setSelection] = useState<Rect | null>(null);
  const [, setHistoryStamp] = useState(0);

  const resolution = tex ? `${tex.width}×${tex.height}` : "";

  // 缓存 selectedKeys 数组，避免每次渲染都创建新数组导致 Select 闪烁
  const brushKeys = useMemo(() => [String(brush)], [brush]);
  const texWidth = tex?.width;
  const textureSizeKeys = useMemo(
    () => (texWidth ? [String(texWidth)] : []),
    [texWidth],
  );

  const paintTexel = useCallback(
    (ctx: CanvasRenderingContext2D, x: number, y: number, erase: boolean) => {
      if (!tex || x < 0 || y < 0 || x >= tex.width || y >= tex.height) return;
      if (erase) {
        ctx.clearRect(x, y, 1, 1);

        return;
      }
      ctx.fillStyle = color;
      ctx.fillRect(x, y, 1, 1);
    },
    [color, tex],
  );

  /** 对一组 texel 应用当前笔刷尺寸 */
  const stamp = useCallback(
    (
      ctx: CanvasRenderingContext2D,
      point: Point,
      erase: boolean,
      size: number,
    ) => {
      const offset = Math.floor((size - 1) / 2);

      for (let dy = 0; dy < size; dy += 1) {
        for (let dx = 0; dx < size; dx += 1) {
          paintTexel(ctx, point.x + dx - offset, point.y + dy - offset, erase);
        }
      }
    },
    [paintTexel],
  );

  const forEachShapeTexel = useCallback(
    (shape: Shape, size: number, callback: (x: number, y: number) => void) => {
      const { start, current } = shape;

      if (shape.type === "rectFill") {
        const rect = normalizeRect(start, current);

        for (let y = rect.y; y < rect.y + rect.h; y += 1) {
          for (let x = rect.x; x < rect.x + rect.w; x += 1) callback(x, y);
        }

        return;
      }

      let points: Point[];

      if (shape.type === "line") {
        points = linePoints(start.x, start.y, current.x, current.y);
      } else if (shape.type === "rect") {
        const rect = normalizeRect(start, current);
        const right = rect.x + rect.w - 1;
        const bottom = rect.y + rect.h - 1;

        points = [
          ...linePoints(rect.x, rect.y, right, rect.y),
          ...linePoints(right, rect.y, right, bottom),
          ...linePoints(right, bottom, rect.x, bottom),
          ...linePoints(rect.x, bottom, rect.x, rect.y),
        ];
      } else {
        const cx = (start.x + current.x) / 2;
        const cy = (start.y + current.y) / 2;

        points = ellipsePoints(
          cx,
          cy,
          Math.abs(current.x - start.x) / 2,
          Math.abs(current.y - start.y) / 2,
        );
      }

      const offset = Math.floor((size - 1) / 2);

      for (const point of points) {
        for (let dy = 0; dy < size; dy += 1) {
          for (let dx = 0; dx < size; dx += 1) {
            callback(point.x + dx - offset, point.y + dy - offset);
          }
        }
      }
    },
    [],
  );

  const redraw = useCallback(() => {
    const view = viewRef.current;

    if (!view || !tex) return;
    const scale = Math.max(1, zoom);

    if (
      view.width !== tex.width * scale ||
      view.height !== tex.height * scale
    ) {
      view.width = tex.width * scale;
      view.height = tex.height * scale;
    }
    const ctx = view.getContext("2d");

    if (!ctx) return;
    ctx.globalAlpha = 1;
    for (let y = 0; y < tex.height; y += 1) {
      for (let x = 0; x < tex.width; x += 1) {
        ctx.fillStyle = (x + y) % 2 === 0 ? "#e5e7eb" : "#f3f4f6";
        ctx.fillRect(x * scale, y * scale, scale, scale);
      }
    }
    ctx.imageSmoothingEnabled = false;
    ctx.drawImage(tex, 0, 0, view.width, view.height);

    const moving = movingRef.current;

    if (moving) {
      ctx.drawImage(
        moving.canvas,
        moving.x * scale,
        moving.y * scale,
        moving.canvas.width * scale,
        moving.canvas.height * scale,
      );
    }

    const shape = shapeRef.current;

    if (shape) {
      ctx.globalAlpha = opacity / 100;
      ctx.fillStyle = color;
      forEachShapeTexel(shape, brush, (x, y) => {
        if (x < 0 || y < 0 || x >= tex.width || y >= tex.height) return;
        ctx.fillRect(x * scale, y * scale, scale, scale);
      });
      ctx.globalAlpha = 1;
    }

    if (selection) {
      ctx.setLineDash([4, 4]);
      ctx.lineWidth = 1;
      ctx.strokeStyle = "#000000";
      ctx.strokeRect(
        selection.x * scale + 0.5,
        selection.y * scale + 0.5,
        selection.w * scale - 1,
        selection.h * scale - 1,
      );
      ctx.strokeStyle = "#ffffff";
      ctx.lineDashOffset = 4;
      ctx.strokeRect(
        selection.x * scale + 0.5,
        selection.y * scale + 0.5,
        selection.w * scale - 1,
        selection.h * scale - 1,
      );
      ctx.setLineDash([]);
      ctx.lineDashOffset = 0;
    }

    if (showGrid && scale >= 6) {
      ctx.strokeStyle = "rgba(0,0,0,0.10)";
      ctx.lineWidth = 1;
      ctx.beginPath();
      for (let x = 1; x < tex.width; x += 1) {
        ctx.moveTo(x * scale + 0.5, 0);
        ctx.lineTo(x * scale + 0.5, view.height);
      }
      for (let y = 1; y < tex.height; y += 1) {
        ctx.moveTo(0, y * scale + 0.5);
        ctx.lineTo(view.width, y * scale + 0.5);
      }
      ctx.stroke();
    }
  }, [
    brush,
    color,
    forEachShapeTexel,
    opacity,
    selection,
    showGrid,
    tex,
    zoom,
  ]);

  useEffect(() => {
    redraw();
  }, [redraw]);

  const pushUndo = useCallback(() => {
    if (!tex) return;
    history.undo.push(tex.toDataURL("image/png"));
    if (history.undo.length > 40) history.undo.shift();
    history.redo.length = 0;
    setHistoryStamp((value) => value + 1);
    onContentChange?.();
  }, [history, onContentChange, tex]);

  const undo = useCallback(() => {
    const snapshot = history.undo.pop();

    if (!tex || !snapshot) return;
    history.redo.push(tex.toDataURL("image/png"));
    const image = new Image();

    image.onload = () => {
      const ctx = tex.getContext("2d");

      if (!ctx) return;
      ctx.globalAlpha = 1;
      ctx.clearRect(0, 0, tex.width, tex.height);
      ctx.drawImage(image, 0, 0);
      redraw();
    };
    image.src = snapshot;
    setHistoryStamp((value) => value + 1);
    onContentChange?.();
  }, [history, onContentChange, redraw, tex]);

  const redo = useCallback(() => {
    const snapshot = history.redo.pop();

    if (!tex || !snapshot) return;
    history.undo.push(tex.toDataURL("image/png"));
    const image = new Image();

    image.onload = () => {
      const ctx = tex.getContext("2d");

      if (!ctx) return;
      ctx.globalAlpha = 1;
      ctx.clearRect(0, 0, tex.width, tex.height);
      ctx.drawImage(image, 0, 0);
      redraw();
    };
    image.src = snapshot;
    setHistoryStamp((value) => value + 1);
    onContentChange?.();
  }, [history, onContentChange, redraw, tex]);

  /** 油漆桶：与起点像素颜色完全一致的连通区域填充 */
  const floodFill = useCallback(
    (point: Point) => {
      const ctx = tex?.getContext("2d");

      if (!tex || !ctx) return;
      const image = ctx.getImageData(0, 0, tex.width, tex.height);
      const data = image.data;
      const startIndex = (point.y * tex.width + point.x) * 4;
      const target = data.slice(startIndex, startIndex + 4);
      const [red, green, blue] = hexToRgb(color);
      const fill = [red, green, blue, Math.round((opacity / 100) * 255)];

      if (
        target[0] === fill[0] &&
        target[1] === fill[1] &&
        target[2] === fill[2] &&
        target[3] === fill[3]
      ) {
        return;
      }
      const match = (index: number) =>
        data[index] === target[0] &&
        data[index + 1] === target[1] &&
        data[index + 2] === target[2] &&
        data[index + 3] === target[3];

      const stack: Point[] = [point];

      while (stack.length > 0) {
        const current = stack.pop() as Point;
        let left = current.x;

        while (left > 0 && match((current.y * tex.width + left - 1) * 4)) {
          left -= 1;
        }
        let right = current.x;

        while (
          right < tex.width - 1 &&
          match((current.y * tex.width + right + 1) * 4)
        ) {
          right += 1;
        }
        for (let x = left; x <= right; x += 1) {
          const index = (current.y * tex.width + x) * 4;

          data[index] = fill[0];
          data[index + 1] = fill[1];
          data[index + 2] = fill[2];
          data[index + 3] = fill[3];
          if (current.y > 0 && match(((current.y - 1) * tex.width + x) * 4)) {
            stack.push({ x, y: current.y - 1 });
          }
          if (
            current.y < tex.height - 1 &&
            match(((current.y + 1) * tex.width + x) * 4)
          ) {
            stack.push({ x, y: current.y + 1 });
          }
        }
      }
      ctx.putImageData(image, 0, 0);
    },
    [color, opacity, tex],
  );

  const commitShape = useCallback(
    (shape: Shape) => {
      const ctx = tex?.getContext("2d");

      if (!tex || !ctx) return;
      pushUndo();
      ctx.globalAlpha = opacity / 100;
      forEachShapeTexel(shape, brush, (x, y) => paintTexel(ctx, x, y, false));
      ctx.globalAlpha = 1;
    },
    [brush, forEachShapeTexel, opacity, paintTexel, pushUndo, tex],
  );

  const pointerToTex = (
    event: React.PointerEvent<HTMLCanvasElement>,
  ): Point | null => {
    const view = viewRef.current;

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

  const pointInRect = (point: Point, rect: Rect) =>
    point.x >= rect.x &&
    point.y >= rect.y &&
    point.x < rect.x + rect.w &&
    point.y < rect.y + rect.h;

  const handlePointerDown = (event: React.PointerEvent<HTMLCanvasElement>) => {
    if (!tex) return;
    const point = pointerToTex(event);

    if (!point) return;
    event.currentTarget.setPointerCapture(event.pointerId);
    drawingRef.current = true;

    if (tool === "eyedropper") {
      const pixel = tex
        .getContext("2d")
        ?.getImageData(point.x, point.y, 1, 1).data;

      if (pixel && pixel[3] !== 0) {
        onColorChange(
          `#${[pixel[0], pixel[1], pixel[2]]
            .map((value) => value.toString(16).padStart(2, "0"))
            .join("")}`,
        );
      }
      onToolChange("pencil");
      drawingRef.current = false;

      return;
    }

    if (tool === "fill") {
      pushUndo();
      floodFill(point);
      redraw();
      drawingRef.current = false;

      return;
    }

    if (tool === "select") {
      if (selection && pointInRect(point, selection)) {
        pushUndo();
        movingRef.current = {
          canvas: extractRegion(tex, selection),
          x: selection.x,
          y: selection.y,
          pointerX: point.x,
          pointerY: point.y,
        };
        const ctx = tex.getContext("2d");

        ctx?.clearRect(selection.x, selection.y, selection.w, selection.h);
        setSelection(null);
        redraw();
      } else {
        selectStartRef.current = point;
        setSelection(null);
      }

      return;
    }

    if (tool === "pencil" || tool === "eraser") {
      pushUndo();
      const ctx = tex.getContext("2d");

      if (ctx) {
        ctx.globalAlpha = opacity / 100;
        stamp(ctx, point, tool === "eraser", brush);
        ctx.globalAlpha = 1;
      }
      redraw();

      return;
    }

    shapeRef.current = { type: tool, start: point, current: point };
    redraw();
  };

  const handlePointerMove = (event: React.PointerEvent<HTMLCanvasElement>) => {
    if (!drawingRef.current || !tex) return;
    const point = pointerToTex(event);

    if (!point) return;

    if (tool === "pencil" || tool === "eraser") {
      const ctx = tex.getContext("2d");

      if (ctx) {
        ctx.globalAlpha = opacity / 100;
        stamp(ctx, point, tool === "eraser", brush);
        ctx.globalAlpha = 1;
      }
      redraw();

      return;
    }

    if (tool === "select") {
      const moving = movingRef.current;

      if (moving) {
        moving.x = Math.max(
          0,
          Math.min(
            tex.width - moving.canvas.width,
            moving.x + (point.x - moving.pointerX),
          ),
        );
        moving.y = Math.max(
          0,
          Math.min(
            tex.height - moving.canvas.height,
            moving.y + (point.y - moving.pointerY),
          ),
        );
        moving.pointerX = point.x;
        moving.pointerY = point.y;
        redraw();
      } else if (selectStartRef.current) {
        setSelection(normalizeRect(selectStartRef.current, point));
      }

      return;
    }

    if (shapeRef.current) {
      shapeRef.current.current = point;
      redraw();
    }
  };

  const handlePointerUp = () => {
    if (!tex) return;
    let mutated = false;

    if (shapeRef.current) {
      commitShape(shapeRef.current);
      shapeRef.current = null;
      redraw();
      mutated = true;
    }

    const moving = movingRef.current;

    if (moving) {
      const ctx = tex.getContext("2d");

      ctx?.drawImage(moving.canvas, moving.x, moving.y);
      setSelection({
        x: moving.x,
        y: moving.y,
        w: moving.canvas.width,
        h: moving.canvas.height,
      });
      movingRef.current = null;
      redraw();
      mutated = true;
    }

    selectStartRef.current = null;
    drawingRef.current = false;
    if (mutated) onContentChange?.();
  };

  const handleKeyDown = useCallback(
    (event: KeyboardEvent) => {
      if (!selection || !tex) return;
      const target = event.target as HTMLElement | null;
      const tag = target?.tagName;

      if (
        tag === "INPUT" ||
        tag === "TEXTAREA" ||
        tag === "SELECT" ||
        target?.isContentEditable
      ) {
        return;
      }
      const step = event.shiftKey ? 4 : 1;
      const moves: Record<string, [number, number]> = {
        ArrowLeft: [-step, 0],
        ArrowRight: [step, 0],
        ArrowUp: [0, -step],
        ArrowDown: [0, step],
      };

      if (event.key === "Delete" || event.key === "Backspace") {
        event.preventDefault();
        pushUndo();
        tex
          .getContext("2d")
          ?.clearRect(selection.x, selection.y, selection.w, selection.h);
        redraw();

        return;
      }

      const move = moves[event.key];

      if (!move) return;
      event.preventDefault();
      const [dx, dy] = move;
      const ctx = tex.getContext("2d");

      if (!ctx) return;
      pushUndo();
      const cropped = extractRegion(tex, selection);
      const nextX = Math.max(
        0,
        Math.min(tex.width - selection.w, selection.x + dx),
      );
      const nextY = Math.max(
        0,
        Math.min(tex.height - selection.h, selection.y + dy),
      );

      ctx.clearRect(selection.x, selection.y, selection.w, selection.h);
      ctx.drawImage(cropped, nextX, nextY);
      setSelection({ ...selection, x: nextX, y: nextY });
      redraw();
    },
    [pushUndo, redraw, selection, tex],
  );

  useEffect(() => {
    window.addEventListener("keydown", handleKeyDown);

    return () => window.removeEventListener("keydown", handleKeyDown);
  }, [handleKeyDown]);

  const fillSelection = () => {
    if (!selection || !tex) return;
    pushUndo();
    const ctx = tex.getContext("2d");

    if (ctx) {
      ctx.globalAlpha = opacity / 100;
      ctx.fillStyle = color;
      ctx.fillRect(selection.x, selection.y, selection.w, selection.h);
      ctx.globalAlpha = 1;
    }
    redraw();
  };

  const flipSelection = (axis: "h" | "v") => {
    if (!selection || !tex) return;
    pushUndo();
    const cropped = extractRegion(tex, selection);
    const ctx = tex.getContext("2d");

    if (!ctx) return;
    ctx.clearRect(selection.x, selection.y, selection.w, selection.h);
    ctx.save();
    ctx.translate(
      axis === "h" ? selection.x + selection.w : selection.x,
      axis === "v" ? selection.y + selection.h : selection.y,
    );
    ctx.scale(axis === "h" ? -1 : 1, axis === "v" ? -1 : 1);
    ctx.drawImage(cropped, 0, 0);
    ctx.restore();
    redraw();
  };

  const deleteSelection = () => {
    if (!selection || !tex) return;
    pushUndo();
    tex
      .getContext("2d")
      ?.clearRect(selection.x, selection.y, selection.w, selection.h);
    redraw();
  };

  const flipTexture = (axis: "h" | "v") => {
    if (!tex) return;
    pushUndo();
    const copy = makeCanvas(tex.width, tex.height);
    const copyCtx = copy.getContext("2d");

    if (!copyCtx) return;
    copyCtx.drawImage(tex, 0, 0);
    const ctx = tex.getContext("2d");

    if (!ctx) return;
    ctx.globalAlpha = 1;
    ctx.clearRect(0, 0, tex.width, tex.height);
    ctx.save();
    ctx.translate(axis === "h" ? tex.width : 0, axis === "v" ? tex.height : 0);
    ctx.scale(axis === "h" ? -1 : 1, axis === "v" ? -1 : 1);
    ctx.drawImage(copy, 0, 0);
    ctx.restore();
    redraw();
  };

  const resizeTexture = (size: number) => {
    if (!tex || size === tex.width) return;
    pushUndo();
    const copy = makeCanvas(tex.width, tex.height);
    const copyCtx = copy.getContext("2d");

    copyCtx?.drawImage(tex, 0, 0);
    tex.width = size;
    tex.height = size;
    const ctx = tex.getContext("2d");

    if (ctx) {
      ctx.globalAlpha = 1;
      ctx.clearRect(0, 0, size, size);
      ctx.imageSmoothingEnabled = false;
      ctx.drawImage(copy, 0, 0);
    }
    setSelection(null);
    setZoom(Math.max(2, Math.min(16, Math.floor(384 / size))));
    redraw();
  };

  const clearTexture = () => {
    if (!tex) return;
    pushUndo();
    tex.getContext("2d")?.clearRect(0, 0, tex.width, tex.height);
    setSelection(null);
    redraw();
  };

  const toolButton = (
    value: TextureTool,
    label: string,
    icon: React.ReactNode,
  ) => (
    <Button
      isIconOnly
      aria-label={label}
      className={tool === value ? "bg-primary/15 text-primary" : ""}
      size="sm"
      title={label}
      variant={tool === value ? "flat" : "light"}
      onPress={() => onToolChange(value)}
    >
      {icon}
    </Button>
  );

  if (!tex) {
    return (
      <div className="flex flex-1 items-center justify-center text-sm text-gray-400">
        {t("该文件没有可绘制的图像数据")}
      </div>
    );
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      {/* 工具行 */}
      <div className="flex flex-wrap items-center gap-1.5 px-3 pt-3">
        {toolButton("pencil", t("画笔"), <PaintBrush20Regular />)}
        {toolButton("eraser", t("橡皮"), <Eraser20Regular />)}
        {toolButton("fill", t("油漆桶"), <PaintBucket20Regular />)}
        {toolButton("eyedropper", t("吸管"), <Color20Regular />)}
        {toolButton("line", t("直线"), <LineHorizontal120Regular />)}
        {toolButton("rect", t("矩形描边"), <RectangleLandscape20Regular />)}
        {toolButton("rectFill", t("矩形填充"), <RectangleLandscape20Regular />)}
        {toolButton("ellipse", t("椭圆描边"), <Oval20Regular />)}
        {toolButton("select", t("选区"), <SelectObject20Regular />)}
        <div className="mx-1 h-5 w-px bg-default-200" />
        <Button
          isIconOnly
          aria-label={t("撤销")}
          isDisabled={history.undo.length === 0}
          size="sm"
          title={t("撤销")}
          variant="light"
          onPress={undo}
        >
          <ArrowUndo20Regular />
        </Button>
        <Button
          isIconOnly
          aria-label={t("重做")}
          isDisabled={history.redo.length === 0}
          size="sm"
          title={t("重做")}
          variant="light"
          onPress={redo}
        >
          <ArrowRedo20Regular />
        </Button>
        <div className="mx-1 h-5 w-px bg-default-200" />
        <Button
          isIconOnly
          aria-label={t("缩小")}
          size="sm"
          title={t("缩小")}
          variant="light"
          onPress={() => setZoom((value) => Math.max(1, value - 2))}
        >
          <Subtract20Regular />
        </Button>
        <span className="min-w-10 text-center text-[11px] text-gray-400">
          {zoom}×
        </span>
        <Button
          isIconOnly
          aria-label={t("放大")}
          size="sm"
          title={t("放大")}
          variant="light"
          onPress={() => setZoom((value) => Math.min(32, value + 2))}
        >
          <Add20Regular />
        </Button>
        <Button
          isIconOnly
          aria-label={t("网格")}
          className={showGrid ? "bg-primary/15 text-primary" : ""}
          size="sm"
          title={t("网格")}
          variant={showGrid ? "flat" : "light"}
          onPress={() => setShowGrid((value) => !value)}
        >
          <Grid20Regular />
        </Button>
        <div className="mx-1 h-5 w-px bg-default-200" />
        <Button
          isIconOnly
          aria-label={t("水平镜像")}
          size="sm"
          title={t("整图水平镜像")}
          variant="light"
          onPress={() => flipTexture("h")}
        >
          <FlipHorizontal20Regular />
        </Button>
        <Button
          isIconOnly
          aria-label={t("垂直镜像")}
          size="sm"
          title={t("整图垂直镜像")}
          variant="light"
          onPress={() => flipTexture("v")}
        >
          <FlipVertical20Regular />
        </Button>
        <Button
          isIconOnly
          aria-label={t("清空")}
          size="sm"
          title={t("清空画布")}
          variant="light"
          onPress={clearTexture}
        >
          <Broom20Regular />
        </Button>
        <span className="flex-none pl-1 text-[11px] text-gray-400">
          {resolution}
        </span>
      </div>

      {/* 参数行：笔刷 / 不透明度 / 尺寸 */}
      <div className="flex flex-wrap items-center gap-3 px-3 pt-2 text-[11px] text-gray-500 dark:text-gray-400">
        <span className="flex items-center gap-1.5">
          {t("笔刷")}
          <Select
            aria-label={t("笔刷大小")}
            className="w-16 [&_*]:min-w-0"
            classNames={{ trigger: "h-7 min-h-7" }}
            popoverProps={selectPopoverProps}
            selectedKeys={brushKeys}
            size="sm"
            onSelectionChange={(keys) =>
              setBrush(Number(Array.from(keys)[0] ?? 1))
            }
          >
            {BRUSH_SIZES.map((size) => (
              <SelectItem key={String(size)}>{`${size}px`}</SelectItem>
            ))}
          </Select>
        </span>
        <span className="flex items-center gap-1.5">
          {t("不透明度")}
          <input
            aria-label={t("画笔不透明度")}
            className="h-1.5 w-24 cursor-pointer accent-primary"
            max={100}
            min={10}
            step={5}
            type="range"
            value={opacity}
            onChange={(event) => setOpacity(Number(event.target.value))}
          />
          <span className="w-8 text-right">{opacity}%</span>
        </span>
        <span className="flex items-center gap-1.5">
          {t("尺寸")}
          <Select
            aria-label={t("贴图尺寸")}
            className="w-20 [&_*]:min-w-0"
            classNames={{ trigger: "h-7 min-h-7" }}
            popoverProps={selectPopoverProps}
            selectedKeys={textureSizeKeys}
            size="sm"
            onSelectionChange={(keys) =>
              resizeTexture(Number(Array.from(keys)[0] ?? tex.width))
            }
          >
            {TEXTURE_SIZE_OPTIONS.map((size) => (
              <SelectItem key={String(size)}>{`${size}²`}</SelectItem>
            ))}
          </Select>
        </span>
      </div>

      {/* 调色板 */}
      <div className="flex flex-wrap items-center gap-1 px-3 pt-2">
        {recentColors.length > 0 ? (
          <>
            {recentColors.slice(0, 6).map((recent, index) => (
              <button
                key={`recent-${recent}-${index}`}
                aria-label={t("最近颜色 {0}", { "0": recent })}
                className="size-5 cursor-pointer rounded-md border border-dashed border-default-400"
                style={{ backgroundColor: recent }}
                title={t("最近使用")}
                type="button"
                onClick={() => onColorChange(recent)}
              />
            ))}
            <span className="mx-0.5 h-5 w-px bg-default-200" />
          </>
        ) : null}
        {PALETTE.map((swatch) => (
          <button
            key={swatch}
            aria-label={t("选择颜色 {0}", { "0": swatch })}
            className={`size-5 cursor-pointer rounded-md border transition-transform hover:scale-110 ${
              color === swatch
                ? "border-primary ring-2 ring-primary/40"
                : "border-default-300 dark:border-gray-600"
            }`}
            style={{ backgroundColor: swatch }}
            type="button"
            onClick={() => onColorChange(swatch)}
          />
        ))}
        <input
          aria-label={t("自定义颜色")}
          className="ml-1 h-6 w-9 cursor-pointer rounded border border-default-300 bg-transparent dark:border-gray-600"
          type="color"
          value={color}
          onChange={(event) => onColorChange(event.target.value)}
        />
        <span className="ml-1 font-mono text-[11px] text-gray-400">
          {color}
        </span>
      </div>

      {/* 选区操作条 */}
      {selection ? (
        <div className="mx-3 mt-2 flex flex-wrap items-center gap-1.5 rounded-lg bg-primary/5 px-2 py-1.5 text-[11px] text-gray-500 dark:text-gray-400">
          <span className="mr-1">
            {t("选区")} {selection.w}×{selection.h} @ {selection.x},
            {selection.y}
          </span>
          <Button size="sm" variant="flat" onPress={fillSelection}>
            {t("填充")}
          </Button>
          <Button
            isIconOnly
            aria-label={t("选区水平翻转")}
            size="sm"
            title={t("选区水平翻转")}
            variant="light"
            onPress={() => flipSelection("h")}
          >
            <FlipHorizontal20Regular />
          </Button>
          <Button
            isIconOnly
            aria-label={t("选区垂直翻转")}
            size="sm"
            title={t("选区垂直翻转")}
            variant="light"
            onPress={() => flipSelection("v")}
          >
            <FlipVertical20Regular />
          </Button>
          <Button size="sm" variant="light" onPress={deleteSelection}>
            {t("删除")}
          </Button>
          <Button size="sm" variant="light" onPress={() => setSelection(null)}>
            {t("取消")}
          </Button>
          <span className="text-gray-400">
            {t("拖动选区可移动，方向键微移（Shift 加速）")}
          </span>
        </div>
      ) : null}

      {/* 画布区 */}
      <div className="flex min-h-0 flex-1 items-center justify-center overflow-auto p-3 outline-none">
        <canvas
          ref={viewRef}
          className="max-w-full cursor-crosshair shadow-sm [image-rendering:pixelated]"
          onPointerCancel={handlePointerUp}
          onPointerDown={handlePointerDown}
          onPointerMove={handlePointerMove}
          onPointerUp={handlePointerUp}
        />
      </div>
    </div>
  );
};

export default TextureEditor;
