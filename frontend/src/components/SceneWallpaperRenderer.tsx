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
import type { WEScenePayload } from "../lib/we-scene/types";

import React, { useEffect, useRef, useState } from "react";

import { SceneRenderer } from "../lib/we-scene/renderer";

/**
 * Wallpaper Engine 场景壁纸渲染组件。
 *
 * 输入是后端 GetWallpaperEngineWallpaper 返回的 Scene 载荷
 * (设计分辨率 + objects + 用户属性),内部用 SceneRenderer(three.js)
 * 搭建动态场景:多图层/效果/粒子/文本脚本/鼠标视差。
 *
 * 生命周期约定:
 *  - 挂载即 start();组件卸载或载荷变化时 dispose(释放 GPU 资源);
 *  - WebGL 不可用或场景组装失败 → onError,父层保留静态兜底图;
 *  - 首帧上屏 → onReady,父层可以把垫底的静态图隐掉省一份绘制。
 */
const SceneWallpaperRenderer: React.FC<{
  payload: WEScenePayload;
  onReady?: () => void;
  onError?: (error: unknown) => void;
  /** 渲染分辨率倍数(相对窗口 CSS 像素,默认 1) */
  pixelRatio?: number;
  /** 刷新率上限(fps);0/未设置 = 自适应 */
  fpsCap?: number;
}> = ({ payload, onReady, onError, pixelRatio, fpsCap }) => {
  const canvasRef = useRef<HTMLCanvasElement>(null);
  // ready/failed 触发父层切换;组件自身渲染始终保持 canvas 存在,
  // 失败时由父层停止挂载本组件
  const [, setReady] = useState(false);
  const readyRef = useRef(onReady);
  const errorRef = useRef(onError);

  readyRef.current = onReady;
  errorRef.current = onError;

  useEffect(() => {
    const canvas = canvasRef.current;

    if (!canvas) return;
    let renderer: SceneRenderer | null = null;
    let cancelled = false;

    const instance = new SceneRenderer({
      canvas,
      payload,
      pixelRatio,
      fpsCap,
      onFirstFrame: () => {
        if (cancelled) return;
        setReady(true);
        readyRef.current?.();
      },
    });

    renderer = instance;
    instance.start().catch((error) => {
      if (cancelled) return;
      console.error("[WE Scene] 场景渲染初始化失败,回退静态图:", error);
      errorRef.current?.(error);
    });

    return () => {
      cancelled = true;
      renderer?.dispose();
    };
    // 载荷对象由轮询重建;按 Entry 标识变化重建渲染器即可,
    // 其余字段变化(换壁纸)必然伴随 Entry 变化;分辨率/刷新率
    // 设置变化同样整体重建(改这两项是低频操作,重建成本可接受)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [payload.Entry, pixelRatio, fpsCap]);

  return (
    <canvas
      ref={canvasRef}
      className="absolute inset-0 h-full w-full nya-bg-fade"
      style={{ display: "block" }}
    />
  );
};

export default SceneWallpaperRenderer;
