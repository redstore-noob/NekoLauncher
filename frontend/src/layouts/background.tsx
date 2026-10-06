/*
 * Copyright 2024 Next UI
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

import React, {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useState,
} from "react";

import { GetValue } from "../../wailsjs/go/bindings/ConfigAPI";
import {
  GetDesktopWallpaperPath,
  GetAcrylicBackdropEnabled,
  GetBingDailyImagePath,
  GetWallpaperEngineWallpaper,
} from "../../wailsjs/go/bindings/SystemAPI";
import { isLinuxPlatform } from "../lib/platform";

// launcher.yaml 中的键（经 ConfigAPI.SetValue/GetValue 读写）
export const BACKGROUND_PATH_KEY = "launcherBackgroundPath";
export const BACKGROUND_BLUR_KEY = "launcherBackgroundBlur";
export const BACKGROUND_MODE_KEY = "launcherBackgroundMode";
export const BACKGROUND_OPACITY_KEY = "launcherBackgroundOpacity";
/** 背景 scrim 不透明度（0-100，默认 80）：罩在模糊壁纸上的黑白底色强度 */
export const BACKGROUND_SCRIM_KEY = "launcherBackgroundScrim";
export const BACKGROUND_WINDOW_OPACITY_KEY = "launcherWindowOpacity";
/** 面板毛玻璃（前端 backdrop-filter）开关；Windows/macOS 未设置时默认开启，Linux 默认关闭 */
export const PANEL_BLUR_KEY = "launcherPanelBlurEnabled";
/** 面板毛玻璃强度（0-100，默认 70 = 历史观感；0 等同于关闭） */
export const PANEL_BLUR_STRENGTH_KEY = "launcherPanelBlurStrength";
/** 网页类壁纸是否允许接收鼠标交互（默认关闭，避免壁纸吃掉界面点击） */
export const WEB_WALLPAPER_INTERACTIVE_KEY = "launcherWebWallpaperInteractive";
/** WE 场景壁纸渲染分辨率倍数（相对窗口 CSS 像素；默认 1，HiDPI 屏可调高） */
export const WE_SCENE_RESOLUTION_KEY = "launcherWeSceneResolution";
/** WE 场景壁纸刷新率上限（"auto"=自适应，重壁纸自动锁 30；或数字 fps） */
export const WE_SCENE_FPS_KEY = "launcherWeSceneFps";
/**
 * Linux 下 WebKitGTK 是否走 GPU 合成（默认开启）。由 Go 侧在 wails.Run 之前
 * 读取——webview 的合成策略只在创建时生效，故改动需重启启动器；键名与
 * internal/config/launcherconfig.go 的 LinuxGpuAccelerationEnabled 一一对应。
 */
export const LINUX_GPU_KEY = "launcherLinuxGpuAcceleration";
/**
 * Windows 下 WebView2 是否启用 GPU 硬件加速（默认开启）。同样由 Go 侧在
 * wails.Run 之前读取（WebviewGpuIsDisabled 只在创建 webview 时生效），
 * 改动需重启启动器；键名与 launcherconfig.go 的 WindowsGpuAccelerationEnabled 对应。
 */
export const WINDOWS_GPU_KEY = "launcherWindowsGpuAcceleration";

/** 毛玻璃强度默认值：blur 系数 = 强度 / 它，因此 70 时系数为 1（观感不变） */
export const DEFAULT_PANEL_BLUR_STRENGTH = 70;

/** 强度（0-100）→ blur 系数；0 表示关闭，上限 2 倍 */
export function panelBlurScale(strength: number): number {
  if (strength <= 0) return 0;
  // 70 是默认值 → 系数 1（观感与改动前完全一致）；100 → 2 倍；越往下越收细
  if (strength <= DEFAULT_PANEL_BLUR_STRENGTH) {
    return Math.max(0.12, strength / DEFAULT_PANEL_BLUR_STRENGTH);
  }

  return (
    1 +
    (strength - DEFAULT_PANEL_BLUR_STRENGTH) /
      (100 - DEFAULT_PANEL_BLUR_STRENGTH)
  );
}

/**
 * 强度（0-100）→ 面板底色不透明度（默认 0.8）。
 *
 * 只调 blur 半径几乎看不出变化：面板底色本来就有 80% 遮盖力，再叠上默认 80% 的
 * "背景压暗度"，透到面板后面的壁纸细节只剩几个百分点——糊不糊都一样。所以强度
 * 同时联动"玻璃通透度"：越强越透，模糊才真的看得见；默认值仍是 0.8，观感不变。
 */
export function panelGlassAlpha(strength: number): number {
  if (strength <= 0) return 1;
  const delta = (strength - DEFAULT_PANEL_BLUR_STRENGTH) * 0.005;

  return Math.min(0.94, Math.max(0.55, 0.8 - delta));
}

/** 后端必应缓存拉取失败时，WebView 直连的兜底地址（第三方跳转服务） */
export const BING_REMOTE_FALLBACK_URL =
  "https://api.ffis.me/bing/bing-images.php";

/**
 * 背景图源：none=纯白（默认）；wallpaper=桌面壁纸；bing=必应每日一图；
 * image=自定义图片；wallpaper-engine=Wallpaper Engine 联动（静态图 / 视频 /
 * 网页壁纸分别用底图、<video>、<iframe> 呈现）
 */
export type BackgroundMode =
  | "none"
  | "wallpaper"
  | "bing"
  | "image"
  | "wallpaper-engine";

/** 桌面壁纸 / WE 壁纸模式下轮询变化的时间间隔（ms），保证与桌面保持一致。
 * Linux 后端靠 spawn gsettings/kreadconfig 等子进程探测壁纸，频率压低以减少
 * 周期性的子进程唤醒（换壁纸本来就是低频事件，晚半分钟感知到无所谓） */
const WALLPAPER_POLL_INTERVAL_MS = isLinuxPlatform() ? 60000 : 15000;

/** 必应每日图重取间隔（ms）：应用长时间挂着跨天时也能换上新一期 */
const BING_REFRESH_INTERVAL_MS = 30 * 60 * 1000;

interface BackgroundState {
  /** 背景图 URL（本地图片经应用内 /localfile 路由中转；空串 = 纯白无背景图） */
  url: string;
  /** 背景视频 URL（Wallpaper Engine 视频壁纸播放原文件；空串 = 不播放视频） */
  videoUrl: string;
  /** 网页壁纸 URL（Wallpaper Engine web 类型；空串 = 不加载 iframe） */
  webUrl: string;
  /** 网页壁纸是否允许接收鼠标交互 */
  webInteractive: boolean;
  /** WE 场景壁纸渲染分辨率倍数(相对窗口 CSS 像素;1 = 不按 DPR 放大) */
  sceneResolution: number;
  /** WE 场景壁纸刷新率上限(fps);0 = 自适应(重壁纸自动锁 30) */
  sceneFps: number;
  /** 背景模糊半径（px，0 = 不模糊） */
  blur: number;
  /** 背景不透明度（0-100，默认 100） */
  opacity: number;
  /** 背景 scrim 强度（0-100，默认 80）：罩在模糊壁纸上的黑白底色不透明度 */
  scrim: number;
  /** 当前图源模式 */
  mode: BackgroundMode;
  /** 使用的是自定义图片（决定设置页展示哪组控件） */
  isCustom: boolean;
  /** 窗口底色不透明度（0-100，默认 100=不透明；越低桌面越透出来） */
  windowOpacity: number;
  /** 亚克力模糊（DWM Acrylic 背景）是否启用：开启时透出的是模糊后的桌面 */
  acrylic: boolean;
  /** 面板毛玻璃（前端 backdrop-filter）是否开启；关闭时面板改为接近不透明底色 */
  panelBlur: boolean;
  /** 面板毛玻璃强度（0-100）：乘到各表面的 blur 半径上 */
  panelBlurStrength: number;
  /** 桌面壁纸路径（仅 wallpaper 模式下同步；空串表示尚未读取到） */
  wallpaperPath: string;
  /** WE 当前壁纸标题（仅 wallpaper-engine 模式下同步，供设置页展示） */
  wallpaperEngineTitle: string;
  /** WE 当前壁纸类型（video/scene/web/img，供设置页提示"动态壁纸展示预览图"） */
  wallpaperEngineType: string;
  /**
   * WE 网页壁纸用户属性的指纹:用户在 WE 里改配置后变化,
   * 拼到 iframe URL 上触发壁纸重载(新配置随入口注入重新生效)。
   */
  wallpaperEngineWebVersion: string;
  /**
   * WE 场景壁纸的完整渲染载荷（多图层/动画/粒子/用户配置）;
   * 非场景类型或载荷组装失败时为 null,背景层据此回退静态图。
   */
  sceneWallpaper: import("../lib/we-scene/types").WEScenePayload | null;
  /** 当前平台不支持 Wallpaper Engine 联动（WE 只有 Windows 版），设置页据此给出提示 */
  wallpaperEngineUnsupported: boolean;
  /**
   * 启动配置是否已从后端读完（mode/不透明度/模糊等一次性落定）。
   * 窗口显示（治启动闪屏）等它变 true 后才执行，避免先按默认值渲染、
   * 配置到位后又整体突变一闪。背景图 URL 属二次异步，不阻塞它。
   */
  hydrated: boolean;
  /** 修改后调用：从后端重读背景设置 */
  refresh: () => void;
}

const BackgroundContext = createContext<BackgroundState>({
  url: "",
  videoUrl: "",
  webUrl: "",
  webInteractive: false,
  sceneResolution: 1,
  sceneFps: 0,
  blur: 0,
  opacity: 100,
  scrim: 80,
  mode: "none",
  isCustom: false,
  windowOpacity: 100,
  acrylic: false,
  panelBlur: !isLinuxPlatform(),
  panelBlurStrength: DEFAULT_PANEL_BLUR_STRENGTH,
  wallpaperPath: "",
  wallpaperEngineTitle: "",
  wallpaperEngineType: "",
  wallpaperEngineWebVersion: "",
  sceneWallpaper: null,
  wallpaperEngineUnsupported: false,
  hydrated: false,
  refresh: () => {
    /* Provider 未挂载时的空实现 */
  },
});

export function useBackground(): BackgroundState {
  return useContext(BackgroundContext);
}

function parseMode(raw: string): BackgroundMode {
  switch (raw) {
    case "wallpaper":
    case "bing":
    case "image":
    case "wallpaper-engine":
      return raw;
    default:
      return "none"; // 未设置（新默认）或值损坏时用纯白
  }
}

function parseBlur(raw: string): number {
  const value = Number(raw);

  if (!Number.isFinite(value) || value <= 0) return 0;

  // Linux 的 WebKitGTK 对全屏 filter: blur 的栅格化成本远高于 WebView2，
  // 半径封顶压到 16px（视觉上仍有"磨砂"感，单帧成本降一个量级）
  const maxBlur = isLinuxPlatform() ? 16 : 40;

  return Math.min(maxBlur, Math.round(value));
}

function parseOpacity(raw: string, fallback = 100): number {
  // 键不存在（空串）时返回默认值，注意 Number('') 为 0 不能直接用
  if (!raw) return fallback;
  const value = Number(raw);

  if (!Number.isFinite(value)) return fallback;

  return Math.min(100, Math.max(0, Math.round(value)));
}

/**
 * 面板毛玻璃开关解析：Windows/macOS 未设置（空串）或值损坏时默认开启。
 * Linux 的 WebKitGTK 合成 backdrop-filter 的开销远高于 WebView2（每个模糊
 * 表面逐帧离屏重采样），未显式开启（"true"）时默认关闭；用户在设置里显式
 * 打开则尊重其选择。
 */
function parsePanelBlur(raw: string): boolean {
  if (isLinuxPlatform()) return raw === "true";

  return raw !== "false";
}

/** 毛玻璃强度解析：0-100，未设置用默认值 */
function parsePanelBlurStrength(raw: string): number {
  if (!raw) return DEFAULT_PANEL_BLUR_STRENGTH;
  const value = Number(raw);

  if (!Number.isFinite(value)) return DEFAULT_PANEL_BLUR_STRENGTH;

  return Math.min(100, Math.max(0, Math.round(value)));
}

/** 布尔配置解析：只有明确写 "true" 才算开启 */
function parseBoolFlag(raw: string): boolean {
  return raw === "true";
}

/** 场景壁纸分辨率倍数解析：0.25~3，未设置/损坏回落 1（窗口 CSS 像素） */
function parseSceneResolution(raw: string): number {
  const value = Number(raw);

  if (!Number.isFinite(value) || value <= 0) return 1;

  return Math.min(3, Math.max(0.25, value));
}

/** 场景壁纸刷新率上限解析："auto"/未设置 = 0（自适应）；否则 10~240 fps */
function parseSceneFps(raw: string): number {
  if (!raw || raw === "auto") return 0;
  const value = Number(raw);

  if (!Number.isFinite(value) || value <= 0) return 0;

  return Math.min(240, Math.max(10, Math.round(value)));
}

/**
 * 网页壁纸入口（项目内相对路径）→ 应用内资源 URL。
 *
 * 末段换成 __entry 别名而不是原样用 index.html：Wails 的 asset server 会把所有
 * 以 "/index.html" 结尾的请求当成启动器首页，往里注入 runtime/IPC 脚本、并把整份
 * HTML 过一遍解析器重新渲染（见 webwallpaper_handler.go 的说明），壁纸会被改坏。
 * 别名与入口同目录，所以壁纸里的相对路径照样解析得到。
 */
function toWebWallpaperUrl(entry: string): string {
  const parts = entry.replace(/^\/+/, "").split("/").filter(Boolean);

  if (parts.length === 0) return "";
  parts[parts.length - 1] = "__entry";

  return `/wwwallpaper/${parts.join("/")}`;
}

function toLocalUrl(path: string): string {
  return `/localfile?path=${encodeURIComponent(path)}`;
}

export const BackgroundProvider: React.FC<{ children: React.ReactNode }> = ({
  children,
}) => {
  const [mode, setMode] = useState<BackgroundMode>("none");
  const [path, setPath] = useState("");
  const [wallpaperPath, setWallpaperPath] = useState("");
  const [bingPath, setBingPath] = useState("");
  const [bingFailed, setBingFailed] = useState(false);
  const [wePath, setWePath] = useState("");
  const [weSource, setWeSource] = useState("");
  const [weWeb, setWeWeb] = useState("");
  const [weTitle, setWeTitle] = useState("");
  const [weType, setWeType] = useState("");
  const [weWebVersion, setWeWebVersion] = useState("");
  const [weScene, setWeScene] = useState<
    import("../lib/we-scene/types").WEScenePayload | null
  >(null);
  const [weUnsupported, setWeUnsupported] = useState(false);
  const [blur, setBlur] = useState(0);
  const [opacity, setOpacity] = useState(100);
  const [scrim, setScrim] = useState(80);
  const [windowOpacity, setWindowOpacity] = useState(100);
  const [acrylic, setAcrylic] = useState(false);
  const [panelBlur, setPanelBlur] = useState(!isLinuxPlatform());
  const [panelBlurStrength, setPanelBlurStrength] = useState(
    DEFAULT_PANEL_BLUR_STRENGTH,
  );
  const [webInteractive, setWebInteractive] = useState(false);
  const [sceneResolution, setSceneResolution] = useState(1);
  const [sceneFps, setSceneFps] = useState(0);
  const [hydrated, setHydrated] = useState(false);

  // 各读取自带 catch（失败回落默认值），Promise.all 必然 resolve。
  // 返回 Promise 供启动流程等待"配置全部落定"再显示窗口（见 WindowReveal）。
  const refresh = useCallback(() => {
    return Promise.all([
      GetValue(BACKGROUND_MODE_KEY)
        .then((value) => setMode(parseMode(value)))
        .catch(() => setMode("none")),
      GetValue(BACKGROUND_PATH_KEY)
        .then((value) => setPath(value || ""))
        .catch(() => setPath("")),
      GetValue(BACKGROUND_BLUR_KEY)
        .then((value) => setBlur(parseBlur(value)))
        .catch(() => setBlur(0)),
      GetValue(BACKGROUND_OPACITY_KEY)
        .then((value) => setOpacity(parseOpacity(value)))
        .catch(() => setOpacity(100)),
      GetValue(BACKGROUND_SCRIM_KEY)
        .then((value) => setScrim(parseOpacity(value, 80)))
        .catch(() => setScrim(80)),
      GetValue(BACKGROUND_WINDOW_OPACITY_KEY)
        .then((value) => setWindowOpacity(parseOpacity(value)))
        .catch(() => setWindowOpacity(100)),
      GetAcrylicBackdropEnabled()
        .then((value) => setAcrylic(!!value))
        .catch(() => setAcrylic(false)),
      GetValue(PANEL_BLUR_KEY)
        .then((value) => setPanelBlur(parsePanelBlur(value)))
        .catch(() => setPanelBlur(!isLinuxPlatform())),
      GetValue(PANEL_BLUR_STRENGTH_KEY)
        .then((value) => setPanelBlurStrength(parsePanelBlurStrength(value)))
        .catch(() => setPanelBlurStrength(DEFAULT_PANEL_BLUR_STRENGTH)),
      GetValue(WEB_WALLPAPER_INTERACTIVE_KEY)
        .then((value) => setWebInteractive(parseBoolFlag(value)))
        .catch(() => setWebInteractive(false)),
      GetValue(WE_SCENE_RESOLUTION_KEY)
        .then((value) => setSceneResolution(parseSceneResolution(value)))
        .catch(() => setSceneResolution(1)),
      GetValue(WE_SCENE_FPS_KEY)
        .then((value) => setSceneFps(parseSceneFps(value)))
        .catch(() => setSceneFps(0)),
    ]).then(() => undefined);
  }, []);

  // 面板毛玻璃开关 + 强度写到 <html>：data-panel-blur 由 globals.css 的属性选择器
  // 统一摘掉所有表面的 backdrop-filter 并提升底色不透明度；--nya-blur-scale 乘到
  // 各表面的 blur 半径上、--nya-glass-alpha 换掉底色不透明度，实现"强度"连续可调
  // （0 直接走关闭那条更省合成的路径）。
  // data-low-fx 标记低性能渲染路径（Linux WebKitGTK）：globals.css 据此停掉
  // 常驻的 paint/layout 型装饰动画（box-shadow/height/background-position）。
  useEffect(() => {
    const root = document.documentElement;
    const strength = panelBlur ? panelBlurStrength : 0;

    root.dataset.panelBlur = strength > 0 ? "on" : "off";
    root.dataset.lowFx = isLinuxPlatform() ? "true" : "false";
    root.style.setProperty(
      "--nya-blur-scale",
      String(panelBlurScale(strength)),
    );
    root.style.setProperty(
      "--nya-glass-alpha",
      String(panelGlassAlpha(strength)),
    );
  }, [panelBlur, panelBlurStrength]);

  useEffect(() => {
    void refresh().then(() => setHydrated(true));
  }, [refresh]);

  // wallpaper 模式：定期重读桌面壁纸，桌面换壁纸后启动器自动跟随
  useEffect(() => {
    if (mode !== "wallpaper") {
      setWallpaperPath("");

      return;
    }
    let alive = true;
    const sync = () => {
      GetDesktopWallpaperPath()
        .then((value) => {
          if (alive) setWallpaperPath(value || "");
        })
        .catch(() => {
          if (alive) setWallpaperPath("");
        });
    };

    sync();
    const timer = window.setInterval(sync, WALLPAPER_POLL_INTERVAL_MS);

    return () => {
      alive = false;
      window.clearInterval(timer);
    };
  }, [mode]);

  // bing 模式：取后端当日缓存（无则触发下载）；失败时回落 WebView 直连远程地址
  useEffect(() => {
    if (mode !== "bing") {
      setBingPath("");
      setBingFailed(false);

      return;
    }
    let alive = true;
    const sync = () => {
      GetBingDailyImagePath()
        .then((value) => {
          if (!alive) return;
          if (value) {
            setBingPath(value);
            setBingFailed(false);
          } else {
            setBingFailed(true);
          }
        })
        .catch(() => {
          if (alive) setBingFailed(true);
        });
    };

    sync();
    const timer = window.setInterval(sync, BING_REFRESH_INTERVAL_MS);

    return () => {
      alive = false;
      window.clearInterval(timer);
    };
  }, [mode]);

  // wallpaper-engine 模式：定期重读 WE 当前壁纸（WE 内切换壁纸后自动跟随）
  useEffect(() => {
    if (mode !== "wallpaper-engine") {
      setWePath("");
      setWeSource("");
      setWeWeb("");
      setWeWebVersion("");
      setWeTitle("");
      setWeType("");
      setWeScene(null);
      setWeUnsupported(false);

      return;
    }
    let alive = true;
    const sync = () => {
      GetWallpaperEngineWallpaper()
        .then((value) => {
          if (!alive) return;
          setWePath(value?.Path || "");
          setWeSource(value?.Source || "");
          setWeWeb(value?.Web || "");
          setWeWebVersion(value?.WebConfigVersion || "");
          setWeTitle(value?.Title || "");
          setWeType(value?.Type || "");
          setWeScene((value?.Scene as WEScenePayload | undefined) ?? null);
          setWeUnsupported(!!value?.Unsupported);
        })
        .catch((err) => {
          if (!alive) return;
          console.error("[WE] 获取壁纸失败:", err);
          setWePath("");
          setWeSource("");
          setWeWeb("");
          setWeWebVersion("");
          setWeTitle("");
          setWeType("");
          setWeScene(null);
          setWeUnsupported(false);
        });
    };

    sync();
    const timer = window.setInterval(sync, WALLPAPER_POLL_INTERVAL_MS);

    return () => {
      alive = false;
      window.clearInterval(timer);
    };
  }, [mode]);

  // WebView 无法直接读盘符路径，本地图片统一经 assetserver 的 /localfile 回退路由中转；
  // none 模式 url 为空串显示纯白底色；图源未就绪时同样留空（必应除外，走远程兜底）
  const localPath =
    mode === "wallpaper"
      ? wallpaperPath
      : mode === "bing"
        ? bingPath
        : mode === "wallpaper-engine"
          ? wePath
          : path;
  const url =
    mode === "none"
      ? ""
      : localPath
        ? toLocalUrl(localPath)
        : mode === "bing" && bingFailed
          ? BING_REMOTE_FALLBACK_URL
          : "";

  // WE 视频壁纸：直接播放原视频（清晰），预览图 url 保留在其后作为解码失败时的回退底图
  const videoUrl =
    mode === "wallpaper-engine" && weSource && weType.toLowerCase() === "video"
      ? toLocalUrl(weSource)
      : "";

  // WE 场景壁纸：完整渲染载荷交给背景层的 SceneWallpaperRenderer(three.js);
  // Path 里同时保留了静态提取图,作为资源加载期间与 WebGL 不可用时的兜底。
  // 载荷只在场景壁纸就绪时给出,其余类型为 null。
  const sceneWallpaper =
    mode === "wallpaper-engine" && weType.toLowerCase() === "scene"
      ? weScene
      : null;

  // WE 网页壁纸：入口 HTML 经 /wwwallpaper 路由交给背景层的 iframe；
  // 预览图仍在 url 里，作为 iframe 加载完成前的底图。
  // 属性指纹拼成 ?v= 参数:用户在 WE 里改配置后指纹变化,iframe 随 key 重挂载,
  // 新的用户属性随入口 HTML 重新注入(见后端 webwallpaper_polyfill.go)。
  const webUrl =
    mode === "wallpaper-engine" && weWeb
      ? toWebWallpaperUrl(weWeb) +
        (weWebVersion ? `?v=${encodeURIComponent(weWebVersion)}` : "")
      : "";

  return (
    <BackgroundContext.Provider
      value={{
        url,
        videoUrl,
        webUrl,
        webInteractive,
        sceneResolution,
        sceneFps,
        blur,
        opacity,
        scrim,
        mode,
        isCustom: !!path,
        windowOpacity,
        acrylic,
        panelBlur,
        panelBlurStrength,
        wallpaperPath,
        wallpaperEngineTitle: weTitle,
        wallpaperEngineType: weType,
        wallpaperEngineWebVersion: weWebVersion,
        sceneWallpaper,
        wallpaperEngineUnsupported: weUnsupported,
        hydrated,
        refresh,
      }}
    >
      {children}
    </BackgroundContext.Provider>
  );
};
