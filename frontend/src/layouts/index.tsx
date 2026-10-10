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
import type { launch } from "../../wailsjs/go/models";

import React, { useEffect, useRef, useState } from "react";
import { Button, Checkbox, Modal, ModalContent } from "@heroui/react";
import { ArrowMinimize20Regular, Power20Regular } from "@fluentui/react-icons";

import { TitleBar } from "../components/title-bar.tsx";
import AutoUpdateNotice from "../components/AutoUpdateNotice";
import DownloadCenter from "../components/download/DownloadCenter";
import FileDropOverlay from "../components/FileDropOverlay";
import ErrorBoundary from "../components/ErrorBoundary";
import SceneWallpaperRenderer from "../components/SceneWallpaperRenderer";
import MicrosoftLoginProgress from "../components/microsoft-login-progress";
import LaunchFXOverlay from "../components/launch/LaunchFXOverlay";
import { ModalShell, modalBehaviorProps } from "../components/modal-shell";
import SwitchTransition, {
  useSwitchDirection,
} from "../components/screen-transition";
import { SetValue } from "../../wailsjs/go/bindings/ConfigAPI";
import {
  ExitLauncher,
  HideLauncher,
  TraySupported,
} from "../../wailsjs/go/bindings/SystemAPI";
import { EventsOn } from "../../wailsjs/runtime/runtime";
import { onNavigate } from "../lib/navigation";
import { prefetchIdlePages } from "../lib/lazy";
import { startAudioBridge } from "../lib/audioBridge";
import { startRepaintOnRestore } from "../lib/repaint";
import { notify } from "../components/overlay/dialog";
import { notifyCrashIfNeeded } from "../lib/crashNotice";
import { GetLaunchSnapshot } from "../../wailsjs/go/bindings/LauncherAPI";
import { extractThemeColor } from "../lib/monet";
import { isLinuxPlatform } from "../lib/platform";
import {
  DEFAULT_PAGE_ID,
  hydratePluginGrants,
  loadPlugins,
  PageHost,
} from "../plugin";
import { t } from "../i18n";
import { useThemeColor } from "../theme-color";

import { BackgroundProvider, useBackground } from "./background";
import Sidebar from "./Sidebar";
import {
  SimpleModeProvider,
  useShellPages,
  useSimpleMode,
} from "./simple-mode";
import {
  SidebarSettingsProvider,
  useSidebarSettings,
} from "./sidebar-settings";

// 底色层：全局背景至少垫一层主题底色，模糊/半透明的背景图叠在它上面混色，
// 而不是直接和透明窗口外的桌面混。窗口不透明度只作用在这一层——越低桌面
// 越透出来；背景层自身的"背景不透明度"不再乘窗口不透明度。
const BaseColorLayer: React.FC = () => {
  const { windowOpacity } = useBackground();

  return (
    <div
      className="absolute inset-0 z-0 pointer-events-none"
      style={{
        backgroundColor: `rgb(var(--nya-shell) / ${windowOpacity / 100})`,
      }}
    />
  );
};

// 背景层：图源/不透明度/模糊均由设置页驱动（layouts/background.tsx）；
// 负 margin + 放大，避免 blur 在边缘露出透明缝隙；url 为空（纯白/图源未就绪）时不渲染。
// WE 视频壁纸额外铺一层 <video> 播放原视频，解码失败时回落到底层预览图；
// WE 网页壁纸铺一层 sandbox iframe（入口经 /wwwallpaper 路由提供）；
// WE 场景壁纸铺一层 SceneWallpaperRenderer（three.js 完整渲染：多图层/动画/粒子），
// 场景首帧就绪前保留静态提取图垫底，失败则一直用静态图。
// 底色由 BaseColorLayer 单独垫底，这里只管"背景不透明度"自身的混色。
const BackgroundLayer: React.FC = () => {
  const {
    url,
    videoUrl,
    webUrl,
    webInteractive,
    blur,
    opacity,
    scrim,
    sceneWallpaper,
    sceneResolution,
    sceneFps,
  } = useBackground();
  const [videoFailed, setVideoFailed] = useState(false);
  // 场景渲染失败(如 WebGL 不可用)时永久回退静态图,避免轮询反复重建
  const [sceneFailed, setSceneFailed] = useState(false);
  // 场景首帧就绪后隐藏静态垫底图,省一份重复绘制
  const [sceneReady, setSceneReady] = useState(false);
  const activeScene = sceneWallpaper && !sceneFailed ? sceneWallpaper : null;

  useEffect(() => {
    setVideoFailed(false);
  }, [videoUrl]);
  useEffect(() => {
    // 换壁纸载荷时重置就绪标记,让新场景同样经历"静态图垫底 → 首帧接管"
    setSceneReady(false);
    setSceneFailed(false);
  }, [sceneWallpaper?.Entry]);

  // 压暗度只管 scrim 罩色强度，模糊只由"背景模糊"滑杆控制，两个滑杆互不影响
  // Linux 的 WebKitGTK 对动态图源（视频/网页/场景壁纸）做全屏 blur 意味着逐帧
  // 重栅格化整窗，成本爆炸：动态背景下一律关掉模糊；静态图是一次性栅格化可
  // 缓存，保留（半径已在 background.tsx 里压到 16px）。
  const totalBlur =
    isLinuxPlatform() && (videoUrl || webUrl || activeScene) ? 0 : blur;

  if (!url && !videoUrl && !webUrl && !activeScene) return null;

  return (
    <div
      className="absolute -inset-4 z-0 overflow-hidden pointer-events-none"
      style={{
        filter: totalBlur > 0 ? `blur(${totalBlur}px)` : undefined,
        opacity: opacity / 100,
      }}
    >
      {url && !sceneReady && (
        <div
          // key 随图源变化 → 换图时重挂载、重放淡入，避免硬切闪一下
          key={url}
          className="absolute inset-0 bg-cover bg-center bg-no-repeat nya-bg-fade"
          style={{ backgroundImage: `url("${url}")` }}
        />
      )}
      {activeScene && (
        <SceneWallpaperRenderer
          fpsCap={sceneFps}
          payload={activeScene}
          pixelRatio={sceneResolution}
          onError={() => setSceneFailed(true)}
          onReady={() => setSceneReady(true)}
        />
      )}
      {videoUrl && !videoFailed && (
        <video
          key={videoUrl}
          autoPlay
          loop
          muted
          playsInline
          className="absolute inset-0 h-full w-full object-cover nya-bg-fade"
          src={videoUrl}
          onError={() => setVideoFailed(true)}
        />
      )}
      {webUrl && (
        // 网页壁纸是创意工坊里的任意 HTML：sandbox 配置需要平衡安全与功能。
        // allow-scripts + allow-same-origin: Canvas/WebGL/AudioContext 等 API 在
        //   opaque origin 下大量受限(部分壁纸直接空白),WE 壁纸本身也是全功能
        //   运行,这里保持同源;启动器自身不存放敏感凭据到 window 上。
        // allow-modals: 部分壁纸用 alert 排错,不给会抛异常。
        // allow-forms: 少量壁纸带搜索/表单控件(文档兼容性要求)。
        // 不给 allow-popups/allow-top-navigation:壁纸不该能开窗口或跳走宿主。
        // 用户属性与 WE API polyfill 由后端注入入口 HTML(先于壁纸脚本执行),
        // ?v= 是属性指纹,用户在 WE 里改配置后 iframe 重挂载、新配置生效。
        <iframe
          key={webUrl}
          className={`absolute inset-0 h-full w-full border-0 nya-bg-fade ${
            webInteractive ? "pointer-events-auto" : "pointer-events-none"
          }`}
          sandbox="allow-scripts allow-modals allow-same-origin allow-forms"
          src={webUrl}
          title={t("网页壁纸")}
        />
      )}
      {/*黑白 scrim：亮色白/暗色黑，强度由设置页"背景压暗度"控制（默认 80%），
       * 把任意壁纸压到接近主题明暗度，保证前景文字可读 */}
      <div
        className="absolute inset-0 nya-bg-scrim"
        style={{ opacity: scrim / 100 }}
      />
    </div>
  );
};

/**
 * 背景取色桥接：主题色设为"跟随背景"时，从当前背景图里提一个莫奈式主色写回
 * ThemeColorProvider。放在 Layouts 里是因为背景上下文只在这一层之上；
 * 取不到（跨域受限 / 纯灰阶壁纸）时什么都不做，保留用户原来的颜色。
 */
const BackgroundMonetBridge: React.FC = () => {
  const { url } = useBackground();
  const { source, extractNonce, setExtractedColor } = useThemeColor();

  useEffect(() => {
    if (source !== "background" || !url) return;
    let alive = true;

    void extractThemeColor(url).then((result) => {
      if (alive && result) setExtractedColor(result.hex);
    });

    return () => {
      alive = false;
    };
  }, [url, source, extractNonce, setExtractedColor]);

  return null;
};

const Layouts: React.FC = () => {
  return (
    <BackgroundProvider>
      <SidebarSettingsProvider>
        <SimpleModeProvider>
          <BackgroundMonetBridge />
          <Shell />
        </SimpleModeProvider>
      </SidebarSettingsProvider>
    </BackgroundProvider>
  );
};

/**
 * 治启动闪屏的"揭幕"组件：窗口由 main.go 的 HideWindowOnStart 隐藏创建，
 * 等两处启动配置（背景不透明度/模式 + 侧边栏布局/自动隐藏）都 hydrate 完、
 * 首屏 DOM 定型后再显示窗口，用户看到的第一眼就是最终样子——不再经历
 * "空白透明窗 → UI 弹出 → 不透明度突变 → 布局跳动"的全过程。
 *
 * 时机注意：
 * - 不能用 requestAnimationFrame 等首帧：窗口隐藏时 WebView2 不合成帧，
 *   rAF 回调永远不触发，窗口会一直藏着（只能等 Go 侧 5s 兜底）。
 *   setTimeout 在隐藏页里至多被节流到 1Hz，仍然可靠。
 * - 略留 50ms 缓冲，让 hydration 引发的重渲染（含各模式取图 effect 的
 *   发起）先提交，Show 后 WebView 立即合成最终布局。
 * - 浏览器里直接跑 Vite dev（无 Wails runtime）时 Show 不存在，安全跳过。
 */
const WindowReveal: React.FC = () => {
  const { hydrated: backgroundHydrated } = useBackground();
  const { hydrated: sidebarHydrated } = useSidebarSettings();
  // S 模式改变侧边栏构成与首屏，同样要等它落定再揭幕
  const { hydrated: simpleModeHydrated } = useSimpleMode();
  const revealed = useRef(false);

  useEffect(() => {
    if (
      !backgroundHydrated ||
      !sidebarHydrated ||
      !simpleModeHydrated ||
      revealed.current
    )
      return;
    revealed.current = true;
    const timer = window.setTimeout(() => {
      window.runtime?.Show?.();
    }, 50);

    return () => window.clearTimeout(timer);
  }, [backgroundHydrated, sidebarHydrated, simpleModeHydrated]);

  return null;
};

// Shell 主框架：底色单独垫在 BaseColorLayer（见上），根容器保持透明，
// 桌面透出程度由窗口不透明度决定（窗口创建为可透明，见 main.go）。
const Shell: React.FC = () => {
  const [activeKey, setActiveKey] = useState<string>(DEFAULT_PAGE_ID);
  // 自动隐藏时内容区顶到窗口边缘，侧边栏改为悬浮弹出、不再占位
  const { autoHide, placement, style } = useSidebarSettings();
  // 订阅页面列表：S 模式下只剩五个页面，主页改标「启动」；插件加载后自动跟上
  const pages = useShellPages();

  // ---- 点 X 的关闭询问（后端 OnBeforeClose 分发为 launcher:close-requested） ----
  const [closeAskOpen, setCloseAskOpen] = useState(false);
  const [rememberClose, setRememberClose] = useState(false);
  // 平台有没有系统托盘（macOS 等为 false）：没有托盘时"最小化到托盘"会把
  // 窗口藏进死路（托盘菜单是唯一唤回入口），所以不提供这个按钮。
  const [traySupported, setTraySupported] = useState(false);

  useEffect(() => {
    void TraySupported()
      .then((supported) => setTraySupported(supported === true))
      .catch(() => setTraySupported(false));
  }, []);

  // 窗口从最小化/遮挡恢复后强制重建合成层：WebView2 在窗口被遮挡期间可能丢掉
  // backdrop-filter 表面与整窗背景层的光栅缓存，恢复时不重建，表现为"卡片变透明、
  // 直接透出桌面"。详见 lib/repaint.ts。
  useEffect(() => startRepaintOnRestore(), []);

  useEffect(
    () => EventsOn("launcher:close-requested", () => setCloseAskOpen(true)),
    [],
  );

  const answerClose = async (action: "tray" | "exit") => {
    const remember = rememberClose;

    setCloseAskOpen(false);
    setRememberClose(false);
    try {
      if (remember) await SetValue("closeAction", action);
    } catch {
      /* 记住失败就下次继续询问 */
    }
    try {
      if (action === "exit") await ExitLauncher();
      else await HideLauncher();
    } catch {
      /* 后端兜底：调用失败窗口保持原样 */
    }
  };

  // 插件在首帧之后加载，避免拖慢启动；注册表变更后界面自动跟上
  useEffect(() => {
    // 先水合权限开关再加载插件：插件激活时就会调 API，那时候授权表必须已经是
    // 用户当前的选择（否则默认值会在启动瞬间短暂放行）
    void hydratePluginGrants().finally(() => loadPlugins());
    // 页面 chunk 空闲预取：首屏定型后逐个预热懒加载页面，切页零等待
    prefetchIdlePages();
    // 音频桥在应用根启动（幂等）：只靠主页卡片/音乐页挂载时启动的话，
    // 首屏不是主页时 Go 侧 PlayTrack 发出的 music:play 会无人接
    startAudioBridge();
  }, []);

  // NekoSolo 首启补全部分失败：后端会保住待装标记下次启动重试，
  // 这里把失败项浮到底部警示条，别让"mod 没装全"只有日志知道
  useEffect(
    () =>
      EventsOn(
        "solo:completionIssue",
        (payload: { pack?: string; total?: number; failures?: string[] }) => {
          const detail = (payload?.failures ?? []).slice(0, 2).join("；");

          notify.warning(
            t("整合包「{0}」有 {1} 项内容安装失败，下次启动将自动重试。", {
              "0": payload?.pack || "NekoSolo",
              "1": String(payload?.total ?? 0),
            }) + (detail ? ` ${detail}` : ""),
            8000,
          );
        },
      ),
    [],
  );

  // 崩溃提示在应用根全局订阅（通知函数自带 Revision 去重）：
  // 放在主页的话切页即卸载，从实例页启动后崩溃的弹窗会永久丢失
  useEffect(() => {
    // 应用启动时也补查一次快照：覆盖"事件先于订阅到达"的窗口
    void GetLaunchSnapshot()
      .then((snapshot) => notifyCrashIfNeeded(snapshot))
      .catch(() => undefined);

    return EventsOn("launch:changed", (snapshot: launch.GameLaunchSnapshot) => {
      notifyCrashIfNeeded(snapshot);
    });
  }, []);

  // 主页小组件等通过导航总线请求切页
  useEffect(() => onNavigate((request) => setActiveKey(request.pageId)), []);

  // 当前页未注册（插件被禁用/卸载）时回落到默认页
  const activePage =
    pages.find((page) => page.id === activeKey) ??
    pages.find((page) => page.id === DEFAULT_PAGE_ID);

  // 切换方向按侧边栏顺序推导：往后面的页面切则新内容从右滑入，反之从左
  const activeIndex = activePage
    ? pages.findIndex((page) => page.id === activePage.id)
    : -1;
  const direction = useSwitchDirection(activeIndex);

  return (
    <div className="relative flex h-screen w-screen overflow-hidden text-gray-900 dark:text-gray-100">
      {/*底色层（窗口不透明度作用在这层）+ 全局背景层（图源/透明度/模糊）*/}
      <BaseColorLayer />
      <BackgroundLayer />

      {/*全局标题栏，也就是窗口标题*/}
      <TitleBar title="NekoLauncher" />

      {/*标题栏下方的内容区（背景层从标题栏一直铺到底部）*/}
      <div className="relative flex flex-1 w-full pt-10 z-[1]">
        {/*全局左侧栏*/}
        <Sidebar activeKey={activeKey} onNavigate={setActiveKey} />

        {/*内容区（自动隐藏时不再为收起的侧边栏留位；按停靠边让位：
         * 岛式 = 面板尺寸 + 12px 边距，陆式 = 贴边只留面板尺寸）*/}
        <div
          className={`flex-1 relative overflow-hidden ${
            autoHide
              ? ""
              : placement === "left"
                ? style === "land"
                  ? "ml-16"
                  : "ml-[76px]"
                : placement === "right"
                  ? style === "land"
                    ? "mr-16"
                    : "mr-[76px]"
                  : placement === "top"
                    ? style === "land"
                      ? "mt-14"
                      : "mt-[68px]"
                    : style === "land"
                      ? "mb-14"
                      : "mb-[68px]"
          }`}
        >
          {/* 按页 key 做滑动切换；页面出错只丢这一页，不会把整个界面带走 */}
          <SwitchTransition
            activeKey={activePage?.id ?? "missing"}
            className="h-full w-full"
            direction={direction}
          >
            <ErrorBoundary title={activePage?.label}>
              {activePage ? <PageHost definition={activePage} /> : null}
            </ErrorBoundary>
          </SwitchTransition>
        </div>
      </div>

      {/*全局下载中心（跨页面常驻：游戏安装 + 内容下载统一展示，类 KDE 通知样式）*/}
      <DownloadCenter onOpenDownloads={() => setActiveKey("download")} />

      {/*启动特效：游戏进程拉起瞬间全窗口庆祝动画（pointer-events-none，纯观看）*/}
      <LaunchFXOverlay />

      {/*全局文件拖放安装：拖 .jar / .zip / .mrpack 进窗口即可装进实例*/}
      <FileDropOverlay />

      {/*内嵌微软登录进度（登录页跳转往返会重载 SPA，浮层全局挂载接力显示）*/}
      <MicrosoftLoginProgress />

      {/*启动时自动更新提示：后端查到新版本广播 update:available，这里弹窗确认*/}
      <AutoUpdateNotice />

      {/*启动闪屏治理：配置落定后揭幕窗口（见组件注释）*/}
      <WindowReveal />

      {/*点 X 时的关闭询问：最小化到托盘 / 退出，可记住选择（设置页可改）*/}
      <Modal
        isOpen={closeAskOpen}
        size="sm"
        onClose={() => setCloseAskOpen(false)}
        {...modalBehaviorProps}
      >
        <ModalContent>
          <ModalShell
            icon={<Power20Regular />}
            title={t("关闭启动器")}
            onClose={() => setCloseAskOpen(false)}
          >
            <div className="flex flex-col gap-3">
              <Checkbox
                isSelected={rememberClose}
                size="sm"
                onValueChange={setRememberClose}
              >
                {t("记住我的选择")}
              </Checkbox>
              <div className="flex flex-wrap justify-end gap-2">
                {traySupported ? (
                  <Button
                    startContent={<ArrowMinimize20Regular />}
                    variant="flat"
                    onPress={() => void answerClose("tray")}
                  >
                    {t("最小化到托盘")}
                  </Button>
                ) : null}
                <Button
                  color="danger"
                  startContent={<Power20Regular />}
                  variant="flat"
                  onPress={() => void answerClose("exit")}
                >
                  {t("退出启动器")}
                </Button>
              </div>
              <div className="text-[11px] text-gray-400">
                {t("记住后可在「设置 → 启动器行为」中修改。")}
              </div>
            </div>
          </ModalShell>
        </ModalContent>
      </Modal>
    </div>
  );
};

export default Layouts;
