/*
 * 内容下载弹层（移植自旧版 components/overlay/ContentDownloadOverlay.tsx，对应
 * Avalonia ContentDownloadOverlay.axaml + DownloadStatusPanel）：Modrinth 内容
 * （Mod / 整合包 / 光影包 / 材质包）版本选择 + 目标实例选择 + 下载进度。
 *
 * 逻辑：
 * - 版本列表经 DownloadAPI.ListResourceVersions（Go 侧拉取
 *   api.modrinth.com/v2/project/{id}/version，失败自动回退国内镜像），
 *   MC 版本 / 加载器双过滤（加载器为 "minecraft" 占位值时不过滤）；
 * - 目标实例经 InstanceAPI.GetCurrentInstanceSnapshot + DownloadAPI.ResolveContentDirectoryForInstance；
 * - Mod/资源包/光影 → DownloadFileToInstance（进度 download:contentProgress）；
 *   自定义路径 → SystemAPI.SaveFile + DownloadFileToPath；
 * - 整合包 → 下载到临时目录 + InstallModpackToInstance，完成后清理临时目录；
 * - 本地整合包导入（localModpackPath 非空）→ 跳过在线版本选择，ReadModpackRequirements 读取要求；
 * - 整合包导入闭环：声明了 MC 版本而本机没有时，先走标准游戏安装
 *   （带加载器 → StartModLoaderDownload 建独立实例；原版 → StartDownload），
 *   装完再解压内容到与启动判定一致的内容目录——保证导入即可启动。
 * 内容下载后端无取消入口，不提供取消按钮。
 */
import type { download, instance, models } from "../../../wailsjs/go/models";

import React, { useEffect, useMemo, useRef, useState } from "react";
import {
  Button,
  Input,
  Modal,
  ModalContent,
  Select,
  SelectItem,
} from "@heroui/react";
import { ChevronDown20Regular, Search20Regular } from "@fluentui/react-icons";

import { selectPopoverProps } from "../../lib/motion";
import { ModalShell, modalBehaviorProps } from "../modal-shell";
import { asArray } from "../../lib/guards";
import {
  DownloadFileToInstance,
  DownloadFileToPath,
  GetCurrentDownloadSnapshot,
  GetModLoaderVersions,
  GetVersions,
  InstallModpackToInstance,
  ListResourceVersions,
  ReadModpackRequirements,
  ResolveContentDirectoryForInstance,
  StartDownload,
  StartModLoaderDownload,
} from "../../../wailsjs/go/bindings/DownloadAPI";
import {
  GetCurrentInstanceSnapshot,
  GetInstalledVersionIds,
  RefreshInstances,
} from "../../../wailsjs/go/bindings/InstanceAPI";
import { DeleteDirectory } from "../../../wailsjs/go/bindings/LauncherAPI";
import { SaveFile as pickSaveFile } from "../../../wailsjs/go/bindings/SystemAPI";
import { notify } from "../overlay/dialog";
import { t } from "../../i18n";

export type ContentKind = "mod" | "modpack" | "resourcepack" | "shaderpack";

// Modrinth 搜索结果里用得到的字段（其余透传不关心）
export interface ProjectLike {
  project_id?: string;
  // 资源平台（modrinth / curseforge）；缺省 modrinth（旧调用方兼容）
  source?: string;
  title?: string;
  description?: string;
  icon_url?: string;
  downloads?: number;
  follows?: number;
}

interface Props {
  open: boolean;
  project: ProjectLike | null; // Modrinth 搜索结果（本地整合包导入模式为 null）
  kind: ContentKind;
  localModpackPath: string; // 非空 = 本地整合包导入模式
  onClose: () => void;
}

interface ModrinthVersion {
  id: string;
  name?: string;
  version_number: string;
  date_published?: string;
  game_versions?: string[];
  loaders?: string[];
  files?: Array<{
    url?: string;
    filename?: string;
    size?: number;
    primary?: boolean;
  }>;
}

// 单个加载器的版本分组（折叠列表的一节）
interface LoaderGroup {
  loader: string;
  versions: ModrinthVersion[];
}

// 内容类型 → 实例内容子目录（Mod 固定 mods，见 DownloadFileToInstance 的缺省）
const SUB_DIRS: Partial<Record<ContentKind, string>> = {
  resourcepack: "resourcepacks",
  shaderpack: "shaderpacks",
};
// Go ModLoaderType 枚举 → 显示名
const LOADER_NAMES: Record<number, string> = {
  0: "",
  1: "Fabric",
  2: "Quilt",
  3: "NeoForge",
  4: "Forge",
};

// GameDownloadPhase 终态值（见 internal/download/game_download_service.go）
const GAME_PHASE_FAILED = 4;

// 折叠分组的展示顺序：常见加载器在前，其余按字母序
const LOADER_GROUP_ORDER = ["fabric", "forge", "neoforge", "quilt"];

function loaderGroupLabel(loader: string): string {
  if (loader === "other") return t("其他");

  return loader.charAt(0).toUpperCase() + loader.slice(1);
}

function versionLabel(v: ModrinthVersion): string {
  return v.name && v.name !== v.version_number
    ? `${v.name}（${v.version_number}）`
    : v.version_number;
}
function primaryFile(v: ModrinthVersion) {
  return (v.files ?? []).find((f) => f.primary) ?? (v.files ?? [])[0] ?? null;
}
function fileNameOf(path?: string | null): string {
  return (
    String(path ?? "")
      .split(/[\\/]/)
      .pop() || ""
  );
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
// MC 版本号按数值降序（1.20.10 > 1.20.2），位数不齐时按 0 补齐
function mcVersionSortDesc(a: string, b: string): number {
  const pa = a.split(".").map((n) => parseInt(n, 10) || 0);
  const pb = b.split(".").map((n) => parseInt(n, 10) || 0);

  for (let i = 0; i < 3; i++) {
    if ((pa[i] ?? 0) !== (pb[i] ?? 0)) return (pb[i] ?? 0) - (pa[i] ?? 0);
  }

  return b.localeCompare(a);
}

// OverlayHelpers.IsValidInstanceName
function validateInstanceName(name: string): string {
  if (!name) return t("请输入新实例的名字。");
  if (name === "." || name === "..")
    return t("实例名包含不安全字符，请换一个名字。");
  if (/[\\/:*?"<>|]/.test(name))
    return t("实例名包含不安全字符，请换一个名字。");

  return "";
}

const ContentDownloadOverlay: React.FC<Props> = ({
  open,
  project,
  kind,
  localModpackPath,
  onClose,
}) => {
  const isLocalModpack = !!localModpackPath;

  // 版本列表与过滤
  const [allVersions, setAllVersions] = useState<ModrinthVersion[]>([]);
  const [versionLoading, setVersionLoading] = useState(false);
  const [gameVersionFilter, setGameVersionFilter] = useState("所有版本");
  // 按加载器分组的可折叠列表（替代原加载器 ComboBox + 版本 Select）
  const [loaderGroups, setLoaderGroups] = useState<LoaderGroup[]>([]);
  const [expandedLoaders, setExpandedLoaders] = useState<string[]>([]);
  const [selectedVersionId, setSelectedVersionId] = useState("");
  // 左栏 MC 版本列表的关键词筛选
  const [gameQuery, setGameQuery] = useState("");
  // 左栏 MC 版本类型筛选：默认只显示正式版（快照 / rc / pre 归入"全部"）
  const [mcReleaseOnly, setMcReleaseOnly] = useState(true);
  // 右栏版本搜索词
  const [versionQuery, setVersionQuery] = useState("");

  const [snapshot, setSnapshot] =
    useState<instance.GameInstanceSnapshot | null>(null);
  const [targetId, setTargetId] = useState("");
  const [newInstanceName, setNewInstanceName] = useState("");

  const [idleText, setIdleText] = useState("");
  const [statusFileText, setStatusFileText] = useState("");
  const [statusDetail, setStatusDetail] = useState("");
  const [statusText, setStatusText] = useState("");
  const [downloading, setDownloading] = useState(false);

  // 下载流程回调中读最新值（避免闭包陈旧）
  const stateRef = useRef({ snapshot, targetId });

  stateRef.current = { snapshot, targetId };
  const loadSeq = useRef(0);

  const headerTitle = isLocalModpack ? t("导入整合包") : (project?.title ?? "");
  const headerSubtitle = isLocalModpack
    ? fileNameOf(localModpackPath)
    : (project?.description ?? "");

  // SetupVersionFilters：MC 版本候选在渲染时按 mcReleaseOnly 派生（见 gameVersionEntries），
  // 默认选中最新的正式版（不再提供"所有版本"选项）
  function setupVersionFilters(first: string) {
    setGameVersionFilter(first);
  }

  // 正式版判定：纯数字点分段（1.21.11 / 26.3），snapshot / rc / pre 等后缀都不算
  function isReleaseGameVersion(v: string): boolean {
    return /^\d+(\.\d+)+$/.test(v);
  }

  // 按加载器分组（MC 版本过滤后），组内按发布时间降序；分组顺序见 LOADER_GROUP_ORDER
  function computeLoaderGroups(
    list: ModrinthVersion[],
    gameVersion: string,
  ): LoaderGroup[] {
    const gv = gameVersion || null;
    const filtered = list.filter(
      (v) =>
        !gv ||
        (v.game_versions ?? []).some(
          (g) => g.toLowerCase() === gv.toLowerCase(),
        ),
    );
    const map = new Map<string, ModrinthVersion[]>();

    for (const v of filtered) {
      // 一条版本声明多个加载器时归入首个真实加载器（"minecraft" 占位跳过）
      const key =
        (v.loaders ?? []).find((l) => l && l.toLowerCase() !== "minecraft") ||
        "other";
      const arr = map.get(key);

      if (arr) arr.push(v);
      else map.set(key, [v]);
    }

    return [...map.keys()]
      .sort((a, b) => {
        const ia = LOADER_GROUP_ORDER.indexOf(a);
        const ib = LOADER_GROUP_ORDER.indexOf(b);

        if (ia !== ib) return (ia < 0 ? 99 : ia) - (ib < 0 ? 99 : ib);

        return a.localeCompare(b);
      })
      .map((loader) => ({
        loader,
        versions: (map.get(loader) ?? []).sort((a, b) =>
          String(b.date_published ?? "").localeCompare(
            String(a.date_published ?? ""),
          ),
        ),
      }));
  }

  // MC 版本筛选变化：重算分组，清空选中，展开首个非空组
  function applyGameVersionFilter(gameVersion: string) {
    setGameVersionFilter(gameVersion);
    const groups = computeLoaderGroups(allVersions, gameVersion);

    setLoaderGroups(groups);
    setSelectedVersionId("");
    setExpandedLoaders(groups[0]?.versions.length ? [groups[0].loader] : []);
  }

  function toggleLoaderGroup(loader: string) {
    setExpandedLoaders((current) =>
      current.includes(loader)
        ? current.filter((l) => l !== loader)
        : [...current, loader],
    );
  }

  async function loadVersions() {
    const pid = project?.project_id;

    if (!pid) return;
    const seq = ++loadSeq.current;

    setVersionLoading(true);
    setAllVersions([]);
    setLoaderGroups([]);
    setExpandedLoaders([]);
    setSelectedVersionId("");
    try {
      // 走后端绑定而非前端直连 fetch：官方域名在国内经常超时，Go 侧带
      // 超时控制并自动回退国内镜像（与搜索接口同一通道）。绑定本身是
      // Promise 异步调用，等待期间界面保持可交互（左栏显示加载态）。
      // source 跟随打开来源的平台（Modrinth / CurseForge）。
      const result = await ListResourceVersions({
        source: project?.source ?? "modrinth",
        projectId: pid,
        gameVersion: "",
        loader: "",
      });

      if (seq !== loadSeq.current) return;
      // 统一模型 → 本组件的 Modrinth 形状（只映射用得到的字段）
      const list: ModrinthVersion[] = asArray<models.ResourceVersion>(
        result?.versions,
      ).map((v) => ({
        id: v.versionId,
        name: v.name ?? "",
        version_number: v.versionNumber,
        date_published: v.datePublished,
        game_versions: v.gameVersions ?? [],
        loaders: v.loaders ?? [],
        files: v.fileUrl
          ? [
              {
                url: v.fileUrl,
                filename: v.fileName,
                size: Number(v.fileSize ?? 0),
                primary: true,
              },
            ]
          : [],
      }));

      setAllVersions(list);
      // 默认按最新的正式版过滤（setupVersionFilters 计算首个候选）
      const all = [...new Set(list.flatMap((v) => v.game_versions ?? []))].sort(
        mcVersionSortDesc,
      );
      const defaultGameVersion = mcReleaseOnly
        ? (all.find(isReleaseGameVersion) ?? all[0])
        : all[0];

      setupVersionFilters(defaultGameVersion ?? "");
      const groups = computeLoaderGroups(list, defaultGameVersion ?? "");

      setLoaderGroups(groups);
      setExpandedLoaders(groups[0]?.versions.length ? [groups[0].loader] : []);
    } catch (ex) {
      if (seq !== loadSeq.current) return;
      setStatusText(
        t("加载版本失败：{0}", { "0": (ex as Error)?.message ?? ex }),
      );
    } finally {
      if (seq === loadSeq.current) setVersionLoading(false);
    }
  }

  // 本地整合包导入：解析声明的 MC / Loader 要求并提示
  async function prepareLocalModpack(path: string) {
    setStatusDetail(t("正在解析整合包所需的游戏版本…"));
    try {
      const req = await ReadModpackRequirements(path);

      if (req?.MinecraftVersion) {
        setIdleText(
          t("要求 MC {0}", { "0": req.MinecraftVersion }) +
            (req.RawLoaderKey
              ? ` + ${LOADER_NAMES[req.LoaderType] || req.RawLoaderKey} ${req.LoaderVersion || ""}`
              : t("（原版）")),
        );
      }
    } catch (ex) {
      console.error(t("读取整合包要求失败"), ex);
    }
    setStatusDetail("");
  }

  // 打开时复位 + 拉数据 + 订阅内容下载进度
  useEffect(() => {
    if (!open) return;
    setStatusText("");
    setStatusFileText("");
    setStatusDetail("");
    setIdleText("");
    setDownloading(false);
    setNewInstanceName("");
    setTargetId("");
    loadSeq.current++;

    let cancelled = false;

    void (async () => {
      let snap: instance.GameInstanceSnapshot | null = null;

      try {
        snap = await GetCurrentInstanceSnapshot();
      } catch (ex) {
        console.error(t("读取实例快照失败"), ex);
        snap = null;
      }
      if (cancelled) return;
      setSnapshot(snap);
      if (isLocalModpack) await prepareLocalModpack(localModpackPath);
      else await loadVersions();
    })();

    return () => {
      cancelled = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, localModpackPath, project?.project_id]);

  function onVersionPicked(v: ModrinthVersion) {
    setSelectedVersionId(v.id);
    const file = primaryFile(v);

    setIdleText(
      file ? `${versionLabel(v)} · ${formatBytes(file.size)}` : versionLabel(v),
    );
  }

  function beginProgress(fileName: string) {
    setDownloading(true);
    setStatusFileText(fileName);
    setStatusDetail("");
    // 发起下载的轻提示：进度统一在右下角下载中心跟踪
    notify.info(t("已开始下载，进度见右下角的下载中心"));
  }

  function finish(message: string) {
    setDownloading(false);
    setStatusDetail(message);
    setStatusFileText(t("完成"));
  }

  // 确保整合包声明的游戏版本已安装。带加载器时总是新建独立实例（干净、
  // 不污染已有实例）；原版时已装则直接复用。返回错误信息（空串 = 成功）。
  async function ensureRequiredVersion(
    requirements: download.ModpackRequirements,
    instanceName: string,
    minecraftDirectory: string,
  ): Promise<string> {
    const mcVersion = requirements.MinecraftVersion;
    let gameVersion: models.MinecraftVersion | null = null;

    try {
      const list = (await GetVersions()) ?? [];

      gameVersion =
        list.find((v) => v.id?.toLowerCase() === mcVersion.toLowerCase()) ??
        null;
    } catch {
      /* 清单拉取失败：下面按可否复用已装版本分流 */
    }

    if (requirements.LoaderType) {
      if (!gameVersion)
        return t("下载源中没有找到 MC {0}，无法自动安装。", { "0": mcVersion });
      if (!instanceName.trim())
        return t("请先给新实例取个名字（需要为整合包创建独立实例）。");

      let loader: download.ModLoaderVersion | null = null;

      try {
        const loaders =
          (await GetModLoaderVersions(
            requirements.LoaderType as never,
            mcVersion,
          )) ?? [];

        loader =
          loaders.find(
            (item) =>
              requirements.LoaderVersion &&
              item.LoaderVersion === requirements.LoaderVersion,
          ) ??
          loaders[0] ??
          null;
      } catch {
        /* 拉取失败按无可用版本处理 */
      }
      if (!loader)
        return t("没有找到 {0} 的可用版本（MC {1}）。", {
          "0": LOADER_NAMES[requirements.LoaderType] || "加载器",
          "1": mcVersion,
        });

      // 后端 StartModLoaderDownload 是阻塞式调用：返回 true 时安装已经全部
      // 完成（终态快照已发布），返回 false 则是被并发任务拒绝或安装失败。
      // 不能在这里再等 download:progress —— 终态 Revision 已是最新，等待器
      // 会因"没有更新的快照"空转 15 秒后误报失败，整合包内容也随之不解压。
      const started = await StartModLoaderDownload(
        gameVersion,
        loader,
        instanceName.trim(),
        true,
      ).catch(() => false);

      if (!started) return await gameDownloadFailureReason();

      return "";
    }

    // 原版：已装复用（内容装进该版本对应的内容目录）
    try {
      const installed =
        (await GetInstalledVersionIds(minecraftDirectory)) ?? [];

      if (installed.some((id) => id.toLowerCase() === mcVersion.toLowerCase()))
        return "";
    } catch {
      /* 查询失败按未安装处理 */
    }
    if (!gameVersion)
      return t("下载源中没有找到 MC {0}，无法自动安装。", { "0": mcVersion });

    // 同上：阻塞式调用，返回 true 即安装完成
    const started = await StartDownload(gameVersion).catch(() => false);

    if (!started) return await gameDownloadFailureReason();

    return "";
  }

  /** 任务被拒 / 安装失败时，从终态快照里取真实原因（阻塞式调用返回时已发布）。 */
  async function gameDownloadFailureReason(): Promise<string> {
    const snap = await GetCurrentDownloadSnapshot().catch(() => null);

    if (snap && snap.Phase === GAME_PHASE_FAILED && snap.Detail)
      return t("游戏版本安装失败：{0}", { "0": snap.Detail });

    return t("游戏安装任务启动失败（可能有正在进行的下载）。");
  }

  // 整合包安装：确保所需版本就绪 → 解压到实例内容目录 + 下载声明依赖 → 汇总
  async function runModpackInstall(
    packPath: string,
    instanceName: string,
    cleanupDir = "",
  ) {
    const snap = stateRef.current.snapshot;

    if (!snap?.MinecraftDirectory) {
      setStatusText(t("无法定位游戏目录。"));

      return;
    }
    try {
      setStatusDetail(t("正在解析整合包所需的游戏版本…"));
      const requirements = await ReadModpackRequirements(packPath).catch(
        () => null,
      );

      // 识别不出版本要求（包根没有 modrinth.index.json / manifest.json /
      // mmc-pack.json，或清单损坏）时不能继续：内容不知道挂到哪个实例、
      // 加载器也无从安装，倒进共享根目录只会污染现有实例——明确拒绝。
      if (!requirements?.MinecraftVersion) {
        setDownloading(false);
        setStatusText(
          t(
            "无法从整合包读取版本要求（未找到可识别的清单文件），已取消安装。请确认这是 .mrpack / CurseForge zip / MultiMC 导出的整合包。",
          ),
        );

        return;
      }

      // 声明了 MC 版本时先确保游戏本体（+加载器）就绪，导入完成即可启动；
      // versionId 同时决定内容目录（与启动时的隔离判定完全一致）
      let versionId = "";

      if (requirements.MinecraftVersion) {
        // 声明了加载器、但本启动器识别不了（LoaderType 为 0 而 RawLoaderKey
        // 非空，如 rift-loader）：继续下去会走"原版"分支，把整合包内容倒进
        // 玩家已装的原版实例里——加载器没装上，现有版本还被污染了。
        // 这里在写任何文件之前拦下，并说清原因。
        if (requirements.RawLoaderKey && !requirements.LoaderType) {
          setDownloading(false);
          setStatusText(
            t(
              "整合包声明的加载器 {0} 暂不受支持，无法自动安装（继续会污染现有原版实例）。请手动为该版本装好加载器后再导入。",
              { "0": requirements.RawLoaderKey },
            ),
          );

          return;
        }

        const loaderName =
          LOADER_NAMES[requirements.LoaderType] || requirements.RawLoaderKey;

        setStatusDetail(
          t("正在准备 MC {0}", { "0": requirements.MinecraftVersion }) +
            (loaderName
              ? ` + ${loaderName} ${requirements.LoaderVersion || ""}`
              : "") +
            "…",
        );
        const ensureError = await ensureRequiredVersion(
          requirements,
          instanceName,
          snap.MinecraftDirectory,
        );

        if (ensureError) {
          setDownloading(false);
          setStatusText(ensureError);

          return;
        }
        versionId = requirements.LoaderType
          ? instanceName.trim()
          : requirements.MinecraftVersion;
      }

      const contentDir =
        (snap && versionId
          ? await ResolveContentDirectoryForInstance(
              snap.MinecraftDirectory,
              snap.SourcePath,
              versionId,
            )
          : "") || snap.MinecraftDirectory;

      setStatusDetail(t("正在解压整合包并安装依赖…"));
      const result = await InstallModpackToInstance(packPath, contentDir);

      await RefreshInstances(snap.MinecraftDirectory).catch(() => {
        /* 刷新失败不影响结果展示 */
      });
      const freshSnap = await GetCurrentInstanceSnapshot().catch(() => snap);

      stateRef.current.snapshot = freshSnap;
      setSnapshot(freshSnap);

      // 声明的运行要求以安装结果回填的为准：本地导入时 requirements 可能为 null
      // （包内清单在更靠内的位置），而安装侧一定读到了同一份清单。
      const mcVersion =
        requirements?.MinecraftVersion ||
        result?.DeclaredMinecraftVersion ||
        "";
      const loaderName =
        result?.DeclaredLoaderName ||
        (requirements?.RawLoaderKey
          ? LOADER_NAMES[requirements.LoaderType] || requirements.RawLoaderKey
          : "");
      const loaderVersion =
        result?.DeclaredLoaderVersion || requirements?.LoaderVersion || "";

      const reqText = !mcVersion
        ? t("未识别到版本要求")
        : t("目标版本：{0}（MC {1}", {
            "0": instanceName || "(选中实例)",
            "1": mcVersion,
          }) +
          (loaderName
            ? t("，加载器 {0} {1}", {
                "0": loaderName,
                "1": loaderVersion,
              })
            : t("，原版")) +
          "）";
      let summary = t("已解压 {0} 个文件", {
        "0": result?.InstalledFiles ?? 0,
      });

      if ((result?.DownloadedMods ?? 0) > 0)
        summary += t("、下载依赖 {0} 个", { "0": result.DownloadedMods });
      if ((result?.Errors ?? []).length > 0)
        summary += t("，{0} 项失败", { "0": result.Errors.length });
      if ((result?.Warnings ?? []).length > 0)
        summary += t("，{0} 项提示", { "0": result.Warnings.length });
      finish(`${summary}\n${reqText}`);
      // 逐条列出具体失败项（而不是只说"N 项失败"）：用户要据此判断
      // 是网络问题还是缺 API Key，光看数量无从下手。
      const failures = result?.Errors ?? [];

      if (failures.length > 0) {
        const shown = failures.slice(0, 3).join("；");
        const more =
          failures.length > 3
            ? t("（另有 {0} 项）", { "0": failures.length - 3 })
            : "";

        setStatusText(`${shown}${more}`);
      } else if ((result?.Warnings ?? []).length > 0) {
        // 没有硬失败时，告警才是用户真正需要看见的（例如加载器不受支持）
        setStatusText(result.Warnings.join("；"));
      }
    } finally {
      // 临时缓存目录清理（本地导入模式不传 cleanupDir，不删除用户源文件）
      if (cleanupDir)
        await DeleteDirectory(cleanupDir).catch(() => {
          /* 清理失败可忽略 */
        });
    }
  }

  // 整合包：先下载到游戏目录下的临时缓存目录再安装，最后清理
  async function downloadToTempAndInstall(
    file: NonNullable<ReturnType<typeof primaryFile>>,
    instanceName: string,
  ) {
    const snap = stateRef.current.snapshot;

    if (!snap?.MinecraftDirectory) {
      setStatusText(t("无法定位游戏目录。"));

      return;
    }
    const tempDir = `${snap.MinecraftDirectory.replace(/[\\/]+$/, "")}\\nya-modpack-tmp`;
    const tempPath = `${tempDir}\\${file.filename || "modpack.mrpack"}`;

    beginProgress(file.filename || "modpack.mrpack");
    await DownloadFileToPath(file.url!, tempPath);
    await runModpackInstall(tempPath, instanceName, tempDir);
  }

  async function onDownload() {
    if (downloading) return;
    setStatusText("");
    try {
      if (isLocalModpack) {
        // 本地导入与在线整合包一致：必须给新实例命名（版本与内容都挂在它上面）
        const name = newInstanceName.trim();
        const nameError = validateInstanceName(name);

        if (nameError) {
          setStatusText(nameError);

          return;
        }
        beginProgress(fileNameOf(localModpackPath));
        await runModpackInstall(localModpackPath, name);

        return;
      }
      const version =
        allVersions.find((v) => v.id === selectedVersionId) ?? null;

      if (!version) {
        setStatusText(t("请先选择版本。"));

        return;
      }
      const file = primaryFile(version);

      if (!file?.url) {
        setStatusText(t("所选版本无可下载文件。"));

        return;
      }
      if (kind === "modpack") {
        // 整合包只能安装为独立实例：用户自定义名字
        const name = newInstanceName.trim();
        const nameError = validateInstanceName(name);

        if (nameError) {
          setStatusText(nameError);

          return;
        }
        await downloadToTempAndInstall(file, name);
      } else if (targetId === "__custom__") {
        const savePath = await pickSaveFile(
          t("保存文件"),
          file.filename ?? "",
          t("内容文件"),
          "*.*",
        );

        if (!savePath) return;
        beginProgress(file.filename ?? "");
        await DownloadFileToPath(file.url, savePath);
        finish(t("已保存到 {0}", { "0": savePath }));
      } else {
        if (!targetId) {
          setStatusText(t("请选择下载目标。"));

          return;
        }
        const snap = stateRef.current.snapshot;
        const contentDir =
          (snap &&
            (await ResolveContentDirectoryForInstance(
              snap.MinecraftDirectory,
              snap.SourcePath,
              targetId,
            ))) ||
          "";

        if (!contentDir) {
          setStatusText(t("无法定位实例内容目录。"));

          return;
        }
        beginProgress(file.filename ?? "");
        await DownloadFileToInstance(
          file.url,
          file.filename ?? "",
          contentDir,
          SUB_DIRS[kind] ?? "mods",
        );
        finish(t("已安装到 {0}", { "0": contentDir }));
      }
    } catch (ex) {
      setDownloading(false);
      setStatusText(t("操作失败：{0}", { "0": (ex as Error)?.message ?? ex }));
    }
  }

  const targetOptions = [
    ...(snapshot?.VersionIds ?? []).map((id) => ({ value: id, label: id })),
    { value: "__custom__", label: t("自定义保存路径…") },
  ];

  // 每个 MC 版本对应的内容版本数（左栏列表的计数徽章）
  const gameVersionCounts = useMemo(() => {
    const map = new Map<string, number>();

    for (const v of allVersions)
      for (const g of v.game_versions ?? []) map.set(g, (map.get(g) ?? 0) + 1);

    return map;
  }, [allVersions]);

  // 左栏 MC 版本候选：按"正式版/全部"筛选后降序
  const gameVersionEntries = useMemo(() => {
    const all = [
      ...new Set(allVersions.flatMap((v) => v.game_versions ?? [])),
    ].sort(mcVersionSortDesc);

    return mcReleaseOnly ? all.filter(isReleaseGameVersion) : all;
  }, [allVersions, mcReleaseOnly]);

  // 切到"正式版"时，已选中的快照版本可能不在候选里，回落到首个候选
  useEffect(() => {
    if (
      gameVersionFilter &&
      !gameVersionEntries.includes(gameVersionFilter) &&
      gameVersionEntries.length > 0
    ) {
      applyGameVersionFilter(gameVersionEntries[0]);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [gameVersionEntries]);

  const shownGameVersions = gameVersionEntries.filter(
    (g) =>
      !gameQuery.trim() ||
      g.toLowerCase().includes(gameQuery.trim().toLowerCase()),
  );

  // 右栏版本搜索：按关键词过滤各组版本，搜索中全部展开便于直接定位
  const displayGroups = useMemo(() => {
    const q = versionQuery.trim().toLowerCase();

    if (!q) return loaderGroups;

    return loaderGroups
      .map((g) => ({
        loader: g.loader,
        versions: g.versions.filter((v) =>
          versionLabel(v).toLowerCase().includes(q),
        ),
      }))
      .filter((g) => g.versions.length > 0);
  }, [loaderGroups, versionQuery]);
  const searchActive = !!versionQuery.trim();

  // 下载到区块（两种模式共用：整合包填新实例名，其余选目标实例）
  const downloadTargetBlock = (
    <div className="flex flex-col gap-2">
      <span className="text-[13px] font-semibold text-gray-600 dark:text-gray-300">
        {t("下载到")}
      </span>
      {kind === "modpack" ? (
        <Input
          maxLength={48}
          placeholder={t("给新实例取个名字，例如 MyModpack")}
          size="sm"
          value={newInstanceName}
          variant="bordered"
          onValueChange={setNewInstanceName}
        />
      ) : (
        <Select
          aria-label={t("下载目标")}
          items={targetOptions}
          placeholder={t("选择目标实例…")}
          popoverProps={selectPopoverProps}
          selectedKeys={targetId ? [targetId] : []}
          size="sm"
          variant="bordered"
          onSelectionChange={(keys) =>
            setTargetId(String(Array.from(keys)[0] ?? ""))
          }
        >
          {(item) => <SelectItem key={item.value}>{item.label}</SelectItem>}
        </Select>
      )}
    </div>
  );

  // 状态面板：进度条统一在右下角下载中心（含速度 / 剩余时间 / 取消），
  // 这里只保留文字状态，避免同一份进度在两处各画一条。
  const statusPanel = (
    <>
      {statusText ? (
        <span className="text-xs text-red-500">{statusText}</span>
      ) : null}
      <div className="flex flex-col gap-2 rounded-medium nya-panel-inner px-3.5 py-3">
        <span className="truncate text-xs text-gray-600 dark:text-gray-300">
          {statusFileText || idleText || t("未选择版本")}
        </span>
        {statusDetail && (
          <span className="whitespace-pre-line break-all text-[11px] text-gray-400">
            {statusDetail}
          </span>
        )}
        {downloading && (
          <span className="text-[11px] text-gray-400">
            {t("实时进度与剩余时间见右下角的下载中心")}
          </span>
        )}
      </div>
    </>
  );

  return (
    // isDismissable=false（modalBehaviorProps）：点外部不关闭，防止误触——关闭一律走右上角 X
    <Modal
      isOpen={open}
      onClose={onClose}
      {...modalBehaviorProps}
      // 弹层尽量占满窗口（此前 66.6vw/66.6vh 在 macOS 小窗口下双栏挤成一团，
      // 版本列表根本没法点）；上限 1280px 防止大屏上过度拉伸
      classNames={{
        ...modalBehaviorProps.classNames,
        base: `${modalBehaviorProps.classNames?.base ?? ""} h-[85vh]! w-[min(1280px,92vw)]! max-w-none`,
      }}
      scrollBehavior="inside"
    >
      <ModalContent className="h-full overflow-hidden">
        <ModalShell
          subtitle={headerSubtitle}
          title={headerTitle || t("下载内容")}
          onClose={onClose}
        >
          {/* 本地导入模式：无版本选择，只有下载到 + 状态面板 */}
          {isLocalModpack ? (
            <>
              {downloadTargetBlock}
              {statusPanel}
              <div className="flex justify-end">
                <Button
                  color="primary"
                  isDisabled={downloading}
                  size="sm"
                  onPress={() => void onDownload()}
                >
                  {t("安装")}
                </Button>
              </div>
            </>
          ) : (
            /* 双栏布局：左栏 MC 版本列表（类型筛选 + 关键词筛选 + 计数徽章），
             * 右栏版本搜索 + 加载器折叠分组 + 下载到 + 状态面板 + 下载按钮 */
            <div className="grid h-full min-h-0 grid-cols-[clamp(160px,22%,220px)_1fr] grid-rows-[minmax(0,1fr)] gap-4">
              <div className="flex min-h-0 flex-col gap-2">
                <span className="text-[13px] font-semibold text-gray-600 dark:text-gray-300">
                  {t("Minecraft 版本")}
                </span>
                <Input
                  aria-label={t("筛选版本")}
                  placeholder={t("筛选版本...")}
                  size="sm"
                  value={gameQuery}
                  variant="bordered"
                  onValueChange={setGameQuery}
                />
                <Select
                  aria-label={t("版本类型")}
                  popoverProps={selectPopoverProps}
                  selectedKeys={[mcReleaseOnly ? "release" : "all"]}
                  size="sm"
                  variant="bordered"
                  onSelectionChange={(keys) =>
                    setMcReleaseOnly(
                      String(Array.from(keys)[0] ?? "release") === "release",
                    )
                  }
                >
                  <SelectItem key="release">{t("正式版")}</SelectItem>
                  <SelectItem key="all">{t("全部")}</SelectItem>
                </Select>
                <div className="nya-panel-inner flex min-h-0 flex-1 flex-col gap-0.5 nya-scroll nya-scroll-area rounded-medium p-1.5">
                  {versionLoading ? (
                    <div className="flex items-center justify-center gap-2 px-3 py-6 text-xs text-gray-400">
                      <span className="size-4 animate-spin rounded-full border-2 border-current border-t-transparent" />
                      {t("正在加载版本…")}
                    </div>
                  ) : (
                    shownGameVersions.map((g) => {
                      const selected = g === gameVersionFilter;

                      return (
                        <button
                          key={g}
                          className={`flex cursor-pointer items-center gap-2 rounded-lg px-2.5 py-1.5 text-left text-[13px] transition-colors ${
                            selected
                              ? "bg-default-300/60 font-semibold text-gray-800 dark:bg-gray-700/70 dark:text-gray-100"
                              : "text-gray-600 hover:bg-default-200/60 dark:text-gray-300 dark:hover:bg-gray-800/60"
                          }`}
                          onClick={() => {
                            if (g !== gameVersionFilter)
                              applyGameVersionFilter(g);
                          }}
                        >
                          <span className="min-w-0 flex-1 truncate">{g}</span>
                          <span className="flex-shrink-0 rounded-full bg-default-200/80 px-1.5 py-0.5 text-[10px] font-semibold text-gray-500 dark:bg-gray-700/70 dark:text-gray-300">
                            {gameVersionCounts.get(g) ?? 0}
                          </span>
                        </button>
                      );
                    })
                  )}
                  {!versionLoading && shownGameVersions.length === 0 ? (
                    <span className="px-2 py-3 text-center text-[11px] text-gray-400">
                      {t("没有匹配的版本")}
                    </span>
                  ) : null}
                </div>
                <span className="flex-shrink-0 text-[11px] text-gray-400">
                  {t("{0} 个版本可用", { "0": allVersions.length })}
                </span>
              </div>

              <div className="flex min-h-0 min-w-0 flex-col gap-2">
                <Input
                  aria-label={t("搜索版本")}
                  placeholder={t("搜索版本...")}
                  size="sm"
                  startContent={<Search20Regular className="text-gray-400" />}
                  value={versionQuery}
                  variant="bordered"
                  onValueChange={setVersionQuery}
                />
                {versionLoading ? (
                  <div className="nya-panel-inner flex items-center justify-center gap-2 rounded-medium px-3.5 py-6 text-xs text-gray-400">
                    <span className="size-4 animate-spin rounded-full border-2 border-current border-t-transparent" />
                    {t("正在加载版本…")}
                  </div>
                ) : displayGroups.length === 0 ? (
                  <div className="nya-panel-inner flex items-center justify-center rounded-medium px-3.5 py-6 text-center text-xs text-gray-400">
                    {statusText || t("该过滤条件下没有可用版本")}
                  </div>
                ) : (
                  <div className="nya-scroll nya-scroll-area flex min-h-0 flex-1 flex-col gap-1.5 pr-0.5">
                    {displayGroups.map((group) => {
                      const expanded =
                        searchActive || expandedLoaders.includes(group.loader);

                      return (
                        <div
                          key={group.loader}
                          className="nya-panel-inner shrink-0 overflow-hidden rounded-medium"
                        >
                          <button
                            className="flex w-full cursor-pointer items-center gap-2 px-3 py-2 text-left transition-colors hover:bg-gray-200/60 dark:hover:bg-gray-800/60"
                            onClick={() => toggleLoaderGroup(group.loader)}
                          >
                            <ChevronDown20Regular
                              className={`size-4 flex-shrink-0 text-gray-400 transition-transform ${
                                expanded ? "rotate-180" : ""
                              }`}
                            />
                            <span className="flex-1 truncate text-[13px] font-semibold text-gray-700 dark:text-gray-200">
                              {loaderGroupLabel(group.loader)}
                            </span>
                            <span className="flex-shrink-0 rounded-full bg-default-200/70 px-2 py-0.5 text-[10px] font-semibold text-gray-500 dark:bg-gray-700/60 dark:text-gray-300">
                              {group.versions.length}
                            </span>
                          </button>
                          {expanded && (
                            <div className="flex flex-col gap-0.5 px-1.5 pb-1.5">
                              {group.versions.map((v) => {
                                const selected = v.id === selectedVersionId;
                                const file = primaryFile(v);

                                return (
                                  <button
                                    key={v.id}
                                    className={`flex cursor-pointer items-center gap-2 rounded-lg px-2.5 py-1.5 text-left transition-colors ${
                                      selected
                                        ? "bg-primary-100 dark:bg-primary-900/40"
                                        : "hover:bg-gray-200/60 dark:hover:bg-gray-800/60"
                                    }`}
                                    onClick={() => onVersionPicked(v)}
                                  >
                                    <span
                                      className={`min-w-0 flex-1 truncate font-mono text-[12px] ${
                                        selected
                                          ? "font-semibold text-primary-600 dark:text-primary-300"
                                          : "text-gray-600 dark:text-gray-300"
                                      }`}
                                    >
                                      {versionLabel(v)}
                                    </span>
                                    {file ? (
                                      <span className="flex-shrink-0 text-[11px] text-gray-400">
                                        {formatBytes(file.size)}
                                      </span>
                                    ) : null}
                                  </button>
                                );
                              })}
                            </div>
                          )}
                        </div>
                      );
                    })}
                  </div>
                )}
                {downloadTargetBlock}
                {statusPanel}
                <div className="flex justify-end">
                  <Button
                    color="primary"
                    isDisabled={downloading}
                    size="sm"
                    onPress={() => void onDownload()}
                  >
                    {kind === "modpack" ? t("安装") : t("下载")}
                  </Button>
                </div>
              </div>
            </div>
          )}
        </ModalShell>
      </ModalContent>
    </Modal>
  );
};

export default ContentDownloadOverlay;
