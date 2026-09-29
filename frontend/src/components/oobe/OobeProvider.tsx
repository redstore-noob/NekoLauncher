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

/**
 * 首次启动引导（OOBE）的全局状态。
 *
 * 是否需要引导以 launcher.yaml 的 oobeCompleted 键为准（经 ConfigAPI 读写）：
 * 键不存在 → 首启，弹出引导；完成或跳过后写入 "true"，此后不再打扰。
 *
 * 引导有两种形态：
 * - 完整覆盖层（dock = false）：欢迎三选页与流程步骤；
 * - 跟随小窗（dock = true）：用户点了"前往某页面"后，引导收成右下角
 *   悬浮小卡继续提示当前步骤——切页不丢引导，操作完仍能"下一步"。
 */
import React, {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
} from "react";

import { GetValue, SetValue } from "../../../wailsjs/go/bindings/ConfigAPI";
import { navigateToPage } from "../../lib/navigation";

/** launcher.yaml 中的键：引导完成标记 */
export const OOBE_COMPLETED_KEY = "oobeCompleted";

/** 引导入口选择的身份，决定后续展示哪条流程 */
export type OobeFlow = "newbie" | "creator";

interface OobeState {
  /** 是否应显示引导（未完成过） */
  oobeNeeded: boolean;
  /** 配置是否已读完（读完后才决定渲染引导，避免先闪一下） */
  hydrated: boolean;
  /** 当前选择的引导流程（欢迎页选择后设置） */
  flow: OobeFlow | null;
  /** 引导是否处于"跟随小窗"形态（切页后仍悬浮提示） */
  docked: boolean;
  /** 从欢迎页进入对应流程 */
  beginFlow: (flow: OobeFlow) => void;
  /** 返回欢迎页（流程内可反悔换路线） */
  backToWelcome: () => void;
  /** 跳到指定页面并收成跟随小窗（引导不中断） */
  followToPage: (pageId: string, detail?: string) => void;
  /** 从跟随小窗展开回完整覆盖层（停留在当前步骤） */
  expandGuide: () => void;
  /** 完成/跳过引导：持久化标记并关闭一切引导 UI */
  completeOobe: () => void;
}

const OobeContext = createContext<OobeState>({
  oobeNeeded: false,
  hydrated: false,
  flow: null,
  docked: false,
  beginFlow: () => {
    /* Provider 未挂载时的空实现 */
  },
  backToWelcome: () => {
    /* 同上 */
  },
  followToPage: () => {
    /* 同上 */
  },
  expandGuide: () => {
    /* 同上 */
  },
  completeOobe: () => {
    /* 同上 */
  },
});

export function useOobe(): OobeState {
  return useContext(OobeContext);
}

export const OobeProvider: React.FC<{ children: React.ReactNode }> = ({
  children,
}) => {
  const [oobeNeeded, setOobeNeeded] = useState(false);
  const [hydrated, setHydrated] = useState(false);
  const [flow, setFlow] = useState<OobeFlow | null>(null);
  const [docked, setDocked] = useState(false);

  useEffect(() => {
    // 键不存在（首次启动）时 GetValue 通常回 null；读失败也按"需要引导"兜底，
    // 宁可老用户多看一次欢迎页，也不要让新人永远错过引导。
    GetValue(OOBE_COMPLETED_KEY)
      .then((value: string) => setOobeNeeded(value !== "true"))
      .catch(() => setOobeNeeded(true))
      .finally(() => setHydrated(true));
  }, []);

  const beginFlow = useCallback((next: OobeFlow) => {
    setDocked(false);
    setFlow(next);
  }, []);
  const backToWelcome = useCallback(() => {
    setDocked(false);
    setFlow(null);
  }, []);

  const followToPage = useCallback((pageId: string, detail?: string) => {
    // 收成小窗而不是关闭：用户在新页面上仍能看到当前步骤的提示
    setDocked(true);
    navigateToPage(pageId, detail);
  }, []);

  const expandGuide = useCallback(() => setDocked(false), []);

  const completeOobe = useCallback(() => {
    setOobeNeeded(false);
    setFlow(null);
    setDocked(false);
    // 写失败不阻塞关闭：下次启动会再看一次引导，可接受
    void SetValue(OOBE_COMPLETED_KEY, "true").catch(() => undefined);
  }, []);

  const value = useMemo(
    () => ({
      oobeNeeded,
      hydrated,
      flow,
      docked,
      beginFlow,
      backToWelcome,
      followToPage,
      expandGuide,
      completeOobe,
    }),
    [
      oobeNeeded,
      hydrated,
      flow,
      docked,
      beginFlow,
      backToWelcome,
      followToPage,
      expandGuide,
      completeOobe,
    ],
  );

  return <OobeContext.Provider value={value}>{children}</OobeContext.Provider>;
};
