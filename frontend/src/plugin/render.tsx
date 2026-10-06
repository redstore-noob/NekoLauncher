/*
 * 扩展点渲染包装。
 *
 * render 是普通函数而非组件。宿主若直接调用（definition.render(ctx)），插件写在 render
 * 里的 hooks 会挂到宿主组件的 hook 链表上——布局里增删一个小组件就改变了宿主的 hook
 * 数量，React 会直接报 "Rendered fewer hooks than expected"。
 *
 * 因此每个扩展点都套一层组件：hooks 各自归属自己的链表，扩展点的增删互不影响。
 */
import type {
  LaunchCardContext,
  LaunchStateSummary,
  PageDefinition,
  PageRenderContext,
  WidgetDefinition,
  WidgetRenderContext,
} from "./types";

import React, { Suspense, useEffect, useState } from "react";
import { Button } from "@heroui/react";

import LoadingRow from "../components/loading-row";
import { notify } from "../components/overlay/dialog";
import { t } from "../i18n";
import { GetLaunchSnapshot } from "../../wailsjs/go/bindings/LauncherAPI";
import { EventsOn } from "../../wailsjs/runtime/runtime";

import { usePageActions } from "./registry";

export const WidgetHost: React.FC<{
  definition: WidgetDefinition;
  context: WidgetRenderContext;
}> = ({ definition, context }) => <>{definition.render(context)}</>;

/**
 * LaunchCardHost：插件启动卡覆盖的渲染宿主。与 WidgetHost 同理——插件
 * render 里写的 hooks 必须挂在这个组件自己的链表上，不能挂到主页组件上，
 * 否则插件启用/停用改变主页 hooks 数量会直接触发 React 报错。
 */
export const LaunchCardHost: React.FC<{
  context: LaunchCardContext;
  render: (context: LaunchCardContext) => React.ReactNode;
}> = ({ context, render }) => <>{render(context)}</>;

/** useLaunchState 订阅启动状态：初始拉取 + launch:changed 事件增量刷新 */
function useLaunchState(): LaunchStateSummary {
  const [state, setState] = useState<LaunchStateSummary>({
    launchPhase: 0,
    isBusy: false,
    isGameRunning: false,
  });

  useEffect(() => {
    let mounted = true;
    const refresh = () => {
      void GetLaunchSnapshot().then((snapshot) => {
        if (!mounted || !snapshot) return;
        const phase = Number(snapshot.Phase ?? 0);

        setState({
          launchPhase: phase,
          isBusy: phase === 1,
          isGameRunning: phase === 2,
        });
      });
    };

    refresh();
    const cancel = EventsOn("launch:changed", refresh);

    return () => {
      mounted = false;
      cancel?.();
    };
  }, []);

  return state;
}

/**
 * PageActionSlot：宿主内置页面里的插件操作按钮插槽（见 types.PageActionDefinition）。
 *
 * 两条硬约定：
 * - 没有插件注册时**直接返回 null**：调用点外层是带 gap 的 flex 行，返回空 div 外壳
 *   会凭空多出 6~16px 间距、甚至挤到换行；
 * - 单个按钮的 onPress 抛错只吃掉它自己：插件按钮不该弄坏宿主页面的这一块。
 * 等待期间按钮进入忙碌态（禁用 + loading），避免用户连点。
 */
export const PageActionSlot: React.FC<{ pageId: string }> = ({ pageId }) => {
  const actions = usePageActions(pageId);
  const [busyId, setBusyId] = useState("");

  if (actions.length === 0) return null;

  const run = async (id: string, onPress: () => void | Promise<void>) => {
    setBusyId(id);
    try {
      await onPress();
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error);

      console.error("[plugins] 页面按钮执行失败：", error);
      notify.error(t("插件操作失败：{0}", { "0": message }));
    } finally {
      setBusyId("");
    }
  };

  return (
    <div className="flex flex-none flex-wrap items-center gap-1.5">
      {actions.map((action) => (
        <Button
          key={action.id}
          isLoading={busyId === action.id}
          size="sm"
          startContent={action.icon}
          title={action.tooltip ?? action.label}
          variant="flat"
          onPress={() => void run(action.id, action.onPress)}
        >
          {action.label}
        </Button>
      ))}
    </div>
  );
};

export const PageHost: React.FC<{ definition: PageDefinition }> = ({
  definition,
}) => {
  const context: PageRenderContext = useLaunchState();

  return (
    // 页面是懒加载 chunk（见 lib/lazy + builtins）：空闲预取命中时这一帧
    // 根本不会出现；只有极早期的快速点击会短暂看到占位，不留白屏闪烁
    <Suspense
      fallback={
        <div className="flex h-full items-center justify-center">
          <LoadingRow className="my-0" text={t("加载中…")} />
        </div>
      }
    >
      {definition.render(context)}
    </Suspense>
  );
};
