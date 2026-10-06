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
import { Button, Input, Select, SelectItem } from "@heroui/react";
import {
  Cube20Regular,
  PuzzleCube20Regular,
  Box20Regular,
  Sparkle20Regular,
  Image20Regular,
  WindowDevTools20Regular,
  ArrowDownload20Regular,
  Heart20Regular,
  Search20Regular,
  ArrowImport20Regular,
  ArrowClockwise20Regular as RefreshIcon,
  Warning20Regular,
} from "@fluentui/react-icons";

// 图标统一用 Fluent UI System Icons（20px 系）

import Pager from "../components/pager";
import SegmentedTabs from "../components/segmented-tabs";
import EmptyState from "../components/empty-state";
import LoadingRow from "../components/loading-row";
import { selectPopoverProps } from "../lib/motion";
import { asArray } from "../lib/guards";
import { consumePendingDetail, onNavigate } from "../lib/navigation";
import {
  ApplyVersionFilter,
  GetResourceSources,
  GetVersions,
  SearchResources,
  StartDownload,
  StartModLoaderDownload,
} from "../../wailsjs/go/bindings/DownloadAPI";
import {
  LookupModNameTranslations,
  RefreshModNameTranslations,
} from "../../wailsjs/go/bindings/ContentAPI";
import { SelectFile } from "../../wailsjs/go/bindings/SystemAPI";
import { ImportSoloExe } from "../../wailsjs/go/bindings/ModpackAPI";
import { EventsOn } from "../../wailsjs/runtime/runtime";
import { notify } from "../components/overlay/dialog";
import MinecraftDownloadOverlay from "../components/download/MinecraftDownloadOverlay";
import ContentDownloadOverlay, {
  type ContentKind,
  type ProjectLike,
} from "../components/download/ContentDownloadOverlay";
import JavaDownloadTab from "../components/download/JavaDownloadTab";
import DownloadTaskPanel from "../components/download/DownloadTaskPanel";
// X-3 资源搜索已并入标签页大列表（版本/实例选择见 ContentDownloadOverlay）
import SwitchTransition, {
  useSwitchDirection,
} from "../components/screen-transition";
import { PageActionSlot } from "../plugin";
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

// 资源平台筛选：Modrinth 免 Key 直接用；CurseForge 需要在设置里填 API Key
// （未配置时界面就地给引导，不报错）。后端按 source 走对应站点接口，
// 官方失败都会自动回退国内镜像。
const CONTENT_SOURCES = [
  { id: "modrinth", label: "Modrinth" },
  { id: "curseforge", label: "CurseForge" },
] as const;

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
    source: hit.source,
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

function formatCount(n?: number | null): string {
  const v = n ?? 0;

  if (v >= 1e6) return (v / 1e6).toFixed(1) + "M";
  if (v >= 1e3) return (v / 1e3).toFixed(1) + "k";

  return String(v);
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
  // 清单拉取失败与"筛完没结果"是两回事：混用空态文案会让断网被当成筛选问题
  const [versionLoadError, setVersionLoadError] = useState("");
  const versionQueryRef = useRef(versionQuery);

  versionQueryRef.current = versionQuery;
  const versionTypeFilterRef = useRef(versionTypeFilter);

  versionTypeFilterRef.current = versionTypeFilter;

  // ---------- Modrinth / CurseForge ----------
  const [contentQuery, setContentQuery] = useState("");
  const [contentPage, setContentPage] = useState(1);
  // 资源平台筛选（modrinth / curseforge）；缓存键 = 平台:标签页
  const [contentSource, setContentSource] = useState("modrinth");
  const [contentCache, setContentCache] = useState<
    Record<
      string,
      {
        all: ProjectLike[];
        loading: boolean;
        error?: string;
        needsApiKey?: boolean;
        message?: string;
      }
    >
  >({});
  // 数据源元信息（CurseForge 是否已配置 Key、申请地址等）
  const [sources, setSources] = useState<models.ResourceSourceInfo[]>([]);

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
  // 游戏本体是否有任务在跑（详情看右下角下载中心，页面内只放一条摘要条）
  const [gameRunning, setGameRunning] = useState(false);
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
    setVersionLoadError("");
    try {
      // 优先走后端清单（跟随下载源镜像），失败回退空列表
      setAllVersions(asArray(await GetVersions()));
    } catch (ex) {
      console.error(t("获取版本清单失败"), ex);
      setAllVersions([]);
      setVersionLoadError(
        t("版本清单获取失败，请检查网络或稍后在设置中切换下载源重试。"),
      );
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
    let started = false;

    try {
      setTaskStatusText(
        t("开始下载 {0}", { "0": options.instanceName || version.id }),
      );
      if (options.loaderType === 0) {
        // 必须 await 后判断：Promise 恒真会让"任务被拒"（已有下载在跑）也弹成功
        started = await StartDownload(version).catch(() => false);
      } else {
        started = await StartModLoaderDownload(
          version,
          options.loaderVersion!,
          options.instanceName,
          options.skipFabricApi,
        ).catch(() => false);
      }
      if (!started) {
        setTaskStatusText(
          t(
            "下载任务未能启动（可能有正在进行的下载），请到右下角下载中心确认。",
          ),
        );
        notify.warning(t("下载任务未能启动，可能已有任务正在进行。"));

        return;
      }
      // 任务已入列：轻提示 + 进度统一在右下角下载中心跟踪
      notify.success(t("已加入下载任务，进度见右下角的下载中心"));
    } catch (ex) {
      console.error(t("启动版本下载失败"), ex);
      setTaskStatusText(
        t("下载失败：{0}", { "0": (ex as Error)?.message ?? ex }),
      );
    }
  };

  // ---------- 资源搜索（300ms 防抖；走后端绑定，自动镜像回退） ----------
  // 搜索是"打字即发"的：慢的旧请求可能后于新请求返回，把列表刷成过期结果，
  // 所以用一个自增序号，回来时不是最新那次就丢弃。
  //
  // 为什么不由前端直接 fetch api.modrinth.com / api.curseforge.com：
  // 官方域名在国内经常超时，后端会先试官方、再回退国内镜像（并把"已走镜像"
  // 如实回报）；展示串也统一由 Go 侧生成，避免同一份格式化逻辑在两端各写一遍。
  const modrinthSeqRef = useRef(0);

  const searchModrinth = async (
    tab: string,
    query: string,
    source: string = contentSource,
  ) => {
    const config = MODRINTH_CONFIG[tab];

    if (!config) return;
    const seq = ++modrinthSeqRef.current;
    const cacheKey = `${source}:${tab}`;

    setContentCache((cache) => ({
      ...cache,
      [cacheKey]: { all: [], loading: true },
    }));
    try {
      const request: models.ResourceSearchRequest = {
        source,
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
        [cacheKey]: {
          all: asArray<models.ResourceHit>(result?.hits).map(toProjectLike),
          loading: false,
          needsApiKey: !!result?.needsApiKey,
          message: result?.message ?? "",
        },
      }));
    } catch (ex) {
      console.error(t("搜索 {0} 失败", { "0": tab }), ex);
      if (seq !== modrinthSeqRef.current) return;
      setContentCache((cache) => ({
        ...cache,
        [cacheKey]: {
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
    if (
      MODRINTH_TABS.includes(tab) &&
      !contentCache[`${contentSource}:${tab}`]
    ) {
      void searchModrinth(tab, contentQuery.trim());
    }
  };

  // 切换资源平台：清页码，搜索由 contentSource 变化的 effect 触发
  const switchContentSource = (source: string) => {
    if (source === contentSource) return;
    setContentSource(source);
    setContentPage(1);
  };

  // 搜索词 / 筛选 / 平台变化：重置页码 + 对资源标签页做 300ms 防抖重查
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
  }, [contentQuery, contentGameVersion, contentLoader, contentSource]);

  // ?? 的右值每次渲染都是新对象，会让下面的 useMemo 依赖永远变化；
  // 固定成一个常量作为空态
  const contentState =
    contentCache[`${contentSource}:${activeTab}`] ?? EMPTY_CONTENT_STATE;

  // ---------- 资源中文名（MC百科） ----------
  // 下载大厅的资源标题是英文原名；复用实例页同一套 MC百科（mcmod.cn）译名服务：
  // 先秒回缓存命中，未命中的由后端限流补查（与已装 Mod 共用一份持久缓存），
  // 补到后经 "modname:updated" 事件触发重查。只查当前页条目，尊重搜索配额。
  // 声明在 contentFiltered 之前：过滤要拿中文名做本地二次匹配。
  const [contentNames, setContentNames] = useState<Record<string, string>>({});

  // 本地二次过滤：标题/描述之外，中文名（MC百科译名）也参与匹配——
  // 输入"钠"能筛出 Sodium。contentNames 补查回来后重算。
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
          .includes(query) ||
        String(contentNames[String(p.title ?? "")] ?? "")
          .toLowerCase()
          .includes(query),
    );
  }, [contentState, contentQuery, contentNames]);

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

  // 整合包标签页：导入本地整合包（.mrpack / CurseForge .zip / NekoSolo .exe）→ ContentDownloadOverlay 安装流程
  const importLocalModpack = async () => {
    try {
      const path = await SelectFile(
        t("选择整合包文件"),
        t("整合包"),
        "*.mrpack;*.zip;*.exe",
      );

      if (!path) return;
      let packPath = path;

      // NekoSolo 安装包：先转存为临时 .mrpack 再走统一导入流程
      if (/\.exe$/i.test(path)) {
        try {
          packPath = await ImportSoloExe(path);
        } catch (ex) {
          console.error(t("解析 NekoSolo 安装包失败"), ex);

          return;
        }
      }
      setContentOverlay({
        project: null,
        kind: "modpack",
        localPath: packPath,
      });
    } catch (ex) {
      console.error(t("选择整合包文件失败"), ex);
    }
  };

  // ---------- 下载状态（download:progress 快照，只取摘要；进度看右下角下载中心） ----------
  // GameDownloadPhase（与 internal/download/game_download_service.go 一致）
  const GAME_PHASE_PREPARING = 1;
  const GAME_PHASE_DOWNLOADING = 2;
  const GAME_PHASE_COMPLETED = 3;
  const GAME_PHASE_FAILED = 4;
  const GAME_PHASE_CANCELLED = 5;

  function applyDownloadSnapshot(snap: download.GameDownloadSnapshot | null) {
    if (!snap) return;
    // 运行判定以 Phase 为准：按 percent 猜的话，失败/取消的任务（percent
    // 停在 0-100 之间）会让横幅永远显示"任务运行中"，Preparing（percent=0）
    // 反而不显示——两种方向都会骗人
    const running =
      !!snap.VersionID &&
      (snap.Phase === GAME_PHASE_PREPARING ||
        snap.Phase === GAME_PHASE_DOWNLOADING);

    setGameRunning(running);
    if (!snap.VersionID) return;
    if (snap.Phase === GAME_PHASE_COMPLETED) {
      // 下载完成：记录版本号，展示「打开文件夹」入口
      setFinishedVersion(snap.VersionID);
      setTaskStatusText(t("{0} 下载完成", { "0": snap.VersionID }));
    } else if (snap.Phase === GAME_PHASE_FAILED) {
      setTaskStatusText(
        t("{0} 下载失败：{1}", {
          "0": snap.VersionID,
          "1": snap.Detail || "未知原因",
        }),
      );
    } else if (snap.Phase === GAME_PHASE_CANCELLED) {
      setTaskStatusText(t("{0} 下载已取消", { "0": snap.VersionID }));
    }
  }

  // 下载中时不再提供页内取消：统一到页内任务面板 / 右下角下载中心操作

  // ---------- 通用 ----------
  function onRefresh() {
    if (activeTab === "Minecraft 本体") void loadVersions();
    else if (MODRINTH_TABS.includes(activeTab))
      void searchModrinth(activeTab, contentQuery.trim());
  }

  // ---------- 生命周期：初始化 + 事件订阅（无阻塞遮罩，失败不锁界面） ----------
  useEffect(() => {
    void loadVersions();
    // 资源平台元信息（CurseForge Key 是否已配置、申请地址），失败不阻塞页面
    void GetResourceSources()
      .then((list) => setSources(asArray<models.ResourceSourceInfo>(list)))
      .catch(() => {});
    // 逐个退订：EventsOff 会连右下角下载中心 / 主页下载卡片的订阅一起清掉
    const offProgress = EventsOn("download:progress", applyDownloadSnapshot);

    return () => {
      offProgress();
    };
  }, []);

  // 主页 Java 卡片等请求打开 Java 标签页；全局拖放的整合包请求打开导入流程。
  // 两个来源：页面已挂载 → 走导航总线实时事件；跨页跳过来 → 读挂载时暂存的 detail
  useEffect(() => {
    const openJavaTab = () => switchTab("Java");
    const openImportedModpack = (detail: string) => {
      setContentOverlay({
        project: null,
        kind: "modpack",
        localPath: detail.slice("import-modpack:".length),
      });
    };

    const pending = consumePendingDetail("download");

    if (pending === "java") openJavaTab();
    else if (pending?.startsWith("import-modpack:"))
      openImportedModpack(pending);

    return onNavigate((request) => {
      if (request.pageId !== "download") return;
      if (request.detail === "java") openJavaTab();
      else if (request.detail?.startsWith("import-modpack:")) {
        openImportedModpack(request.detail);
      }
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // 翻页后把当前标签的列表滚回顶部（列表有自己的滚动区，翻页不会自动复位）
  const pageRootRef = useRef<HTMLDivElement>(null);
  const resetListScroll = () => {
    pageRootRef.current
      ?.querySelectorAll<HTMLElement>(".nya-scroll-area")
      .forEach((el) => {
        el.scrollTop = 0;
      });
  };

  // 分页条 / 空态 / 加载行统一走共享组件（components/pager | empty-state | loading-row）
  return (
    <div
      ref={pageRootRef}
      className="relative flex h-full min-h-0 w-full flex-col gap-4 overflow-hidden px-6 py-5"
    >
      {/* 标题区 */}
      <div className="flex flex-none items-center gap-3">
        <div className="flex min-w-0 flex-1 flex-col gap-0.5">
          <h1 className="overflow-hidden text-xl font-bold tracking-tight text-ellipsis whitespace-nowrap">
            {t("下载大厅")}
          </h1>
          {taskStatusText && !gameRunning && !finishedVersion ? (
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
        {/* 插件页面按钮插槽：没有插件注册时组件直接返回 null，不占位 */}
        <PageActionSlot pageId="download" />
      </div>

      {/* 页内任务面板：全部下载任务的实时进度 / 速度 / 暂停取消 / 清除已完成
          （与右下角下载中心同源；没有任何任务时整块不占位） */}
      <DownloadTaskPanel />

      {/* 标签栏（无卡：下划线标签 + 一条基线细线，不再铺胶囊外壳与滑块） */}
      <div className="nya-border nya-scroll flex flex-none items-center gap-5 overflow-x-auto border-b">
        {TAB_NAMES.map((tab) => (
          <button
            key={tab}
            className={`flex flex-none cursor-pointer items-center gap-1.5 border-b-2 px-0.5 pt-1 pb-2 text-[13px] transition-colors ${
              tab === activeTab
                ? "border-primary font-semibold text-primary"
                : "border-transparent text-gray-600 dark:text-gray-300 hover:text-gray-900 dark:hover:text-gray-100"
            }`}
            type="button"
            onClick={() => switchTab(tab)}
          >
            {TAB_ICONS[tab]}
            <span>{t(tab)}</span>
          </button>
        ))}
      </div>

      {/* 内容区：工具栏与分页条固定，只有列表本身滚动（Java 标签整页滚动） */}
      <div className="flex min-h-0 flex-1 flex-col">
        <SwitchTransition
          activeKey={activeTab}
          className={`flex min-h-0 flex-1 flex-col gap-3 ${
            activeTab === "Java" ? "nya-scroll nya-scroll-area pr-1" : ""
          }`}
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
                  popoverProps={selectPopoverProps}
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
                <div className="min-h-0 flex-1">
                  <LoadingRow text={t("正在获取版本清单…")} />
                </div>
              ) : versionPageItems.length === 0 ? (
                <div className="min-h-0 flex-1">
                  <EmptyState
                    icon={<Cube20Regular className="h-8 w-8" />}
                    text={versionLoadError || t("没有找到匹配的版本")}
                  />
                </div>
              ) : (
                <div className="nya-scroll nya-scroll-area flex min-h-0 flex-1 flex-col gap-1.5 pr-1">
                  {versionPageItems.map((v) => {
                    const meta = versionTypeMeta(v.type);

                    return (
                      <button
                        key={v.id}
                        className="group flex flex-none cursor-pointer items-center gap-3 rounded-lg px-3 py-2.5 text-left transition-colors hover:bg-primary/[0.08]"
                        onClick={() => setMcOverlayVersion(v)}
                      >
                        <span className="flex size-9 flex-none items-center justify-center rounded-md bg-default-100 text-primary dark:bg-default-100/60">
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
              <div className="flex flex-none items-center justify-between gap-3">
                <span className="text-xs text-gray-400">
                  {t("共")} {versionFiltered.length} {t("个版本")}
                </span>
                <Pager
                  page={versionPage}
                  totalPages={versionTotalPages}
                  onChange={(page) => {
                    setVersionPage(page);
                    resetListScroll();
                  }}
                />
              </div>
            </>
          )}

          {/* ===== Modrinth / CurseForge 内容类 ===== */}
          {MODRINTH_TABS.includes(activeTab) && (
            <>
              <div className="flex flex-none flex-wrap items-center gap-2">
                {/* 资源平台筛选：胶囊滑块切换（滑动指示条），未配置 Key 直接标在名字后 */}
                <SegmentedTabs
                  className="flex flex-none flex-shrink-0 items-center gap-1 rounded-full border nya-border p-1"
                  items={CONTENT_SOURCES.map((item) => {
                    const info = sources.find((s) => s.id === item.id);

                    return {
                      key: item.id,
                      label: (
                        <span className="flex items-center">
                          {item.label}
                          {info?.requiresApiKey && !info.apiKeyConfigured ? (
                            <span className="ml-1 text-[10px] opacity-80">
                              {t("（未配置 Key）")}
                            </span>
                          ) : null}
                        </span>
                      ),
                    };
                  })}
                  layoutId="nya-download-source"
                  value={contentSource}
                  onChange={(next) => switchContentSource(next)}
                />
                <Input
                  aria-label={t("搜索{0}", { "0": t(activeTab) })}
                  className="min-w-[180px] flex-1"
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
                    popoverProps={selectPopoverProps}
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
              {/* 内置 Key 未生效：提示而不是报错（Key 唯一来源是后端编译期内置值） */}
              {contentState.needsApiKey ? (
                <div className="flex flex-col gap-2 border-l-2 border-warning-400 pl-3.5">
                  <span className="flex items-center gap-2 text-xs text-warning-600 dark:text-warning-400">
                    <Warning20Regular />
                    {contentState.message ||
                      t(
                        "CurseForge 搜索需要内置 API Key，当前构建未包含，请使用官方发布版。",
                      )}
                  </span>
                </div>
              ) : null}
              {contentState.loading ? (
                <div className="min-h-0 flex-1">
                  <LoadingRow text={`${t("正在搜索")} ${t(activeTab)}…`} />
                </div>
              ) : contentState.error ? (
                <div className="flex min-h-0 flex-1 flex-col items-center justify-center gap-3 text-center">
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
                <div className="min-h-0 flex-1">
                  <EmptyState
                    icon={TAB_ICONS[activeTab]}
                    text={t("没有找到匹配的{0}", { "0": t(activeTab) })}
                  />
                </div>
              ) : (
                <div className="nya-scroll nya-scroll-area flex min-h-0 flex-1 flex-col gap-1.5 pr-1">
                  {contentPageItems.map((p) => (
                    <button
                      key={String(p.project_id)}
                      className="group flex flex-none cursor-pointer items-center gap-3 rounded-lg px-3 py-2.5 text-left transition-colors hover:bg-primary/[0.08]"
                      onClick={() => downloadContent(p)}
                    >
                      {p.icon_url ? (
                        <img
                          alt=""
                          className="size-10 flex-none rounded-md object-cover"
                          decoding="async"
                          loading="lazy"
                          src={String(p.icon_url)}
                        />
                      ) : (
                        <span className="flex size-10 flex-none items-center justify-center rounded-md bg-primary/15 text-primary">
                          {TAB_ICONS[activeTab]}
                        </span>
                      )}
                      <div className="flex min-w-0 flex-1 flex-col gap-0.5">
                        <span
                          className="overflow-hidden text-sm font-semibold text-ellipsis whitespace-nowrap"
                          title={
                            contentNames[String(p.title ?? "")]
                              ? // 与实例页同一展示格式 "(中文名) 原名"
                                `(${contentNames[String(p.title ?? "")]}) ${String(p.title ?? "")}`
                              : String(p.title ?? "")
                          }
                        >
                          {contentNames[String(p.title ?? "")]
                            ? `(${contentNames[String(p.title ?? "")]}) ${String(p.title ?? "")}`
                            : String(p.title ?? "")}
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
              <div className="flex flex-none items-center justify-between gap-3">
                <span className="text-xs text-gray-400">
                  {t("来自 {0} · 共", {
                    "0":
                      CONTENT_SOURCES.find((item) => item.id === contentSource)
                        ?.label ?? "Modrinth",
                  })}{" "}
                  {contentFiltered.length} {t("个结果")}
                </span>
                <Pager
                  page={contentPage}
                  totalPages={contentTotalPages}
                  onChange={(page) => {
                    setContentPage(page);
                    resetListScroll();
                  }}
                />
              </div>
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
