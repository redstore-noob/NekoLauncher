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
/*
 * 下载大厅（UI 已脱离原版 DownloadPage 布局，改为胶囊分段标签 + 毛玻璃卡片风格）：
 * Minecraft 本体 / Mod / 整合包 / 光影包 / 材质包 / Java 六个标签，
 * MinecraftDownloadOverlay（版本确认）与 ContentDownloadOverlay（内容下载）弹层保留。
 *
 * 与旧版的刻意差异（修"遮罩层卡死"）：不再使用等待初始化完成才消失的全屏 loading 遮罩
 * ——旧版遮罩要等 GetVersions / QueryAvailableJavaVersions（联网）返回才关闭，请求一卡
 * 整个界面就被盖死。这里改为各标签页内联加载态，任何请求失败都不阻塞交互。
 */
import type { download, models } from "../../wailsjs/go/models";

import React, { useEffect, useMemo, useRef, useState } from "react";
import {
  Button,
  Input,
  Progress,
  Select,
  SelectItem,
  Spinner,
} from "@heroui/react";
// 图标统一用 Fluent UI System Icons（20px 系）
import {
  Cube20Regular,
  PuzzleCube20Regular,
  Box20Regular,
  Sparkle20Regular,
  Image20Regular,
  WindowDevTools20Regular,
  ChevronLeft20Regular,
  ChevronRight20Regular,
  ArrowDownload20Regular,
  Heart20Regular,
  Search20Regular,
  Dismiss20Regular,
  FolderOpen20Regular,
  CheckmarkCircle20Regular,
  ArrowImport20Regular,
  ArrowClockwise20Regular as RefreshIcon,
  Warning20Regular,
} from "@fluentui/react-icons";

import SegmentedTabs from "../components/segmented-tabs";
import { asArray } from "../lib/guards";
import { consumePendingDetail, onNavigate } from "../lib/navigation";
import { popoverMotionProps } from "../lib/motion";
import {
  ApplyVersionFilter,
  CancelDownload,
  GetCurrentDownloadSnapshot,
  GetVersions,
  SearchResources,
  StartDownload,
  StartModLoaderDownload,
} from "../../wailsjs/go/bindings/DownloadAPI";
import { GetGameDirectory } from "../../wailsjs/go/bindings/ConfigAPI";
import {
  LookupModNameTranslations,
  RefreshModNameTranslations,
} from "../../wailsjs/go/bindings/ContentAPI";
import {
  OpenInExplorer,
  SelectFile,
} from "../../wailsjs/go/bindings/SystemAPI";
import { EventsOn } from "../../wailsjs/runtime/runtime";
import MinecraftDownloadOverlay from "../components/download/MinecraftDownloadOverlay";
import ContentDownloadOverlay, {
  type ContentKind,
  type ProjectLike,
} from "../components/download/ContentDownloadOverlay";
import JavaDownloadTab from "../components/download/JavaDownloadTab";
// X-3 资源搜索已并入标签页大列表（版本/实例选择见 ContentDownloadOverlay）
import SwitchTransition, {
  useSwitchDirection,
} from "../components/screen-transition";
import { t } from "../i18n";

const PAGE_SIZE = 50;
/** Modrinth 标签页的空态：必须是稳定引用，否则相关 useMemo 每帧都会重算 */
const EMPTY_CONTENT_STATE: { all: ProjectLike[]; loading: boolean } = {
  all: [],
  loading: false,
};
// 中文名同时充当语言无关的标签 id（用于状态与各类映射表），展示时才 t()
const TAB_NAMES = [
  "Minecraft 本体",
  "Mod",
  "整合包",
  "光影包",
  "材质包",
  "Java",
];
const MODRINTH_TABS = ["Mod", "整合包", "光影包", "材质包"];

// 加载器筛选（原资源搜索弹层；空串 = 不过滤，Go 侧把空串当"全部"）
const MOD_LOADER_OPTIONS = ["", "fabric", "forge", "neoforge", "quilt"];

function loaderOptionLabel(loader: string): string {
  if (!loader) return t("全部加载器");
  if (loader === "neoforge") return "NeoForge";

  return loader.charAt(0).toUpperCase() + loader.slice(1);
}

// Modrinth project_type 与加载器 facets
const MODRINTH_CONFIG: Record<
  string,
  { type: string; loaders: string[] | null }
> = {
  Mod: { type: "mod", loaders: ["fabric", "forge", "quilt", "neoforge"] },
  整合包: { type: "modpack", loaders: null },
  光影包: { type: "shader", loaders: null },
  材质包: { type: "resourcepack", loaders: null },
};

/** 统一资源条目 → 弹层用的项目形状（字段名沿用 Modrinth 的接口命名） */
function toProjectLike(hit: models.ResourceHit): ProjectLike {
  return {
    project_id: hit.projectId,
    title: hit.title,
    description: hit.description,
    icon_url: hit.iconUrl,
    downloads: hit.downloads,
    follows: hit.follows,
  };
}

const TAB_KINDS: Record<string, ContentKind> = {
  Mod: "mod",
  整合包: "modpack",
  光影包: "shaderpack",
  材质包: "resourcepack",
};
const TAB_ICONS: Record<string, React.ReactNode> = {
  "Minecraft 本体": <Cube20Regular />,
  Mod: <PuzzleCube20Regular />,
  整合包: <Box20Regular />,
  光影包: <Sparkle20Regular />,
  材质包: <Image20Regular />,
  Java: <WindowDevTools20Regular />,
};

// 后端筛选不可用时的本地近似（C# VersionFilter 语义）
function localVersionFilter(
  list: models.MinecraftVersion[],
  key: string,
): models.MinecraftVersion[] {
  const map: Record<string, (v: models.MinecraftVersion) => boolean> = {
    release: (v) => v.type === "release",
    snapshot: (v) => v.type === "snapshot",
    old: (v) => ["old_alpha", "old_beta"].includes(v.type),
  };
  const predicate = map[key];

  return predicate ? list.filter(predicate) : list;
}

function joinPath(dir: string | undefined | null, name: string): string {
  if (!dir) return name;

  return dir.replace(/[\\/]+$/, "") + "\\" + name;
}
function formatCount(n?: number | null): string {
  const v = n ?? 0;

  if (v >= 1e6) return (v / 1e6).toFixed(1) + "M";
  if (v >= 1e3) return (v / 1e3).toFixed(1) + "k";

  return String(v);
}
function formatBytes(n?: number | null): string {
  if (!n) return "0 B";
  const units = ["B", "KB", "MB", "GB"];
  let i = 0;
  let v = n;

  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }

  return `${v.toFixed(1)} ${units[i]}`;
}
function formatDate(value: unknown): string {
  const date = value ? new Date(value as string) : null;

  return date && !isNaN(date.getTime()) ? date.toLocaleDateString() : "—";
}

// 版本类型徽章（正式版 / 快照 / 远古）
function versionTypeMeta(type: string): { label: string; className: string } {
  if (type === "release")
    return {
      label: t("正式版"),
      className: "bg-success-500/15 text-success-600 dark:text-success-400",
    };
  if (type === "snapshot")
    return {
      label: t("快照"),
      className: "bg-warning-500/15 text-warning-600 dark:text-warning-400",
    };

  return { label: t("远古"), className: "bg-default-100 text-gray-500" };
}

// 版本类型 → instance-icons 下的图标文件（缺失回落 Fluent 立方体）
const VERSION_TYPE_ICON: Record<string, string> = {
  release: "vanilla",
  snapshot: "snapshot_version",
  old: "old_version",
};

/** 版本行图标：优先图片，加载失败回落 Cube20Regular */
function VersionTypeIcon({ type }: { type: string }) {
  const [broken, setBroken] = React.useState(false);
  const key = VERSION_TYPE_ICON[type] ?? VERSION_TYPE_ICON.release;

  if (!broken) {
    return (
      <img
        alt=""
        className="size-7 object-contain"
        src={`/instance-icons/${key}.png`}
        onError={() => setBroken(true)}
      />
    );
  }

  return <Cube20Regular />;
}

const DownloadPage: React.FC = () => {
  // ---------- 标签切换 ----------
  const [activeTab, setActiveTab] = useState("Minecraft 本体");
  // 标签序号 → 切换方向（新标签从右侧或左侧滑入）
  const tabDirection = useSwitchDirection(TAB_NAMES.indexOf(activeTab));

  // ---------- Minecraft 版本 ----------
  const [allVersions, setAllVersions] = useState<models.MinecraftVersion[]>([]);
  const [versionQuery, setVersionQuery] = useState("");
  const [versionTypeFilter, setVersionTypeFilter] = useState("release");
  const [versionPage, setVersionPage] = useState(1);
  const [versionFiltered, setVersionFiltered] = useState<
    models.MinecraftVersion[]
  >([]);
  const [versionLoading, setVersionLoading] = useState(false);
  const versionQueryRef = useRef(versionQuery);

  versionQueryRef.current = versionQuery;
  const versionTypeFilterRef = useRef(versionTypeFilter);

  versionTypeFilterRef.current = versionTypeFilter;

  // ---------- Modrinth ----------
  const [contentQuery, setContentQuery] = useState("");
  const [contentPage, setContentPage] = useState(1);
  const [contentCache, setContentCache] = useState<
    Record<string, { all: ProjectLike[]; loading: boolean; error?: string }>
  >({});

  // ---------- 弹层与下载进度 ----------
  // Modrinth 列表筛选（原资源搜索弹层的两项）：游戏版本 + 加载器（仅 Mod）
  const [contentGameVersion, setContentGameVersion] = useState("");
  const [contentLoader, setContentLoader] = useState("");
  const [contentOverlay, setContentOverlay] = useState<{
    project: ProjectLike | null;
    kind: ContentKind;
    localPath: string;
  } | null>(null);
  const [mcOverlayVersion, setMcOverlayVersion] =
    useState<models.MinecraftVersion | null>(null);
  const [downloadActive, setDownloadActive] = useState(false);
  const [progressPercent, setProgressPercent] = useState(0);
  const [taskStatusText, setTaskStatusText] = useState("");
  const [finishedVersion, setFinishedVersion] = useState("");

  const searchTimer = useRef<ReturnType<typeof setTimeout> | null>(null);

  // ---------- Minecraft 版本筛选（走后端 ApplyVersionFilter，失败回退本地） ----------
  const recomputeVersionFilter = async () => {
    const query = versionQueryRef.current.trim().toLowerCase();
    let list = allVersions;

    if (query) list = list.filter((v) => v.id.toLowerCase().includes(query));
    try {
      if (versionTypeFilterRef.current !== "all") {
        list =
          (await ApplyVersionFilter(list, versionTypeFilterRef.current)) ??
          list;
      }
    } catch (ex) {
      console.error(t("版本筛选失败，回退本地筛选"), ex);
      list = localVersionFilter(list, versionTypeFilterRef.current);
    }
    setVersionFiltered(list);
  };

  useEffect(() => {
    setVersionPage(1);
    void recomputeVersionFilter();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [versionQuery, versionTypeFilter, allVersions]);

  const loadVersions = async () => {
    setVersionLoading(true);
    try {
      // 优先走后端清单（跟随下载源镜像），失败回退空列表
      setAllVersions(asArray(await GetVersions()));
    } catch (ex) {
      console.error(t("获取版本清单失败"), ex);
      setAllVersions([]);
    } finally {
      setVersionLoading(false);
    }
  };

  // MinecraftDownloadOverlay 确认回调：原版 StartDownload；带加载器 StartModLoaderDownload
  const onMcOverlayConfirm = async (options: {
    loaderType: number;
    loaderVersion: download.ModLoaderVersion | null;
    instanceName: string;
    skipFabricApi: boolean;
  }) => {
    const version = mcOverlayVersion;

    setMcOverlayVersion(null);
    if (!version) return;
    try {
      setTaskStatusText(
        t("开始下载 {0}", { "0": options.instanceName || version.id }),
      );
      if (options.loaderType === 0) {
        await StartDownload(version);
      } else {
        await StartModLoaderDownload(
          version,
          options.loaderVersion!,
          options.instanceName,
          options.skipFabricApi,
        );
      }
    } catch (ex) {
      console.error(t("启动版本下载失败"), ex);
      setTaskStatusText(
        t("下载失败：{0}", { "0": (ex as Error)?.message ?? ex }),
      );
    }
  };

  // ---------- Modrinth 搜索（300ms 防抖；走后端绑定，自动镜像回退） ----------
  // 搜索是"打字即发"的：慢的旧请求可能后于新请求返回，把列表刷成过期结果，
  // 所以用一个自增序号，回来时不是最新那次就丢弃。
  //
  // 为什么不再由前端直接 fetch api.modrinth.com：官方域名在国内经常超时，
  // 后端会先试官方、再回退国内镜像（并把"已走镜像"如实回报）；展示串也统一由
  // Go 侧生成，避免同一份格式化逻辑在两端各写一遍。
  const modrinthSeqRef = useRef(0);

  const searchModrinth = async (tab: string, query: string) => {
    const config = MODRINTH_CONFIG[tab];

    if (!config) return;
    const seq = ++modrinthSeqRef.current;

    setContentCache((cache) => ({
      ...cache,
      [tab]: { all: [], loading: true },
    }));
    try {
      const request: models.ResourceSearchRequest = {
        source: "modrinth",
        projectType: config.type,
        query,
        // 原资源搜索弹层的筛选：游戏版本（可选）与加载器（仅 Mod 标签页）
        gameVersion: contentGameVersion.trim(),
        loader: config.loaders ? contentLoader : "",
        loaders: config.loaders ?? [],
        limit: 100,
      };
      const result = await SearchResources(request);

      if (seq !== modrinthSeqRef.current) return;
      setContentCache((cache) => ({
        ...cache,
        [tab]: {
          all: asArray<models.ResourceHit>(result?.hits).map(toProjectLike),
          loading: false,
        },
      }));
    } catch (ex) {
      console.error(t("搜索 {0} 失败", { "0": tab }), ex);
      if (seq !== modrinthSeqRef.current) return;
      setContentCache((cache) => ({
        ...cache,
        [tab]: {
          all: [],
          loading: false,
          error: (ex as Error)?.message ?? String(ex),
        },
      }));
    }
  };

  const switchTab = (tab: string) => {
    setActiveTab(tab);
    setContentPage(1);
    if (MODRINTH_TABS.includes(tab) && !contentCache[tab]) {
      void searchModrinth(tab, contentQuery.trim());
    }
  };

  // 搜索词 / 筛选变化：重置页码 + 对 Modrinth 标签页做 300ms 防抖重查
  const firstQueryRender = useRef(true);

  useEffect(() => {
    if (firstQueryRender.current) {
      firstQueryRender.current = false;

      return;
    }
    setContentPage(1);
    if (!MODRINTH_TABS.includes(activeTab)) return;
    if (searchTimer.current) clearTimeout(searchTimer.current);
    searchTimer.current = setTimeout(() => {
      void searchModrinth(activeTab, contentQuery.trim());
    }, 300);

    return () => {
      if (searchTimer.current) clearTimeout(searchTimer.current);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [contentQuery, contentGameVersion, contentLoader]);

  // ?? 的右值每次渲染都是新对象，会让下面的 useMemo 依赖永远变化；
  // 固定成一个常量作为空态
  const contentState = contentCache[activeTab] ?? EMPTY_CONTENT_STATE;
  const contentFiltered = useMemo(() => {
    const query = contentQuery.trim().toLowerCase();
    const all = contentState.all;

    if (!query) return all;

    return all.filter(
      (p) =>
        String(p.title ?? "")
          .toLowerCase()
          .includes(query) ||
        String(p.description ?? "")
          .toLowerCase()
          .includes(query),
    );
  }, [contentState, contentQuery]);

  const versionTotalPages = Math.max(
    1,
    Math.ceil(versionFiltered.length / PAGE_SIZE),
  );
  const versionPageItems = versionFiltered.slice(
    (versionPage - 1) * PAGE_SIZE,
    versionPage * PAGE_SIZE,
  );
  const contentTotalPages = Math.max(
    1,
    Math.ceil(contentFiltered.length / PAGE_SIZE),
  );
  const contentPageItems = contentFiltered.slice(
    (contentPage - 1) * PAGE_SIZE,
    contentPage * PAGE_SIZE,
  );

  // ---------- 资源中文名（MC百科） ----------
  // 下载大厅的资源标题是英文原名；复用实例页同一套 MC百科（mcmod.cn）译名服务：
  // 先秒回缓存命中，未命中的由后端限流补查（与已装 Mod 共用一份持久缓存），
  // 补到后经 "modname:updated" 事件触发重查。只查当前页条目，尊重搜索配额。
  const [contentNames, setContentNames] = useState<Record<string, string>>({});
  const contentPageTitles = useMemo(
    () =>
      contentPageItems
        .map((p) => String(p.title ?? ""))
        .filter((n) => !!n)
        .join("\n"),
    [contentPageItems],
  );

  useEffect(() => {
    const titles = contentPageTitles.split("\n").filter((n) => !!n);

    if (titles.length === 0) {
      setContentNames({});

      return;
    }
    let cancelled = false;
    const refresh = () => {
      void LookupModNameTranslations(titles)
        .then((map) => {
          if (!cancelled && map && Object.keys(map).length > 0) {
            setContentNames(map);
          }
        })
        .catch(() => {
          /* 译名查询失败不影响列表展示 */
        });
    };

    refresh();
    void RefreshModNameTranslations(titles).catch(() => {});
    const off = EventsOn("modname:updated", refresh);

    return () => {
      cancelled = true;
      off();
    };
    // contentPageTitles 是拼接串：页码/搜索词变化才变，避免每帧重发请求
  }, [contentPageTitles]);

  function downloadContent(project: ProjectLike) {
    setContentOverlay({
      project,
      kind: TAB_KINDS[activeTab] ?? "mod",
      localPath: "",
    });
  }

  // 整合包标签页：导入本地整合包（.mrpack / CurseForge .zip）→ ContentDownloadOverlay 安装流程
  const importLocalModpack = async () => {
    try {
      const path = await SelectFile(
        t("选择整合包文件"),
        t("整合包"),
        "*.mrpack;*.zip",
      );

      if (!path) return;
      setContentOverlay({ project: null, kind: "modpack", localPath: path });
    } catch (ex) {
      console.error(t("选择整合包文件失败"), ex);
    }
  };

  // ---------- 下载进度（download:progress 快照驱动） ----------
  function applyDownloadSnapshot(snap: download.GameDownloadSnapshot | null) {
    if (!snap) return;
    const percent = Math.min(100, snap.Percentage ?? 0);
    const running = !!snap.VersionID && percent > 0 && percent < 100;

    setDownloadActive(running);
    if (running) {
      setProgressPercent(percent);
      setTaskStatusText(
        `${snap.VersionID} · ${snap.StageName ?? ""} ${snap.Detail ?? ""} ` +
          `${formatBytes(snap.CompletedBytes)}/${formatBytes(snap.TotalBytes)} · ${formatBytes(snap.BytesPerSecond)}/s`,
      );
    } else if (snap.VersionID && percent >= 100) {
      // 下载完成：记录版本号，展示「打开文件夹」入口
      setFinishedVersion(snap.VersionID);
      setTaskStatusText(t("{0} 下载完成", { "0": snap.VersionID }));
    }
  }

  const openDownloadFolder = async () => {
    const version = finishedVersion;

    if (!version) return;
    try {
      const gameDir = (await GetGameDirectory()) || "";
      // 优先定位到版本目录；不存在时退回打开游戏根目录
      const candidates = [
        joinPath(joinPath(gameDir, "versions"), version),
        gameDir,
      ].filter(Boolean);

      for (const dir of candidates) {
        try {
          await OpenInExplorer(dir);

          return;
        } catch {
          /* 尝试下一个 */
        }
      }
    } catch (ex) {
      console.error(t("打开下载目录失败"), ex);
    }
  };

  const onCancelDownload = async () => {
    try {
      await CancelDownload();
      setTaskStatusText(t("已取消"));
      setDownloadActive(false);
    } catch (ex) {
      console.error(t("取消下载失败"), ex);
    }
  };

  // ---------- 通用 ----------
  function onRefresh() {
    if (activeTab === "Minecraft 本体") void loadVersions();
    else if (MODRINTH_TABS.includes(activeTab))
      void searchModrinth(activeTab, contentQuery.trim());
  }

  // ---------- 生命周期：初始化 + 事件订阅（无阻塞遮罩，失败不锁界面） ----------
  useEffect(() => {
    void (async () => {
      await loadVersions();
      try {
        applyDownloadSnapshot(await GetCurrentDownloadSnapshot());
      } catch (ex) {
        console.error(t("读取下载快照失败"), ex);
      }
    })();
    // 逐个退订：EventsOff 会连下载浮标 / 主页下载卡片的订阅一起清掉
    const offProgress = EventsOn("download:progress", applyDownloadSnapshot);

    return () => {
      offProgress();
    };
  }, []);

  // 主页 Java 卡片等请求打开 Java 标签页。
  // 两个来源：页面已挂载 → 走导航总线实时事件；跨页跳过来 → 读挂载时暂存的 detail
  useEffect(() => {
    const openJavaTab = () => switchTab("Java");

    if (consumePendingDetail("download") === "java") openJavaTab();

    return onNavigate((request) => {
      if (request.pageId === "download" && request.detail === "java") {
        openJavaTab();
      }
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // 分页条（图标箭头 + 页码）
  const renderPager = (
    page: number,
    totalPages: number,
    setPage: (updater: (p: number) => number) => void,
  ) => (
    <div className="flex items-center justify-center gap-2 pt-1">
      <Button
        isIconOnly
        aria-label={t("上一页")}
        isDisabled={page <= 1}
        radius="full"
        size="sm"
        variant="flat"
        onPress={() => setPage((p) => Math.max(1, p - 1))}
      >
        <ChevronLeft20Regular />
      </Button>
      <span className="min-w-[64px] text-center text-xs text-gray-400 tabular-nums">
        {page} / {totalPages}
      </span>
      <Button
        isIconOnly
        aria-label={t("下一页")}
        isDisabled={page >= totalPages}
        radius="full"
        size="sm"
        variant="flat"
        onPress={() => setPage((p) => Math.min(totalPages, p + 1))}
      >
        <ChevronRight20Regular />
      </Button>
    </div>
  );

  const emptyState = (
    text: string,
    icon: React.ReactNode = <Box20Regular className="w-8 h-8" />,
  ) => (
    <div className="my-12 flex flex-col items-center gap-3 text-center text-gray-400">
      <div className="flex size-16 items-center justify-center rounded-3xl bg-gradient-to-br from-default-200 to-default-100 dark:from-gray-800 dark:to-gray-800/50 shadow-inner">
        {icon}
      </div>
      <span className="text-[15px] font-semibold text-gray-500 dark:text-gray-400">
        {text}
      </span>
    </div>
  );

  return (
    <div className="relative flex h-full min-h-0 w-full flex-col gap-4 overflow-hidden px-6 py-5">
      {/* 标题区 */}
      <div className="flex flex-none items-center gap-3">
        <div className="flex min-w-0 flex-1 flex-col gap-0.5">
          <h1 className="overflow-hidden text-xl font-bold tracking-tight text-ellipsis whitespace-nowrap">
            {t("下载大厅")}
          </h1>
          {taskStatusText && !downloadActive && !finishedVersion ? (
            <span className="truncate text-[11px] text-gray-400">
              {taskStatusText}
            </span>
          ) : null}
        </div>
        <Button
          radius="full"
          size="sm"
          startContent={<RefreshIcon />}
          variant="flat"
          onPress={onRefresh}
        >
          {t("刷新")}
        </Button>
      </div>

      {/* 下载状态卡（下载中 / 刚完成时显示） */}
      {(downloadActive || finishedVersion) && (
        <div className="flex flex-none items-center gap-3 rounded-2xl border nya-border nya-panel px-4 py-2.5 shadow-sm backdrop-blur-md">
          {downloadActive ? (
            <Spinner className="flex-none" color="primary" size="sm" />
          ) : (
            <span className="flex-none text-success-500">
              <CheckmarkCircle20Regular />
            </span>
          )}
          <div className="flex min-w-0 flex-1 flex-col gap-1">
            <span className="truncate text-[12px] text-gray-600 dark:text-gray-300">
              {taskStatusText}
            </span>
            {downloadActive && (
              <Progress
                aria-label={t("下载进度")}
                className="max-w-full"
                size="sm"
                value={progressPercent}
              />
            )}
          </div>
          {downloadActive && (
            <Button
              className="flex-none"
              color="danger"
              radius="full"
              size="sm"
              startContent={<Dismiss20Regular />}
              variant="flat"
              onPress={() => void onCancelDownload()}
            >
              {t("取消")}
            </Button>
          )}
          {finishedVersion && !downloadActive && (
            <Button
              className="flex-none"
              radius="full"
              size="sm"
              startContent={<FolderOpen20Regular />}
              variant="flat"
              onPress={() => void openDownloadFolder()}
            >
              {t("打开文件夹")}
            </Button>
          )}
        </div>
      )}

      {/* 胶囊分段标签栏（主色滑块随选中项滑动） */}
      <SegmentedTabs
        className="flex flex-none items-center gap-1 self-start rounded-full border nya-border nya-panel p-1 shadow-sm backdrop-blur-md"
        items={TAB_NAMES.map((tab) => ({
          key: tab,
          label: (
            <>
              {TAB_ICONS[tab]}
              <span>{t(tab)}</span>
            </>
          ),
        }))}
        layoutId="download-main-tab"
        value={activeTab}
        onChange={switchTab}
      />

      {/* 内容区 */}
      <div className="nya-scroll flex min-h-0 flex-1 flex-col gap-3 overflow-x-hidden overflow-y-auto pr-1">
        <SwitchTransition
          activeKey={activeTab}
          className="flex flex-col gap-3"
          direction={tabDirection}
        >
          {/* ===== Minecraft 本体 ===== */}
          {activeTab === "Minecraft 本体" && (
            <>
              <div className="flex flex-none items-center gap-3">
                <Input
                  aria-label={t("搜索版本号")}
                  className="min-w-0 flex-1"
                  classNames={{
                    inputWrapper:
                      "bg-default-100/80 data-[hover=true]:bg-default-200",
                  }}
                  placeholder={t("搜索版本号")}
                  radius="full"
                  size="sm"
                  startContent={<Search20Regular className="text-gray-400" />}
                  value={versionQuery}
                  onValueChange={setVersionQuery}
                />
                <Select
                  aria-label={t("版本类型")}
                  className="w-32 min-w-0 max-w-full flex-shrink-0 [&_*]:min-w-0"
                  popoverProps={{ motionProps: popoverMotionProps }}
                  radius="full"
                  selectedKeys={[versionTypeFilter]}
                  size="sm"
                  onSelectionChange={(keys) =>
                    setVersionTypeFilter(
                      String(Array.from(keys)[0] ?? "release"),
                    )
                  }
                >
                  <SelectItem key="all">{t("全部版本")}</SelectItem>
                  <SelectItem key="release">{t("正式版")}</SelectItem>
                  <SelectItem key="snapshot">{t("快照版")}</SelectItem>
                  <SelectItem key="old">{t("远古版本")}</SelectItem>
                </Select>
              </div>
              {versionLoading ? (
                <div className="flex items-center gap-2 my-10 justify-center text-xs text-gray-400">
                  <Spinner size="sm" /> {t("正在获取版本清单…")}
                </div>
              ) : versionPageItems.length === 0 ? (
                emptyState(
                  t("没有找到匹配的版本"),
                  <Cube20Regular className="w-8 h-8" />,
                )
              ) : (
                <div className="flex flex-col gap-2">
                  {versionPageItems.map((v) => {
                    const meta = versionTypeMeta(v.type);

                    return (
                      <button
                        key={v.id}
                        className="group flex cursor-pointer items-center gap-3 rounded-2xl border border-transparent nya-panel px-3.5 py-3 text-left backdrop-blur-md transition-all hover:translate-x-0.5 hover:border-primary/30 hover:bg-primary/[0.06]"
                        onClick={() => setMcOverlayVersion(v)}
                      >
                        <span className="flex size-10 flex-none items-center justify-center rounded-xl bg-default-100 text-primary shadow-inner dark:bg-default-100/60">
                          <VersionTypeIcon type={v.type} />
                        </span>
                        <div className="flex min-w-0 flex-1 items-center gap-2.5">
                          <span className="overflow-hidden text-sm font-semibold text-ellipsis whitespace-nowrap">
                            {v.id}
                          </span>
                          <span
                            className={`flex-none rounded-full px-2 py-0.5 text-[10px] font-medium ${meta.className}`}
                          >
                            {meta.label}
                          </span>
                        </div>
                        <span className="flex-none text-[11px] text-gray-400 tabular-nums">
                          {formatDate(v.releaseTime)}
                        </span>
                        <span className="flex-none text-gray-300 transition-colors group-hover:text-primary">
                          <ArrowDownload20Regular />
                        </span>
                      </button>
                    );
                  })}
                </div>
              )}
              <div className="text-center text-xs text-gray-400">
                {t("共")} {versionFiltered.length} {t("个版本")}
              </div>
              {renderPager(versionPage, versionTotalPages, setVersionPage)}
            </>
          )}

          {/* ===== Modrinth 内容类 ===== */}
          {MODRINTH_TABS.includes(activeTab) && (
            <>
              <div className="flex flex-none items-center gap-3">
                <Input
                  aria-label={t("搜索{0}", { "0": t(activeTab) })}
                  className="min-w-0 flex-1"
                  classNames={{
                    inputWrapper:
                      "bg-default-100/80 data-[hover=true]:bg-default-200",
                  }}
                  placeholder={t("搜索{0}", { "0": t(activeTab) })}
                  radius="full"
                  size="sm"
                  startContent={<Search20Regular className="text-gray-400" />}
                  value={contentQuery}
                  onValueChange={setContentQuery}
                />
                {/* 游戏版本筛选（原资源搜索弹层）：输入如 1.20.1，回车/防抖后生效 */}
                <Input
                  aria-label={t("游戏版本过滤")}
                  className="w-36 flex-none"
                  classNames={{
                    inputWrapper:
                      "bg-default-100/80 data-[hover=true]:bg-default-200",
                  }}
                  placeholder={t("游戏版本（可选）")}
                  radius="full"
                  size="sm"
                  value={contentGameVersion}
                  onValueChange={setContentGameVersion}
                />
                {/* 加载器筛选：仅 Mod 标签页有意义 */}
                {MODRINTH_CONFIG[activeTab]?.loaders ? (
                  <Select
                    aria-label={t("加载器过滤")}
                    className="w-36 flex-none"
                    items={MOD_LOADER_OPTIONS.map((value) => ({
                      key: value || "__all__",
                      label: loaderOptionLabel(value),
                    }))}
                    popoverProps={{ motionProps: popoverMotionProps }}
                    selectedKeys={[contentLoader || "__all__"]}
                    size="sm"
                    onSelectionChange={(keys) => {
                      const raw = String(Array.from(keys)[0] ?? "__all__");

                      setContentLoader(raw === "__all__" ? "" : raw);
                    }}
                  >
                    {(item) => (
                      <SelectItem key={item.key}>{item.label}</SelectItem>
                    )}
                  </Select>
                ) : null}
                {activeTab === "整合包" && (
                  <Button
                    className="flex-shrink-0"
                    radius="full"
                    size="sm"
                    startContent={<ArrowImport20Regular />}
                    variant="flat"
                    onPress={() => void importLocalModpack()}
                  >
                    {t("导入本地整合包")}
                  </Button>
                )}
              </div>
              {contentState.loading ? (
                <div className="flex items-center gap-2 my-10 justify-center text-xs text-gray-400">
                  <Spinner size="sm" /> {t("正在搜索")} {t(activeTab)}…
                </div>
              ) : contentState.error ? (
                <div className="my-10 flex flex-col items-center gap-3 text-center">
                  <div className="flex items-center gap-2 text-xs text-danger">
                    <Warning20Regular />
                    {t("搜索失败：{0}", { "0": contentState.error })}
                  </div>
                  <div className="max-w-md text-[11px] leading-relaxed text-gray-400">
                    {t(
                      "搜索走后端绑定：官方接口失败时会自动改用国内镜像，仍失败才是网络问题；稍后重试即可。",
                    )}
                  </div>
                  <Button
                    size="sm"
                    startContent={<RefreshIcon />}
                    variant="flat"
                    onPress={() =>
                      void searchModrinth(activeTab, contentQuery.trim())
                    }
                  >
                    {t("重试")}
                  </Button>
                </div>
              ) : contentPageItems.length === 0 ? (
                emptyState(
                  t("没有找到匹配的{0}", { "0": t(activeTab) }),
                  TAB_ICONS[activeTab],
                )
              ) : (
                <div className="flex flex-col gap-2">
                  {contentPageItems.map((p) => (
                    <button
                      key={String(p.project_id)}
                      className="group flex cursor-pointer items-center gap-3 rounded-2xl border border-transparent nya-panel px-3.5 py-3 text-left backdrop-blur-md transition-all hover:translate-x-0.5 hover:border-primary/30 hover:bg-primary/[0.06]"
                      onClick={() => downloadContent(p)}
                    >
                      {p.icon_url ? (
                        <img
                          alt=""
                          className="size-11 flex-none rounded-xl object-cover shadow-sm"
                          src={String(p.icon_url)}
                        />
                      ) : (
                        <span className="flex size-11 flex-none items-center justify-center rounded-xl bg-primary text-primary-foreground shadow-md shadow-primary/25">
                          {TAB_ICONS[activeTab]}
                        </span>
                      )}
                      <div className="flex min-w-0 flex-1 flex-col gap-0.5">
                        <span className="flex items-baseline gap-2 overflow-hidden">
                          <span className="overflow-hidden text-sm font-semibold text-ellipsis whitespace-nowrap">
                            {String(p.title ?? "")}
                          </span>
                          {contentNames[String(p.title ?? "")] && (
                            <span className="flex-none text-xs font-medium text-primary/80">
                              {contentNames[String(p.title ?? "")]}
                            </span>
                          )}
                        </span>
                        <span className="overflow-hidden text-xs text-gray-400 text-ellipsis whitespace-nowrap">
                          {String(p.description ?? "")}
                        </span>
                      </div>
                      <div className="flex flex-none items-center gap-3 text-[11px] text-gray-400">
                        <span className="flex items-center gap-1">
                          <ArrowDownload20Regular className="w-4 h-4" />
                          {formatCount(p.downloads)}
                        </span>
                        <span className="flex items-center gap-1">
                          <Heart20Regular className="w-4 h-4" />
                          {formatCount(p.follows)}
                        </span>
                      </div>
                    </button>
                  ))}
                </div>
              )}
              <div className="text-center text-xs text-gray-400">
                {t("来自 Modrinth · 共")} {contentFiltered.length} {t("个结果")}
              </div>
              {renderPager(contentPage, contentTotalPages, setContentPage)}
            </>
          )}

          {/* ===== Java（独立组件：候选下载 + 已安装管理） ===== */}
          {activeTab === "Java" && <JavaDownloadTab />}
        </SwitchTransition>
      </div>

      {/* 版本下载确认弹层 */}
      <MinecraftDownloadOverlay
        version={mcOverlayVersion}
        onClose={() => setMcOverlayVersion(null)}
        onConfirm={(options) => void onMcOverlayConfirm(options)}
      />

      {/* 内容下载弹层（版本选择 + 目标实例 + 进度） */}
      <ContentDownloadOverlay
        kind={contentOverlay?.kind ?? "modpack"}
        localModpackPath={contentOverlay?.localPath ?? ""}
        open={contentOverlay !== null}
        project={contentOverlay?.project ?? null}
        onClose={() => setContentOverlay(null)}
      />

      {/* 资源搜索弹层已并入各标签页的大列表：搜索框 + 列表 + ContentDownloadOverlay */}
    </div>
  );
};

export default DownloadPage;
