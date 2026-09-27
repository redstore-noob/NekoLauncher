/*
 * 运行平台判断（轻量）：WebView 的 userAgent 与宿主 OS 一致，够文件对话框
 * 过滤器这类场景用。别拿它做能力检测——需要系统能力时走后端 binding。
 */

export type RuntimePlatform = "windows" | "macos" | "linux";

export function currentPlatform(): RuntimePlatform {
  const ua = navigator.userAgent;

  if (/Windows NT/i.test(ua)) return "windows";
  if (/Mac OS X|Macintosh/i.test(ua)) return "macos";

  return "linux";
}

export function isWindowsPlatform(): boolean {
  return currentPlatform() === "windows";
}
