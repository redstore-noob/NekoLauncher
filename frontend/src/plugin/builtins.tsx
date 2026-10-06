/*
 * 内置扩展点：20 个主页小组件 + 10 个侧边栏页面。
 *
 * 内置实现与第三方插件走完全相同的注册通道（registerWidget / registerPage），
 * 区别只是没有 "<插件id>:" 前缀——这几个 id 已经写在用户的 launcher.yaml 里，
 * 改名会让既有布局失效，故保持不变（下线组件时在 lib/home.ts 配置 id 迁移）。
 */
import {
  AnimalCat20Regular,
  Apps20Regular,
  ArrowDownload20Regular,
  CalendarClock20Regular,
  CalendarLtr20Regular,
  Chat20Regular,
  Clock20Regular,
  DocumentText20Regular,
  Flag20Regular,
  FoodFish20Regular,
  Globe20Regular,
  HardDrive20Regular,
  History20Regular,
  Home20Regular,
  Image20Regular,
  MusicNote220Regular,
  PaintBrush20Regular,
  Person20Regular,
  Pulse20Regular,
  PuzzleCube20Regular,
  Rss20Regular,
  Server20Regular,
  Settings20Regular,
  Star20Regular,
  Timer20Regular,
  TopSpeed20Regular,
  Wand20Regular,
  WindowDevTools20Regular,
} from "@fluentui/react-icons";

import NekoAgentIcon from "../components/neko-agent-icon";
import CalendarCard from "../components/home/CalendarCard";
import ClockCard from "../components/home/ClockCard";
import CountdownCard from "../components/home/CountdownCard";
import DailyLuckCard from "../components/home/DailyLuckCard";
import DiskCard from "../components/home/DiskCard";
import DownloadCard from "../components/home/DownloadCard";
import FavoriteInstancesCard from "../components/home/FavoriteInstancesCard";
import GoalCard from "../components/home/GoalCard";
import HitokotoCard from "../components/home/HitokotoCard";
import JavaCard from "../components/home/JavaCard";
import LaunchLogCard from "../components/home/LaunchLogCard";
import MascotCard from "../components/home/MascotCard";
import MemoryCard from "../components/home/MemoryCard";
import MuyuCard from "../components/home/MuyuCard";
import MusicControlCard from "../components/home/MusicControlCard";
import NetworkCard from "../components/home/NetworkCard";
import PerformanceCard from "../components/home/PerformanceCard";
import PlaytimeCard from "../components/home/PlaytimeCard";
import HostedServerCard from "../components/home/HostedServerCard";
import QuickDownloadCard from "../components/home/QuickDownloadCard";
import QuickServerCard from "../components/home/QuickServerCard";
import QuickSettingsCard from "../components/home/QuickSettingsCard";
import RecentWorldsCard from "../components/home/RecentWorldsCard";
import RssFeedCard from "../components/home/RssFeedCard";
import RewindCard from "../components/home/RewindCard";
import ScreenshotWallCard from "../components/home/ScreenshotWallCard";
import SkinViewCard from "../components/home/SkinViewCard";
import AppearanceQuickCard from "../components/home/AppearanceQuickCard";
import HomePage from "../layouts/home";
import { t } from "../i18n";
import { lazyPage } from "../lib/lazy";

// vendored 插件（路一收编）：CSS 随主包静态分发，import 即接线
import "../plugins-vendored/tno-ui";

import { registerPage, registerWidget } from "./registry";

/*
 * 除主页外的页面全部切成懒加载 chunk（见 lib/lazy.ts）：首包只带壳层与主页，
 * 其余在首帧后的空闲期预取，真正切换过去时已在内存中，无感直达。
 */
const AccountPage = lazyPage(() => import("../layouts/account"));
const AiPage = lazyPage(() => import("../layouts/ai"));
const AppearancePage = lazyPage(() => import("../layouts/appearance"));
const CreatorPage = lazyPage(() => import("../layouts/creator"));
const DownloadPage = lazyPage(() => import("../layouts/download"));
const InstancesPage = lazyPage(() => import("../layouts/instances"));
const MusicPage = lazyPage(() => import("../layouts/music"));
const OnlinePageWrapper = lazyPage(() =>
  import("../layouts/multiplayer").then((m) => ({
    default: m.OnlinePageWrapper,
  })),
);
const PluginsPage = lazyPage(() => import("../layouts/plugins"));
const ServersPageWrapper = lazyPage(() =>
  import("../layouts/multiplayer").then((m) => ({
    default: m.ServersPageWrapper,
  })),
);
const SettingsPage = lazyPage(() => import("../layouts/settings"));

let registered = false;

/** registerBuiltins 注册全部内置小组件与页面；重复调用无副作用 */
export function registerBuiltins() {
  if (registered) return;
  registered = true;

  registerWidget({
    id: "playtime",
    title: t("游玩统计"),
    description: t("总时长与玩得最久的实例排行"),
    icon: <Timer20Regular />,
    tileClass: "from-cyan-400 via-blue-500 to-purple-500 shadow-cyan-500/30",
    render: (context) => (
      <PlaytimeCard
        isGameRunning={context.isGameRunning}
        records={context.playtimeRecords}
      />
    ),
  });

  registerWidget({
    id: "worlds",
    title: t("最近存档"),
    description: t("按上次游玩时间列出存档，可直接接着玩"),
    icon: <Globe20Regular />,
    tileClass: "from-emerald-400 via-teal-500 to-cyan-500 shadow-teal-500/30",
    render: (context) => (
      <RecentWorldsCard
        isBusy={context.isBusy}
        reloadKey={context.launchPhase}
        selectedVersion={context.selectedVersion}
        onLaunchWorld={context.onLaunchWorld}
        onSelectVersion={context.onSelectVersion}
      />
    ),
  });

  registerWidget({
    id: "quickjoin",
    title: t("快速进服"),
    description: t("多服务器列表：彩色 MOTD、人数与延迟，一键进服"),
    icon: <Server20Regular />,
    tileClass: "from-sky-400 via-indigo-500 to-violet-500 shadow-indigo-500/30",
    render: (context) => (
      <QuickServerCard isBusy={context.isBusy} onJoin={context.onJoin} />
    ),
  });

  registerWidget({
    id: "hosted-server",
    title: t("托管服务器"),
    description: t("本机托管的服务器：选择查看状态，可直接启停"),
    icon: <Server20Regular />,
    tileClass:
      "from-teal-400 via-emerald-500 to-green-500 shadow-emerald-500/30",
    render: () => <HostedServerCard />,
  });

  registerWidget({
    id: "log",
    title: t("运行日志"),
    description: t("实时输出，退出后推测可能的原因"),
    icon: <DocumentText20Regular />,
    tileClass: "from-slate-400 via-gray-500 to-zinc-600 shadow-gray-500/30",
    render: (context) => (
      <LaunchLogCard
        phase={context.launchPhase}
        revision={context.launchRevision}
      />
    ),
  });

  registerWidget({
    id: "memory",
    title: t("内存监控"),
    description: t("启动器与 JVM 内存占用、Java 进程数"),
    icon: <Pulse20Regular />,
    tileClass: "from-fuchsia-400 via-pink-500 to-rose-500 shadow-pink-500/30",
    render: () => <MemoryCard />,
  });

  registerWidget({
    id: "rss",
    title: t("RSS 订阅"),
    description: t("订阅任意 RSS / Atom 源，点击条目打开原文"),
    icon: <Rss20Regular />,
    tileClass:
      "from-orange-400 via-amber-500 to-yellow-500 shadow-amber-500/30",
    render: () => <RssFeedCard />,
  });

  registerWidget({
    id: "performance",
    title: t("性能监控"),
    description: t("CPU / GPU / 内存占用折线图，任务管理器同款"),
    icon: <TopSpeed20Regular />,
    tileClass: "from-blue-400 via-cyan-500 to-teal-500 shadow-cyan-500/30",
    render: () => <PerformanceCard />,
  });

  registerWidget({
    id: "luck",
    title: t("今日运气"),
    description: t("日期 + 设备码决定的每日运势，每天 0 点重摇"),
    icon: <CalendarLtr20Regular />,
    tileClass: "from-amber-400 via-orange-500 to-rose-500 shadow-orange-500/30",
    render: () => <DailyLuckCard />,
  });

  registerWidget({
    id: "muyu",
    title: t("敲木鱼"),
    description: t("电子木鱼，敲一下积一点功德"),
    icon: <FoodFish20Regular />,
    tileClass: "from-amber-600 via-amber-500 to-yellow-500 shadow-amber-500/30",
    render: () => <MuyuCard />,
  });

  registerWidget({
    id: "mascot",
    title: t("看板娘"),
    description: t("点一下会弹跳，气泡里冒出随机的话"),
    icon: <AnimalCat20Regular />,
    tileClass: "from-lime-400 via-green-500 to-emerald-500 shadow-green-500/30",
    render: () => <MascotCard />,
  });

  registerWidget({
    id: "music",
    title: t("音乐控制"),
    description: t("当前播放曲目与快捷播放控制"),
    icon: <MusicNote220Regular />,
    tileClass:
      "from-violet-400 via-purple-500 to-fuchsia-500 shadow-purple-500/30",
    render: () => <MusicControlCard />,
  });

  registerWidget({
    id: "skin",
    title: t("皮肤展示"),
    description: t("当前账号的 3D 皮肤预览（双层皮肤 / 披风）"),
    icon: <Person20Regular />,
    tileClass:
      "from-lime-400 via-emerald-500 to-teal-500 shadow-emerald-500/30",
    render: (context) => <SkinViewCard context={context} />,
  });

  registerWidget({
    id: "download",
    title: t("下载任务"),
    description: t("主页查看 Minecraft/实例下载进度、速度与暂停控制"),
    icon: <ArrowDownload20Regular />,
    tileClass: "from-sky-400 via-blue-500 to-indigo-500 shadow-blue-500/30",
    render: () => <DownloadCard />,
  });

  registerWidget({
    id: "java",
    title: t("Java 环境"),
    description: t("已检测的 Java 版本一览，一键前往 Java 下载页"),
    icon: <WindowDevTools20Regular />,
    tileClass:
      "from-orange-400 via-amber-500 to-yellow-500 shadow-amber-500/30",
    render: () => <JavaCard />,
  });

  registerWidget({
    id: "disk",
    title: t("磁盘空间"),
    description: t("监控 .minecraft 所在硬盘的剩余空间，快满时变红提醒"),
    icon: <HardDrive20Regular />,
    tileClass: "from-slate-400 via-gray-500 to-zinc-600 shadow-gray-500/30",
    render: () => <DiskCard />,
  });

  registerWidget({
    id: "network",
    title: t("网络状态"),
    description: t("BMCLAPI / 官方源延迟与可用性，点进下载设置"),
    icon: <Globe20Regular />,
    tileClass: "from-cyan-400 via-sky-500 to-blue-500 shadow-sky-500/30",
    render: () => <NetworkCard />,
  });

  registerWidget({
    id: "hitokoto",
    title: t("一言"),
    description: t("Hitokoto 每日一言，可刷新换一条"),
    icon: <Chat20Regular />,
    tileClass: "from-rose-400 via-pink-500 to-fuchsia-500 shadow-pink-500/30",
    render: () => <HitokotoCard />,
  });

  registerWidget({
    id: "countdown",
    title: t("假期倒计时"),
    description: t("距离周末还有几天，以及最近几个假期"),
    icon: <CalendarClock20Regular />,
    tileClass: "from-teal-400 via-emerald-500 to-green-500 shadow-teal-500/30",
    render: () => <CountdownCard />,
  });

  registerWidget({
    id: "clock",
    title: t("时钟"),
    description: t("数字时钟与日期星期，秒级跳动"),
    icon: <Clock20Regular />,
    tileClass: "from-indigo-400 via-blue-500 to-cyan-500 shadow-blue-500/30",
    render: () => <ClockCard />,
  });

  registerWidget({
    id: "calendar",
    title: t("日历"),
    description: t("月历视图，节假日红字标记"),
    icon: <CalendarLtr20Regular />,
    tileClass:
      "from-violet-400 via-purple-500 to-fuchsia-500 shadow-purple-500/30",
    render: () => <CalendarCard />,
  });

  registerWidget({
    id: "favorites",
    title: t("常用实例"),
    description: t("手动置顶的实例快捷启动，可收藏当前实例"),
    icon: <Star20Regular />,
    tileClass:
      "from-yellow-400 via-amber-500 to-orange-500 shadow-amber-500/30",
    render: (context) => (
      <FavoriteInstancesCard
        isBusy={context.isBusy}
        selectedVersion={context.selectedVersion}
        onLaunchVersion={(versionId) => context.onLaunchWorld(versionId, "")}
      />
    ),
  });

  registerWidget({
    id: "goal",
    title: t("成就目标"),
    description: t("自定义游玩时长目标（累计/本周），进度条跟进"),
    icon: <Flag20Regular />,
    tileClass:
      "from-violet-400 via-purple-500 to-indigo-500 shadow-purple-500/30",
    render: (context) => (
      <GoalCard
        isGameRunning={context.isGameRunning}
        records={context.playtimeRecords}
      />
    ),
  });

  registerWidget({
    id: "rewind",
    title: t("时光回溯"),
    description: t("快照总数、存储占用与最近快照时间线，直达实例页管理"),
    icon: <History20Regular />,
    tileClass: "from-emerald-400 via-teal-500 to-cyan-500 shadow-teal-500/30",
    render: () => <RewindCard />,
  });

  registerWidget({
    id: "screenshots",
    title: t("截图墙"),
    description: t("轮播当前实例的截图，点击可打开"),
    icon: <Image20Regular />,
    tileClass: "from-fuchsia-400 via-pink-500 to-rose-500 shadow-rose-500/30",
    render: () => <ScreenshotWallCard />,
  });

  registerWidget({
    id: "quick-download",
    title: t("快速下载"),
    description: t("一键前往下载页，附下载设置与 Java 下载直达"),
    icon: <ArrowDownload20Regular />,
    tileClass: "from-sky-400 via-blue-500 to-indigo-500 shadow-blue-500/30",
    render: () => <QuickDownloadCard />,
  });

  registerWidget({
    id: "quick-settings",
    title: t("快速设置"),
    description: t("常用设置分区直达：启动 / 内存 / Java / 下载 / 网络"),
    icon: <Settings20Regular />,
    tileClass: "from-slate-400 via-gray-500 to-zinc-600 shadow-gray-500/30",
    render: () => <QuickSettingsCard />,
  });

  registerWidget({
    id: "appearance-quick",
    title: t("外观调节"),
    description: t("毛玻璃开关与窗口 / 壁纸不透明度、背景模糊即时调节"),
    icon: <PaintBrush20Regular />,
    tileClass:
      "from-fuchsia-400 via-purple-500 to-indigo-500 shadow-purple-500/30",
    render: () => <AppearanceQuickCard />,
  });

  registerPage({
    id: "home",
    label: t("主页"),
    icon: <Home20Regular />,
    order: 10,
    render: () => <HomePage />,
  });

  registerPage({
    id: "account",
    label: t("账户"),
    icon: <Person20Regular />,
    order: 20,
    render: () => <AccountPage />,
  });

  registerPage({
    id: "download",
    label: t("下载"),
    icon: <ArrowDownload20Regular />,
    order: 30,
    render: () => <DownloadPage />,
  });

  registerPage({
    id: "instances",
    label: t("实例"),
    icon: <Apps20Regular />,
    order: 40,
    render: () => <InstancesPage />,
  });

  registerPage({
    id: "servers",
    label: t("服务器"),
    icon: <Server20Regular />,
    order: 44,
    render: () => <ServersPageWrapper />,
  });

  registerPage({
    id: "online",
    label: t("联机"),
    icon: <Globe20Regular />,
    order: 46,
    render: () => <OnlinePageWrapper />,
  });

  registerPage({
    id: "music",
    label: t("音乐"),
    icon: <MusicNote220Regular />,
    order: 50,
    render: () => <MusicPage />,
  });

  registerPage({
    id: "plugins",
    label: t("插件"),
    icon: <PuzzleCube20Regular />,
    order: 60,
    render: () => <PluginsPage />,
  });

  registerPage({
    id: "creator",
    label: t("创作中心"),
    icon: <Wand20Regular />,
    order: 65,
    render: () => <CreatorPage />,
  });

  registerPage({
    id: "ai",
    label: t("NekoAgent喵"),
    icon: <NekoAgentIcon className="h-5 w-5" />,
    order: 68,
    render: () => <AiPage />,
  });

  registerPage({
    id: "appearance",
    label: t("外观"),
    icon: <PaintBrush20Regular />,
    order: 80,
    render: () => <AppearancePage />,
  });

  registerPage({
    id: "settings",
    label: t("设置"),
    icon: <Settings20Regular />,
    order: 90,
    render: () => <SettingsPage />,
  });
}
