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
import type { bindings } from "../../../wailsjs/go/models";

import React, { useCallback, useEffect, useRef, useState } from "react";
import {
  ArrowRepeatAll20Regular,
  ArrowClockwise20Regular,
  Person20Regular,
  Warning20Regular,
} from "@fluentui/react-icons";
import { Button, Tooltip } from "@heroui/react";

import {
  GetSkinTexture,
  RefreshSkinTexture,
} from "../../../wailsjs/go/bindings/AccountAPI";
import { EventsOn, EventsOff } from "../../../wailsjs/runtime/runtime";
import { tooltipMotionProps } from "../../lib/motion";
import { t } from "../../i18n";

type SkinTexture = bindings.SkinTexture;

/** 动作轮换（与 skinview3d 内置动画一一对应） */
export const SKIN_ANIMATIONS = [
  t("待机"),
  t("走路"),
  t("奔跑"),
  t("飞行"),
] as const;

interface SkinViewerLike {
  loadSkin: (
    source: string,
    options?: { model?: "default" | "slim" },
  ) => Promise<unknown>;
  loadCape: (
    source: string,
    options?: { backEquipment?: "cape" | "elytra" },
  ) => Promise<unknown>;
  dispose: () => void;
  width: number;
  height: number;
  animation: unknown;
  controls: {
    enableZoom: boolean;
    enablePan: boolean;
    minDistance: number;
    maxDistance: number;
  };
  zoom: number;
}

/** skinview3d 体积较大（含 three.js），按需动态加载、独立分包 */
async function loadSkinViewerModule() {
  return import("skinview3d");
}

/**
 * 3D 皮肤预览面板：skinview3d 渲染指定账号的皮肤（双层皮肤 / 披风），
 * 贴图经后端 GetSkinTexture 取回（data URI）。主页皮肤展示小组件与
 * 账户页底部的展示区共用本面板；账号选择逻辑由调用方负责。
 */
const SkinPreviewPanel: React.FC<{
  /** 展示哪个账号（稳定键）；空串显示未选择账号占位 */
  accountKey: string;
  /** 画布高度（px），缺省 230 */
  height?: number;
}> = ({ accountKey, height = 230 }) => {
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const wrapRef = useRef<HTMLDivElement>(null);
  const viewerRef = useRef<SkinViewerLike | null>(null);
  const aliveRef = useRef(true);

  const [texture, setTexture] = useState<SkinTexture | null>(null);
  const [error, setError] = useState("");
  const [ready, setReady] = useState(false);
  const [refreshing, setRefreshing] = useState(false);
  const [animationIndex, setAnimationIndex] = useState(0);

  useEffect(() => {
    aliveRef.current = true;

    return () => {
      aliveRef.current = false;
    };
  }, []);

  // 拉取皮肤贴图：force 为 true 时强制刷新并更新后端缓存
  const loadTexture = useCallback(
    async (force: boolean) => {
      if (!accountKey) return;
      if (force) setRefreshing(true);
      try {
        const tex = force
          ? await RefreshSkinTexture(accountKey)
          : await GetSkinTexture(accountKey);

        if (!aliveRef.current) return;
        if (tex?.skinUri) {
          setTexture(tex);
          setError("");
        } else {
          setError(t("没有可用的皮肤贴图"));
        }
      } catch (ex) {
        if (aliveRef.current) setError(String((ex as Error)?.message ?? ex));
      } finally {
        if (force && aliveRef.current) setRefreshing(false);
      }
    },
    [accountKey],
  );

  // 账号切换时重新加载
  useEffect(() => {
    setTexture(null);
    setError("");
    void loadTexture(false);
  }, [loadTexture]);

  // 后端每日自动刷新皮肤缓存后通知前端重新加载
  useEffect(() => {
    const onTextureChanged = (key: string) => {
      if (key && key !== accountKey) return;
      void loadTexture(false);
    };

    EventsOn("skin:textureChanged", onTextureChanged);

    return () => EventsOff("skin:textureChanged");
  }, [accountKey, loadTexture]);

  // 初始化渲染器（动态 import，失败时给出错误提示）
  useEffect(() => {
    let alive = true;
    let resizeObserver: ResizeObserver | null = null;

    void (async () => {
      if (!canvasRef.current) return;
      try {
        const { SkinViewer, IdleAnimation } = await loadSkinViewerModule();

        if (!alive || !canvasRef.current) return;
        const viewer = new SkinViewer({
          canvas: canvasRef.current,
          width: canvasRef.current.parentElement?.clientWidth ?? 300,
          height,
          renderPaused: true,
        });

        viewer.controls.enableZoom = true;
        viewer.controls.enablePan = false;
        viewer.controls.minDistance = 21;
        viewer.controls.maxDistance = 60;
        viewer.zoom = 0.78;
        viewer.animation = new IdleAnimation();
        viewer.renderPaused = false;
        viewerRef.current = viewer as unknown as SkinViewerLike;
        setReady(true);

        resizeObserver = new ResizeObserver((entries) => {
          const width = entries[0]?.contentRect.width;

          if (width && width > 0) {
            viewer.width = Math.round(width);
            viewer.height = height;
          }
        });
        if (wrapRef.current) resizeObserver.observe(wrapRef.current);
      } catch (ex) {
        console.error(t("加载 3D 皮肤渲染器失败"), ex);
        if (alive) setError(t("3D 渲染器加载失败"));
      }
    })();

    return () => {
      alive = false;
      resizeObserver?.disconnect();
      viewerRef.current?.dispose();
      viewerRef.current = null;
      setReady(false);
    };
    // height 在挂载期内不变（调用方传常量），无需参与依赖
  }, [height]);

  // 贴图/模型变化 → 载入渲染器
  useEffect(() => {
    const viewer = viewerRef.current;

    if (!viewer || !texture?.skinUri) return;
    viewer
      .loadSkin(texture.skinUri, {
        model: texture.model === "slim" ? "slim" : "default",
      })
      .catch((ex) => {
        console.error(t("皮肤贴图加载失败"), ex);
        setError(t("皮肤贴图加载失败"));
      });
    if (texture.capeUri) {
      viewer
        .loadCape(texture.capeUri, { backEquipment: "cape" })
        .catch(() => undefined);
    }
  }, [texture, ready]);

  const cycleAnimation = () => {
    const next = (animationIndex + 1) % SKIN_ANIMATIONS.length;

    setAnimationIndex(next);
    void (async () => {
      const viewer = viewerRef.current;

      if (!viewer) return;
      const mod = await loadSkinViewerModule();
      const {
        IdleAnimation,
        WalkingAnimation,
        RunningAnimation,
        FlyingAnimation,
      } = mod;
      const animations = [
        () => new IdleAnimation(),
        () => new WalkingAnimation(),
        () => new RunningAnimation(),
        () => new FlyingAnimation(),
      ];

      viewer.animation = animations[next]();
    })();
  };

  return (
    <div ref={wrapRef} className="relative flex flex-col items-center gap-1">
      {/* 画布常驻挂载（渲染器只在 mount 时初始化一次），无账号/出错时盖提示层 */}
      <canvas
        ref={canvasRef}
        className={`w-full rounded-lg bg-default-100/50 ${
          accountKey && !error
            ? "cursor-grab active:cursor-grabbing"
            : "pointer-events-none opacity-20"
        }`}
        style={{ height }}
      />
      {error ? (
        <div className="absolute inset-0 flex items-center justify-center p-4">
          <div className="flex items-start gap-2 rounded-lg bg-danger/10 px-3 py-2.5 text-xs text-danger">
            <span className="mt-px flex-none">
              <Warning20Regular className="h-4 w-4" />
            </span>
            <span className="break-all">{error}</span>
          </div>
        </div>
      ) : !accountKey ? (
        <div className="absolute inset-0 flex flex-col items-center justify-center gap-2 bg-default-100/40 text-center text-gray-400">
          <div className="flex size-12 items-center justify-center rounded-lg bg-default-100/90">
            <Person20Regular className="h-6 w-6" />
          </div>
          <span className="text-xs leading-relaxed">{t("未选择账号")}</span>
        </div>
      ) : !texture ? (
        <div className="absolute inset-0 flex items-center justify-center text-xs text-gray-400">
          {t("正在加载皮肤…")}
        </div>
      ) : null}
      <div className="flex w-full items-center justify-between gap-2 pt-1">
        <span className="truncate text-[11px] text-gray-400">
          {texture
            ? `${texture.model === "slim" ? "纤细模型" : "经典模型"}${texture.capeUri ? " · 已装备披风" : ""}${texture.cached ? " · 缓存" : ""}`
            : " "}
        </span>
        <div className="flex flex-none items-center gap-1.5">
          <Tooltip
            content={t("刷新皮肤")}
            delay={300}
            motionProps={tooltipMotionProps}
          >
            <Button
              isIconOnly
              className="h-7 min-w-0 flex-none"
              isDisabled={!accountKey}
              isLoading={refreshing}
              radius="full"
              size="sm"
              variant="flat"
              onPress={() => void loadTexture(true)}
            >
              <ArrowClockwise20Regular />
            </Button>
          </Tooltip>
          <Tooltip
            content={t("切换动作")}
            delay={300}
            motionProps={tooltipMotionProps}
          >
            <Button
              className="h-7 min-w-0 flex-none px-2.5 text-[11px]"
              isDisabled={!texture}
              radius="full"
              size="sm"
              startContent={<ArrowRepeatAll20Regular />}
              variant="flat"
              onPress={cycleAnimation}
            >
              {SKIN_ANIMATIONS[animationIndex]}
            </Button>
          </Tooltip>
        </div>
      </div>
    </div>
  );
};

export default SkinPreviewPanel;
