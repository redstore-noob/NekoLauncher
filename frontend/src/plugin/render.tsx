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
  LaunchStateSummary,
  PageDefinition,
  PageRenderContext,
  WidgetDefinition,
  WidgetRenderContext,
} from "./types";

import React, { useEffect, useState } from "react";

import { GetLaunchSnapshot } from "../../wailsjs/go/bindings/LauncherAPI";
import { EventsOn } from "../../wailsjs/runtime/runtime";

export const WidgetHost: React.FC<{
  definition: WidgetDefinition;
  context: WidgetRenderContext;
}> = ({ definition, context }) => <>{definition.render(context)}</>;

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

export const PageHost: React.FC<{ definition: PageDefinition }> = ({
  definition,
}) => {
  const context: PageRenderContext = useLaunchState();

  return <>{definition.render(context)}</>;
};
