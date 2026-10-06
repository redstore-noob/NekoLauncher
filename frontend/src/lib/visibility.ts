// 无卡设计 · 后台省电：高频轮询在窗口隐藏/最小化时暂停，
// 回到前台立即补一拍（数据不显旧），再恢复常规节拍。
// 返回清理函数，直接放进 useEffect 的 return 即可。
export function startVisiblePoll(
  fn: () => void,
  intervalMs: number,
): () => void {
  let timer: number | null = null;

  const stop = () => {
    if (timer !== null) {
      window.clearInterval(timer);
      timer = null;
    }
  };
  const start = () => {
    if (timer !== null) return;
    timer = window.setInterval(fn, intervalMs);
  };
  const onVisibility = () => {
    if (document.hidden) {
      stop();
    } else {
      fn(); // 回前台先补一拍，避免窗口恢复时展示几十秒前的旧数据
      start();
    }
  };

  start();
  document.addEventListener("visibilitychange", onVisibility);

  return () => {
    stop();
    document.removeEventListener("visibilitychange", onVisibility);
  };
}
