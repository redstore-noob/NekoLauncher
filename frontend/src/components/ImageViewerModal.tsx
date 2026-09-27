/*
 * Copyright 2024 Next UI
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
import React, { useCallback, useEffect, useRef, useState } from "react";
import { Modal, ModalContent, Spinner } from "@heroui/react";
import {
  ArrowExportLtr20Regular,
  Dismiss20Regular,
  Image20Regular,
  ZoomIn20Regular,
  ZoomOut20Regular,
} from "@fluentui/react-icons";

import { SaveFileAs } from "../../wailsjs/go/bindings/SystemAPI";
import { errorMessage } from "../lib/home";
import { t } from "../i18n";

import { modalBehaviorProps } from "./modal-shell";

/** 缩放上下限（1 = 图片原始尺寸） */
const MIN_ZOOM = 0.1;
const MAX_ZOOM = 12;
/** 按钮缩放步进 */
const ZOOM_STEP = 1.25;

interface Offset {
  x: number;
  y: number;
}

interface ImageViewerModalProps {
  isOpen: boolean;
  onClose: () => void;
  /** 展示用 URL（本地文件请传 /localfile 中转地址） */
  src: string;
  /** 文件名：标题与另存为默认名 */
  fileName: string;
  /** 本地绝对路径（另存为的源文件；缺省隐藏另存为按钮） */
  sourcePath?: string;
}

/** clamp 视口内的平移量：图片比视口大时限制在边界内，小时锁回居中 */
function clampOffset(
  offset: Offset,
  zoom: number,
  natural: Offset,
  viewport: Offset,
): Offset {
  const maxX = Math.max(0, (natural.x * zoom - viewport.x) / 2);
  const maxY = Math.max(0, (natural.y * zoom - viewport.y) / 2);

  return {
    x: Math.min(maxX, Math.max(-maxX, offset.x)),
    y: Math.min(maxY, Math.max(-maxY, offset.y)),
  };
}

function clampZoom(zoom: number): number {
  return Math.min(MAX_ZOOM, Math.max(MIN_ZOOM, zoom));
}

/**
 * 应用内图片查看器：深色全屏弹层，支持滚轮/按钮缩放（以光标为中心）、
 * 拖拽平移、双击在适配与 2× 间切换、另存为（走系统对话框复制原文件）。
 * 遮罩行为与业务弹窗统一（modalBehaviorProps：不可点外关闭、跳过退出动画）。
 */
const ImageViewerModal: React.FC<ImageViewerModalProps> = ({
  isOpen,
  onClose,
  src,
  fileName,
  sourcePath,
}) => {
  const viewportRef = useRef<HTMLDivElement>(null);
  const dragRef = useRef<{
    pointerId: number;
    startClient: Offset;
    baseOffset: Offset;
  } | null>(null);

  const [viewport, setViewport] = useState<Offset>({ x: 0, y: 0 });
  const [natural, setNatural] = useState<Offset>({ x: 0, y: 0 });
  const [fitZoom, setFitZoom] = useState(1);
  const [zoom, setZoom] = useState(1);
  const [offset, setOffset] = useState<Offset>({ x: 0, y: 0 });
  const [dragging, setDragging] = useState(false);
  const [saving, setSaving] = useState(false);
  const [status, setStatus] = useState<{ ok: boolean; text: string } | null>(
    null,
  );

  // 打开/换图：归位 + 清状态
  useEffect(() => {
    if (!isOpen) return;
    setNatural({ x: 0, y: 0 });
    setFitZoom(1);
    setZoom(1);
    setOffset({ x: 0, y: 0 });
    setStatus(null);
    setSaving(false);
  }, [isOpen, src]);

  // 视口尺寸跟随（弹层尺寸固定后只需少量重算）
  useEffect(() => {
    if (!isOpen) return;
    const element = viewportRef.current;

    if (!element) return;
    const observer = new ResizeObserver((entries) => {
      const rect = entries[0]?.contentRect;

      if (rect && rect.width > 0 && rect.height > 0) {
        setViewport({ x: rect.width, y: rect.height });
      }
    });

    observer.observe(element);

    return () => observer.disconnect();
  }, [isOpen]);

  // 知道图片尺寸后计算适配缩放并归位
  useEffect(() => {
    if (natural.x <= 0 || natural.y <= 0 || viewport.x <= 0 || viewport.y <= 0)
      return;
    const fit = Math.min(viewport.x / natural.x, viewport.y / natural.y, 1);

    setFitZoom(fit);
    setZoom(fit);
    setOffset({ x: 0, y: 0 });
  }, [natural, viewport]);

  // Esc 关闭（modalBehaviorProps 关闭了弹层自带的 Esc 行为）
  useEffect(() => {
    if (!isOpen) return;
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") onClose();
    };

    window.addEventListener("keydown", onKeyDown);

    return () => window.removeEventListener("keydown", onKeyDown);
  }, [isOpen, onClose]);

  // 滚轮缩放：以光标为不动点（non-passive，阻止页面滚动）
  useEffect(() => {
    if (!isOpen) return;
    const element = viewportRef.current;

    if (!element) return;
    const onWheel = (event: WheelEvent) => {
      event.preventDefault();
      const rect = element.getBoundingClientRect();
      const anchor = {
        x: event.clientX - rect.left - rect.width / 2,
        y: event.clientY - rect.top - rect.height / 2,
      };

      setZoom((prevZoom) => {
        const nextZoom = clampZoom(
          prevZoom * (event.deltaY < 0 ? ZOOM_STEP : 1 / ZOOM_STEP),
        );

        if (nextZoom === prevZoom) return prevZoom;
        setOffset((prevOffset) =>
          clampOffset(
            {
              x: anchor.x - ((anchor.x - prevOffset.x) * nextZoom) / prevZoom,
              y: anchor.y - ((anchor.y - prevOffset.y) * nextZoom) / prevZoom,
            },
            nextZoom,
            natural,
            viewport,
          ),
        );

        return nextZoom;
      });
    };

    element.addEventListener("wheel", onWheel, { passive: false });

    return () => element.removeEventListener("wheel", onWheel);
  }, [isOpen, natural, viewport]);

  const zoomBy = useCallback((factor: number) => {
    // 按钮缩放保持视口中心不动；边界 clamp 留给下一次拖拽/滚轮
    setZoom((prev) => clampZoom(prev * factor));
  }, []);

  const resetView = useCallback(() => {
    setZoom(fitZoom);
    setOffset({ x: 0, y: 0 });
  }, [fitZoom]);

  const toggleZoom = useCallback(
    (anchor: Offset) => {
      setZoom((prevZoom) => {
        const target =
          prevZoom > fitZoom * 1.9 ? fitZoom : clampZoom(fitZoom * 2);

        if (Math.abs(target - prevZoom) < 1e-6) return prevZoom;
        setOffset((prevOffset) =>
          clampOffset(
            {
              x: anchor.x - ((anchor.x - prevOffset.x) * target) / prevZoom,
              y: anchor.y - ((anchor.y - prevOffset.y) * target) / prevZoom,
            },
            target,
            natural,
            viewport,
          ),
        );

        return target;
      });
    },
    [fitZoom, natural, viewport],
  );

  // 拖拽平移（pointer capture：移出视口也持续跟踪）
  const onPointerDown = (event: React.PointerEvent) => {
    if (event.button !== 0) return;
    event.preventDefault();
    dragRef.current = {
      pointerId: event.pointerId,
      startClient: { x: event.clientX, y: event.clientY },
      baseOffset: offset,
    };
    setDragging(true);
    event.currentTarget.setPointerCapture(event.pointerId);
  };

  const onPointerMove = (event: React.PointerEvent) => {
    const drag = dragRef.current;

    if (!drag || drag.pointerId !== event.pointerId) return;
    const next = clampOffset(
      {
        x: drag.baseOffset.x + (event.clientX - drag.startClient.x),
        y: drag.baseOffset.y + (event.clientY - drag.startClient.y),
      },
      zoom,
      natural,
      viewport,
    );

    setOffset(next);
  };

  const endDrag = (event: React.PointerEvent) => {
    const drag = dragRef.current;

    if (!drag || drag.pointerId !== event.pointerId) return;
    dragRef.current = null;
    setDragging(false);
  };

  // 另存为：系统对话框 + 复制原文件
  const saveAs = async () => {
    if (!sourcePath || saving) return;
    setSaving(true);
    setStatus(null);
    try {
      const destination = await SaveFileAs(
        sourcePath,
        fileName,
        t("图片"),
        "*.png;*.jpg;*.jpeg;*.webp;*.bmp;*.gif",
      );

      if (destination) {
        setStatus({ ok: true, text: t("已保存到 {0}", { "0": destination }) });
      }
    } catch (ex) {
      setStatus({
        ok: false,
        text: t("保存失败：{0}", { "0": errorMessage(ex) }),
      });
    } finally {
      setSaving(false);
    }
  };

  // 状态提示自动消失
  useEffect(() => {
    if (!status) return;
    const timer = window.setTimeout(() => setStatus(null), 6000);

    return () => window.clearTimeout(timer);
  }, [status]);

  const handleImageLoad = (event: React.SyntheticEvent<HTMLImageElement>) => {
    setNatural({
      x: event.currentTarget.naturalWidth,
      y: event.currentTarget.naturalHeight,
    });
  };

  const viewportPointFromEvent = (event: React.MouseEvent): Offset => {
    const rect = viewportRef.current?.getBoundingClientRect();

    if (!rect) return { x: 0, y: 0 };

    return {
      x: event.clientX - rect.left - rect.width / 2,
      y: event.clientY - rect.top - rect.height / 2,
    };
  };

  return (
    <Modal
      isOpen={isOpen}
      size="full"
      onClose={onClose}
      {...modalBehaviorProps}
      classNames={{
        // 覆盖会整体替换 modalBehaviorProps 的 classNames，这里手动合并：
        // 保留遮罩淡入/入场动画钩子，面板沿用图片查看器自己的深色玻璃底
        backdrop: "nya-modal-backdrop",
        base: "nya-modal-enter border nya-border bg-black/85 text-gray-100 backdrop-blur-xl",
      }}
    >
      <ModalContent>
        <div className="flex h-full flex-col overflow-hidden">
          {/* 标题行：文件名 + 缩放读数 + 操作 */}
          <div className="flex flex-none items-center gap-2 px-4 py-3">
            <span className="flex size-9 flex-none items-center justify-center rounded-xl bg-white/10 text-gray-300">
              <Image20Regular />
            </span>
            <span className="min-w-0 flex-1 overflow-hidden text-sm font-semibold text-ellipsis whitespace-nowrap">
              {fileName}
            </span>
            <span className="flex-none rounded-full bg-white/10 px-2.5 py-0.5 text-[11px] font-medium tabular-nums text-gray-300">
              {Math.round(zoom * 100)}%
            </span>
            <button
              aria-label={t("缩小")}
              className="flex size-8 flex-none cursor-pointer items-center justify-center rounded-lg text-gray-300 transition-colors hover:bg-white/10 hover:text-white"
              title={t("缩小")}
              type="button"
              onClick={() => zoomBy(1 / ZOOM_STEP)}
            >
              <ZoomOut20Regular />
            </button>
            <button
              aria-label={t("放大")}
              className="flex size-8 flex-none cursor-pointer items-center justify-center rounded-lg text-gray-300 transition-colors hover:bg-white/10 hover:text-white"
              title={t("放大")}
              type="button"
              onClick={() => zoomBy(ZOOM_STEP)}
            >
              <ZoomIn20Regular />
            </button>
            <button
              className="flex-none cursor-pointer rounded-lg px-2.5 py-1.5 text-[11px] text-gray-300 transition-colors hover:bg-white/10 hover:text-white"
              type="button"
              onClick={resetView}
            >
              {t("适应窗口")}
            </button>
            {sourcePath ? (
              <button
                className="flex flex-none cursor-pointer items-center gap-1.5 rounded-lg px-2.5 py-1.5 text-[11px] text-gray-300 transition-colors hover:bg-white/10 hover:text-white disabled:cursor-default disabled:opacity-50"
                disabled={saving}
                title={t("另存为")}
                type="button"
                onClick={() => void saveAs()}
              >
                {saving ? (
                  <Spinner className="h-3.5 w-3.5" size="sm" />
                ) : (
                  <ArrowExportLtr20Regular className="h-4 w-4" />
                )}

                {t("另存为")}
              </button>
            ) : null}
            <button
              aria-label={t("关闭")}
              className="flex size-8 flex-none cursor-pointer items-center justify-center rounded-lg text-gray-300 transition-colors hover:bg-white/10 hover:text-white"
              title={t("关闭")}
              type="button"
              onClick={onClose}
            >
              <Dismiss20Regular />
            </button>
          </div>

          {/* 视口：拖拽平移 / 滚轮缩放 / 双击切换 */}
          <div
            ref={viewportRef}
            className={`
              relative min-h-0 flex-1 overflow-hidden bg-black/40
              ${dragging ? "cursor-grabbing" : "cursor-grab"}
            `}
            style={{ touchAction: "none" }}
            onDoubleClick={(event) => {
              event.preventDefault();
              toggleZoom(viewportPointFromEvent(event));
            }}
            onPointerCancel={endDrag}
            onPointerDown={onPointerDown}
            onPointerMove={onPointerMove}
            onPointerUp={endDrag}
          >
            {src ? (
              <img
                alt={fileName}
                className="absolute left-1/2 top-1/2 max-w-none select-none"
                draggable={false}
                src={src}
                style={{
                  transform: `translate(calc(-50% + ${offset.x}px), calc(-50% + ${offset.y}px)) scale(${zoom})`,
                }}
                onLoad={handleImageLoad}
              />
            ) : null}
          </div>

          {/* 保存结果 / 提示行 */}
          {status ? (
            <div
              className={`flex-none px-4 py-2 text-[11px] leading-relaxed ${
                status.ok ? "text-emerald-300" : "text-danger"
              }`}
            >
              {status.text}
            </div>
          ) : (
            <div className="flex-none px-4 py-2 text-[11px] text-gray-500">
              {t("滚轮缩放 · 拖拽平移 · 双击放大 · Esc 关闭")}
            </div>
          )}
        </div>
      </ModalContent>
    </Modal>
  );
};

export default ImageViewerModal;
