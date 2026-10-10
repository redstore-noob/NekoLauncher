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
import type { WEScenePayload } from "../lib/we-scene-payload";

import React, { useEffect, useRef, useState } from "react";

import { createAudioSource } from "../lib/we-wallgl-audio";

/**
 * WebWallGL 场景壁纸宿主组件。
 *
 * 场景渲染交给 oneincase/webwallgl(MIT):它直接吃 scene.pkg 原始字节,
 * 自己做解析、装配、脚本沙箱、粒子/模型/效果与音频可视化——这正是
 * dsh-wallpaper-engine 用的那套渲染核心。宿主侧的自研渲染器已移除。
 *
 * 资源来源:后端的 /wescene 路由(带会话指纹的基址),httpSource 会去取
 *   {Base}/project.json  属性表与场景形态判定
 *   {Base}/scene.pkg     场景包原始字节
 *   {Base}/<相对路径>     松散工程文件(没有 scene.pkg 的壁纸)
 *
 * 生命周期:
 *  - 挂载后动态 import 库(约 1.1MB,放进独立 chunk,不拖启动包)并 mount;
 *  - 首帧 onReady → 父层收掉静态垫底图;失败/超时 → onError,父层保留静态图;
 *  - 窗口最小化/失焦 → pause()(帧循环整体停,省 GPU);回前台 resume();
 *  - 卸载 → destroy(释放 GL/视频/音频资源,并淘汰本实例的包解析缓存)。
 */
const WebWallglScene: React.FC<{
  payload: WEScenePayload;
  onReady?: () => void;
  onError?: (error: unknown) => void;
  /** 渲染分辨率倍数(相对窗口 CSS 像素) */
  pixelRatio?: number;
  /** 刷新率上限(fps);0/未设置 = 库默认(60) */
  fpsCap?: number;
  /** 窗口是否在用户眼前(false 时暂停渲染) */
  windowVisible?: boolean;
}> = ({
  payload,
  onReady,
  onError,
  pixelRatio,
  fpsCap,
  windowVisible = true,
}) => {
  const hostRef = useRef<HTMLDivElement>(null);
  const instanceRef = useRef<{
    pause: () => void;
    resume: () => void;
    destroy: (opts?: { releasePkgCache?: boolean }) => void;
  } | null>(null);
  const readyRef = useRef(onReady);
  const errorRef = useRef(onError);
  const [, setReady] = useState(false);

  readyRef.current = onReady;
  errorRef.current = onError;

  useEffect(() => {
    const host = hostRef.current;

    if (!host) return;
    let cancelled = false;

    const start = async () => {
      const { mount, httpSource } = await import("webwallgl");

      if (cancelled) return;
      // 后端给的是带指纹的基址(/wescene/v/<token>);旧后端没有 Base 字段时
      // 回落到不带指纹的 /wescene(功能可用,只是换壁纸后库里缓存不吃新包,
      // 但组件是按载荷变化重新挂载的,实际影响仅限同一会话内的极端情况)
      const base = payload.Base || "/wescene";

      const instance = await mount(host, {
        source: httpSource(base),
        properties: payload.Properties ?? undefined,
        // 分辨率倍数与帧率上限直接对接既有设置
        renderDpr: pixelRatio && pixelRatio > 0 ? pixelRatio : undefined,
        fps: fpsCap && fpsCap > 0 ? fpsCap : undefined,
        // 音频可视化走启动器自己播的音乐(见 we-wallgl-audio.ts):
        // 库自带的模拟频谱是无声的假数据,关掉换成真实的音乐频谱
        audio: createAudioSource(),
        // 首帧看门狗:库默认 60s,压到 20s——场景挂不上要尽早回落静态图,
        // 而不是让用户对着"什么都没有"的后台转圈
        mountTimeoutMs: 20000,
        onDiagnostic: (message: string, level: string) => {
          if (level === "error") console.warn("[WE WebWallGL]", message);
        },
        onReady: () => {
          if (cancelled) return;
          setReady(true);
          readyRef.current?.();
        },
        onError: (error: unknown) => {
          if (cancelled) return;
          console.error("[WE WebWallGL] 场景挂载失败,回退静态图:", error);
          errorRef.current?.(error);
        },
      });

      if (cancelled) {
        instance.destroy({ releasePkgCache: true });

        return;
      }
      instanceRef.current = instance;
      if (!windowVisible) instance.pause();
    };

    start().catch((error) => {
      if (cancelled) return;
      console.error("[WE WebWallGL] 初始化失败,回退静态图:", error);
      errorRef.current?.(error);
    });

    return () => {
      cancelled = true;
      // 换壁纸必须淘汰包解析缓存:同一会话里几十 MB 的包压着不放会持续吃内存
      instanceRef.current?.destroy({ releasePkgCache: true });
      instanceRef.current = null;
    };
    // payload 由轮询重建;按 Base(带会话指纹:换壁纸/包被重写就会变)重建实例,
    // 分辨率/帧率改动属低频操作,重建成本可接受
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [payload.Base, pixelRatio, fpsCap]);

  // 窗口隐藏(最小化/失焦)→ 停渲染;回前台 → 恢复
  useEffect(() => {
    const instance = instanceRef.current;

    if (!instance) return;
    if (windowVisible) instance.resume();
    else instance.pause();
  }, [windowVisible]);

  return <div ref={hostRef} className="absolute inset-0 h-full w-full" />;
};

export default WebWallglScene;
