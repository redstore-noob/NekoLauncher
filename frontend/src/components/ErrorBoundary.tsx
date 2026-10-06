/*
 * 渲染错误隔离。
 *
 * React 里未捕获的渲染错误会把整棵树卸载掉——主页里一个小组件抛错，整个启动器界面
 * 就会变成空白。所有扩展点（内置与插件一视同仁）都套一层这里，坏掉一个只丢一个。
 */
import React from "react";

import { t } from "../i18n";

interface ErrorBoundaryProps {
  /** 出错提示里显示的名字，如小组件标题；缺省用泛化文案 */
  title?: string;
  children: React.ReactNode;
}

interface ErrorBoundaryState {
  error: Error | null;
}

export default class ErrorBoundary extends React.Component<
  ErrorBoundaryProps,
  ErrorBoundaryState
> {
  state: ErrorBoundaryState = { error: null };

  static getDerivedStateFromError(error: Error): ErrorBoundaryState {
    return { error };
  }

  componentDidCatch(error: Error, info: React.ErrorInfo) {
    console.error(
      t("[NekoLauncher] {0}渲染失败：", { "0": this.props.title ?? "组件" }),
      error,
      info.componentStack,
    );
  }

  render() {
    const { error } = this.state;

    if (!error) return this.props.children;

    return (
      <div className="rounded-lg border border-red-200/70 bg-red-50/70 px-4 py-3 text-xs leading-relaxed text-red-600 dark:border-red-900/50 dark:bg-red-950/40 dark:text-red-300">
        {this.props.title
          ? t("「{0}」加载失败", { "0": this.props.title })
          : t("组件加载失败")}
        {error.message ? `：${error.message}` : ""}
      </div>
    );
  }
}
