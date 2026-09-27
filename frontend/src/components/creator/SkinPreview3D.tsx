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
 * 皮肤编辑器的 3D 预览（独立模块：skinview3d + three 体积大，经 React.lazy
 * 动态加载、独立分包）。除渲染外还承担：
 *  - 3D 直接绘画：绘画模式下对模型 raycast，命中面的 UV 反解为贴图像素，
 *    回调给父组件画到 64x64 贴图画布上（所见即所得）；
 *  - 移动模式：只有点下「移动」才开启轨道控制拖动/旋转人偶；
 *  - 披风与皮肤内外层显示开关。
 */
import React, { useEffect, useRef, useState } from "react";
import * as THREE from "three";

import { t } from "../../i18n";

/** 3D 交互工具（与 2D 画布共用语义；移动模式由 moveMode 单独控制） */
export type PreviewTool = "pencil" | "eraser" | "eyedropper";

interface SkinViewerLike {
  loadSkin(
    source: string,
    options?: { model?: "default" | "slim" },
  ): Promise<void>;
  loadCape(
    source: string,
    options?: { backEquipment?: "cape" | "elytra" },
  ): Promise<void>;
  controls: {
    enableZoom: boolean;
    enableRotate: boolean;
    enablePan: boolean;
  };
  setSize(width: number, height: number): void;
  dispose(): void;
}

interface Props {
  skinUri: string;
  model: SkinModel;
  capeUri: string | null;
  showCape: boolean;
  showInner: boolean;
  showOuter: boolean;
  /** true = 拖动旋转人偶；false = 在模型上绘画 */
  moveMode: boolean;
  tool: PreviewTool;
  /** 绘画回调：命中贴图像素坐标（吸管模式走 onTexelPick） */
  onTexelPaint?: (x: number, y: number) => void;
  onTexelPick?: (x: number, y: number) => void;
  /** 一笔结束（指针抬起），父组件借此刷新贴图/3D 材质 */
  onStrokeEnd?: () => void;
}

type SkinModel = "classic" | "slim";

const SkinPreview3D: React.FC<Props> = ({
  skinUri,
  model,
  capeUri,
  showCape,
  showInner,
  showOuter,
  moveMode,
  tool,
  onTexelPaint,
  onTexelPick,
  onStrokeEnd,
}) => {
  const containerRef = useRef<HTMLDivElement>(null);
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const viewerRef = useRef<ViewerHandle | null>(null);
  const paintingRef = useRef(false);
  const [failed, setFailed] = useState(false);
  /** skinview3d 是动态 import 的，渲染器就绪后置位以触发皮肤/披风/图层加载 */
  const [ready, setReady] = useState(false);

  // 回调经 ref 转发，避免父组件每次渲染都重建 three 事件依赖
  const handlersRef = useRef({ onTexelPaint, onTexelPick, onStrokeEnd, tool });

  useEffect(() => {
    handlersRef.current = { onTexelPaint, onTexelPick, onStrokeEnd, tool };
  });

  useEffect(() => {
    let alive = true;

    void (async () => {
      if (!canvasRef.current) return;
      try {
        const [{ SkinViewer }, THREE_NS] = await Promise.all([
          import("skinview3d"),
          import("three"),
        ]);

        if (!alive || !canvasRef.current) return;
        const rect = containerRef.current?.getBoundingClientRect();
        const viewer = new SkinViewer({
          canvas: canvasRef.current,
          width: Math.max(200, Math.round(rect?.width ?? 280)),
          height: Math.max(240, Math.round(rect?.height ?? 340)),
          renderPaused: true,
        });

        viewer.controls.enableZoom = true;
        viewer.controls.enableRotate = false; // 绘画模式默认不动镜头
        viewer.controls.enablePan = false;
        viewer.renderPaused = false;
        viewerRef.current = {
          viewer: viewer as unknown as SkinViewerLike,
          camera: viewer.camera,
          playerWrapper: viewer.playerWrapper,
          playerObject: viewer.playerObject,
          raycaster: new THREE_NS.Raycaster(),
        };
        // viewerRef 是 ref：赋值不会触发重渲染。下面几个"加载皮肤/披风/图层"
        // 的 effect 在首次提交时一定拿不到句柄（动态 import 还没 resolve），
        // 必须靠这个 ready 再跑一次，否则预览里永远是 skinview3d 的默认皮肤。
        if (alive) setReady(true);
      } catch {
        if (alive) setFailed(true);
      }
    })();

    return () => {
      alive = false;
      viewerRef.current?.viewer.dispose();
      viewerRef.current = null;
    };
  }, []);

  // 容器尺寸变化 → 同步渲染器尺寸，避免 CSS 拉伸导致的人偶变形
  useEffect(() => {
    const container = containerRef.current;

    if (!container) return;
    const observer = new ResizeObserver(() => {
      const handle = viewerRef.current;

      if (!handle) return;
      const rect = container.getBoundingClientRect();

      if (rect.width < 1 || rect.height < 1) return;
      handle.viewer.setSize(
        Math.max(200, Math.round(rect.width)),
        Math.max(240, Math.round(rect.height)),
      );
    });

    observer.observe(container);

    return () => observer.disconnect();
  }, []);

  // 皮肤与模型变化 → 重新加载
  useEffect(() => {
    const handle = viewerRef.current;

    if (!handle || !skinUri) return;
    handle.viewer
      .loadSkin(skinUri, { model: model === "slim" ? "slim" : "default" })
      .catch(() => undefined);
  }, [skinUri, model, ready]);

  // 披风：有贴图且开关打开时加载并装备，否则卸下
  useEffect(() => {
    const handle = viewerRef.current;

    if (!handle) return;
    if (showCape && capeUri) {
      void Promise.resolve(
        handle.viewer.loadCape(capeUri, { backEquipment: "cape" }),
      ).catch(() => undefined);
    } else {
      handle.playerObject.backEquipment = null;
    }
  }, [showCape, capeUri, ready]);

  // 皮肤内外层显示
  useEffect(() => {
    const skin = viewerRef.current?.playerObject.skin;

    if (!skin) return;
    skin.setInnerLayerVisible(showInner);
    skin.setOuterLayerVisible(showOuter);
  }, [showInner, showOuter, skinUri, ready]);

  // 移动模式：只有它开启轨道旋转
  useEffect(() => {
    const handle = viewerRef.current;

    if (handle) handle.viewer.controls.enableRotate = moveMode;
  }, [moveMode, ready]);

  /** 指针位置 → 命中模型面的贴图像素坐标 */
  const texelFromPointer = (event: React.PointerEvent<HTMLCanvasElement>) => {
    const handle = viewerRef.current;

    if (!handle) return null;
    const rect = event.currentTarget.getBoundingClientRect();
    const ndc = new THREE.Vector2(
      ((event.clientX - rect.left) / rect.width) * 2 - 1,
      -((event.clientY - rect.top) / rect.height) * 2 + 1,
    );

    handle.raycaster.setFromCamera(ndc, handle.camera);
    const hit = handle.raycaster
      .intersectObject(handle.playerWrapper, true)
      .find((entry) => entry.uv);

    if (!hit?.uv) return null;
    const image = handle.playerObject.skin.map?.image;
    const width = image?.width ?? 64;
    const height = image?.height ?? 64;

    return {
      x: Math.min(width - 1, Math.max(0, Math.floor(hit.uv.x * width))),
      y: Math.min(height - 1, Math.max(0, Math.floor((1 - hit.uv.y) * height))),
    };
  };

  const handlePointerDown = (event: React.PointerEvent<HTMLCanvasElement>) => {
    if (moveMode || !skinUri) return;
    const point = texelFromPointer(event);

    if (!point) return;
    event.currentTarget.setPointerCapture(event.pointerId);
    paintingRef.current = true;
    if (handlersRef.current.tool === "eyedropper")
      handlersRef.current.onTexelPick?.(point.x, point.y);
    else handlersRef.current.onTexelPaint?.(point.x, point.y);
  };

  const handlePointerMove = (event: React.PointerEvent<HTMLCanvasElement>) => {
    if (!paintingRef.current) return;
    // 吸管只取色不落笔：拖动时继续画会污染贴图
    if (handlersRef.current.tool === "eyedropper") return;
    const point = texelFromPointer(event);

    if (point) handlersRef.current.onTexelPaint?.(point.x, point.y);
  };

  const handlePointerUp = () => {
    if (!paintingRef.current) return;
    paintingRef.current = false;
    handlersRef.current.onStrokeEnd?.();
  };

  if (failed) {
    return (
      <div className="flex h-full min-h-[240px] items-center justify-center text-xs text-gray-400">
        {t("3D 渲染器加载失败")}
      </div>
    );
  }

  return (
    <div
      ref={containerRef}
      className="h-full min-h-[240px] w-full overflow-hidden"
    >
      <canvas
        ref={canvasRef}
        aria-label={t("皮肤 3D 预览")}
        className={`block h-full w-full ${
          moveMode ? "cursor-grab active:cursor-grabbing" : "cursor-crosshair"
        }`}
        onPointerDown={handlePointerDown}
        onPointerMove={handlePointerMove}
        onPointerUp={handlePointerUp}
      />
    </div>
  );
};

interface ViewerHandle {
  viewer: SkinViewerLike;
  camera: THREE.PerspectiveCamera;
  playerWrapper: THREE.Group;
  playerObject: {
    skin: {
      map: THREE.Texture | null;
      setInnerLayerVisible(value: boolean): void;
      setOuterLayerVisible(value: boolean): void;
    };
    backEquipment: "cape" | "elytra" | null;
  };
  raycaster: THREE.Raycaster;
}

export default SkinPreview3D;
