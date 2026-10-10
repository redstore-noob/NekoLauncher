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

import { useEffect, useState } from "react";

/** 心跳轮询间隔(ms):WebView2 最小化时不一定触发 visibilitychange */
const HEARTBEAT_MS = 1000;

/**
 * 窗口当前是否"真的在用户眼前"。
 *
 * 三种信号合并判断(取"看不见"的那一个为准):
 *   - document.hidden:切换到别的标签页/系统挂起时的标准信号;
 *   - 窗口 blur/focus:用户点了别的应用;
 *   - document.hasFocus():WebView2/WebKitGTK 里窗口最小化往往不发
 *     visibilitychange 也不发 blur,只能靠这个值轮询兜底——启动器最小化到
 *     托盘后壁纸还在满帧跑 GPU 是实打实的耗电,值得每秒问一次。
 *
 * 返回 false 时调用方应停掉一切逐帧工作(场景渲染、视频播放)。
 * 判定为"看不见"会有最多 1 秒延迟(心跳),恢复则靠 focus 事件即时生效。
 */
export function useWindowVisible(): boolean {
  const [visible, setVisible] = useState(true);

  useEffect(() => {
    const evaluate = () => setVisible(!document.hidden && document.hasFocus());

    evaluate();

    const timer = window.setInterval(evaluate, HEARTBEAT_MS);

    window.addEventListener("focus", evaluate);
    window.addEventListener("blur", evaluate);
    document.addEventListener("visibilitychange", evaluate);

    return () => {
      window.clearInterval(timer);
      window.removeEventListener("focus", evaluate);
      window.removeEventListener("blur", evaluate);
      document.removeEventListener("visibilitychange", evaluate);
    };
  }, []);

  return visible;
}
