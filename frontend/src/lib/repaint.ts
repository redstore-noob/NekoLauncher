/**
 * 窗口重新可见后强制重建合成层。
 *
 * 背景：窗口是"可透明 + 无重定向位图"（main.go 的 WindowIsTranslucent），页面里
 * 又有大量 backdrop-filter 表面与整窗大小的背景层。WebView2/Chromium 在窗口被
 * 最小化、或被别的窗口完全遮挡时，可能把这些合成层的光栅缓存释放掉，恢复时
 * 不一定重建——表现为背景层/卡片底色不再绘制，直接透出桌面（"卡片变透明"）。
 *
 * 这里在"重新变为可见 / 重新获得焦点"时主动触发一次图层树重建：
 * 给 <html> 临时挂上会提升图层的类，下一帧再摘掉，迫使 Chromium 重新装配并
 * 光栅化整棵图层树。
 *
 * 为什么要跨帧：同一次任务里"加上再摘掉"会被样式系统合并成"没有变化"，
 * 等于什么都没做。必须让第一帧真的提交一次带该类的样式，第二帧再提交去掉它。
 */
const REPAINT_CLASS = "nya-force-repaint";

/** 已排期待处理的标记：焦点与可见性可能在同一瞬间先后触发，合并成一次 */
let scheduled = false;

function forceRepaint(): void {
  if (scheduled) return;
  scheduled = true;

  const root = document.documentElement;

  root.classList.add(REPAINT_CLASS);

  requestAnimationFrame(() => {
    requestAnimationFrame(() => {
      root.classList.remove(REPAINT_CLASS);
      scheduled = false;
    });
  });
}

/**
 * 挂上"恢复可见/获得焦点即重绘"的监听，返回清理函数（直接放进 useEffect 的
 * return 即可）。浏览器里跑 Vite dev 时行为一致，无副作用。
 */
export function startRepaintOnRestore(): () => void {
  const onVisibility = () => {
    if (!document.hidden) forceRepaint();
  };
  const onFocus = () => forceRepaint();

  document.addEventListener("visibilitychange", onVisibility);
  window.addEventListener("focus", onFocus);

  return () => {
    document.removeEventListener("visibilitychange", onVisibility);
    window.removeEventListener("focus", onFocus);
  };
}
