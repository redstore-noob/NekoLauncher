/*
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
 * 整合包制作（创作中心的二级界面）：界面完全对齐旧版 NyaLauncher 的
 * ModpackView（ModpackCreatorPage.axaml 的移植）——页头实例摘要、左侧
 * 打包内容（搜索 + 分类折叠 + 三态勾选 + 全选/恢复默认）、右侧打包设置
 * （格式单选 / 元数据 / 图标 / Modrinth 直链）、底部状态行。唯一新增是
 * 工具行的「实例版本」选择框（旧版跟随全局所选实例）；Minecraft 版本与
 * 加载器不作编辑，从所选实例自动识别并显示在右侧的目标信息里。
 * 后端为 internal/modpack 导出服务；导出档案（nya-pack.json）按版本目录
 * 存取，记录元数据与排除项，下次导出自动回填。
 */
import React, { useCallback, useEffect, useRef, useState } from "react";
import {
  Button,
  Checkbox,
  Input,
  Modal,
  ModalContent,
  Progress,
  Radio,
  RadioGroup,
  Select,
  SelectItem,
  Spinner,
  Textarea,
} from "@heroui/react";
import { FolderZip20Regular } from "@fluentui/react-icons";

import {
  EnsureDefaultMinecraftDirectory,
  GetCurrentInstanceSnapshot,
  GetInstalledVersionIds,
  GetVersionDetails,
  ResolveInstanceIsolation,
} from "../../../wailsjs/go/bindings/InstanceAPI";
import { GetGameDirectory } from "../../../wailsjs/go/bindings/ConfigAPI";
import {
  CancelExport,
  CollectExportContent,
  ExportModpack,
  ExportSoloPack,
  GetSoloStubStatus,
  DownloadSoloStub,
  LoadExportProfile,
  NewExportOptions,
  SaveExportProfile,
} from "../../../wailsjs/go/bindings/ModpackAPI";
import { SaveFile, SelectFile } from "../../../wailsjs/go/bindings/SystemAPI";
import { EventsOff, EventsOn } from "../../../wailsjs/runtime/runtime";
import { instance, modpack, solo } from "../../../wailsjs/go/models";
import { ModalShell, modalBehaviorProps } from "../modal-shell";
import { popoverMotionProps } from "../../lib/motion";
import { t } from "../../i18n";

/** 后端 modpack:exportProgress 事件负载（未生成绑定，字段见 exportservice.go） */
interface ExportProgress {
  Phase: string;
  Current: number;
  Total: number;
}

interface ContentRow {
  item: modpack.ModpackContentItem;
  included: boolean;
}

interface GroupView {
  category: string;
  items: ContentRow[];
  isExpanded: boolean;
  /** true 全选 / false 全不选 / null 半选 */
  masterChecked: boolean | null;
  countDisplay: string;
  sizeDisplay: string;
}

/** 后端 Category 原值 → 展示名（对应 Go 侧 CategoryDisplay 方法） */
const CATEGORY_DISPLAY: Record<string, string> = {
  mods: t("模组"),
  config: t("配置"),
  root: t("根文件"),
  resourcepacks: t("资源包"),
  shaderpacks: t("光影包"),
  saves: t("存档"),
};

function asMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

function trimZeros(text: string): string {
  return text.replace(/\.?0+$/, "");
}

function formatSize(bytes: number): string {
  if (bytes >= 1073741824)
    return `${trimZeros((bytes / 1073741824).toFixed(2))} GB`;
  if (bytes >= 1048576) return `${trimZeros((bytes / 1048576).toFixed(2))} MB`;
  if (bytes >= 1024) return `${trimZeros((bytes / 1024).toFixed(1))} KB`;

  return `${bytes} B`;
}

function sanitizeFileName(name: string): string {
  return name.trim().replace(/[<>:"/\\|?*]/g, "_");
}

const ModpackExportDialog: React.FC<{
  isOpen: boolean;
  onClose: () => void;
  /** 只导出 NekoSolo 安装包（创作中心的独立入口）：隐藏格式选择并固定 format=3 */
  soloOnly?: boolean;
}> = ({ isOpen, onClose, soloOnly = false }) => {
  const [snapshot, setSnapshot] =
    useState<instance.GameInstanceSnapshot | null>(null);
  const [versions, setVersions] = useState<string[]>([]);
  const [selectedVersion, setSelectedVersion] = useState("");
  const [details, setDetails] = useState<instance.GameVersionDetails | null>(
    null,
  );
  const [contentDir, setContentDir] = useState("");
  /** 导出档案（nya-pack.json）所在的版本目录，与内容目录区分 */
  const [profileDir, setProfileDir] = useState("");
  const [loadingVersion, setLoadingVersion] = useState(false);

  // 内容清单：行状态 + 按分类的折叠分组视图（跟随搜索过滤）
  const [rows, setRows] = useState<ContentRow[]>([]);
  const [groups, setGroups] = useState<GroupView[]>([]);
  const [filterQuery, setFilterQuery] = useState("");
  const expandedCategories = useRef<Set<string>>(new Set());
  /** 导出档案：元数据回填源 + 「恢复默认」基准 + 排除项持久化 */
  const profile = useRef<Partial<modpack.ModpackExportProfile>>({});

  // 表单（选项记忆：上次导出用的格式/元信息存 localStorage，下次打开自动带上）
  const EXPORT_PREFS_KEY = "nekolauncher-export-prefs";
  const loadExportPrefs = (): Record<string, unknown> => {
    try {
      return JSON.parse(
        localStorage.getItem(EXPORT_PREFS_KEY) ?? "{}",
      ) as Record<string, unknown>;
    } catch {
      return {};
    }
  };
  const exportPrefs = loadExportPrefs();
  const [format, setFormat] = useState(
    typeof exportPrefs.format === "number" ? exportPrefs.format : 0,
  );
  const [packName, setPackName] = useState("");
  const [packVersion, setPackVersion] = useState(
    typeof exportPrefs.packVersion === "string"
      ? exportPrefs.packVersion
      : "1.0.0",
  );
  const [author, setAuthor] = useState(
    typeof exportPrefs.author === "string" ? exportPrefs.author : "",
  );
  const [updateLink, setUpdateLink] = useState(
    typeof exportPrefs.updateLink === "string" ? exportPrefs.updateLink : "",
  );
  const [description, setDescription] = useState("");
  const [resolveLinks, setResolveLinks] = useState(
    typeof exportPrefs.resolveLinks === "boolean"
      ? exportPrefs.resolveLinks
      : true,
  );
  const [iconPngPath, setIconPngPath] = useState("");
  // NekoSolo 安装包（format 3）专属：捆绑首选 Java 运行时与安装器模板状态
  const [bundleJava, setBundleJava] = useState(
    typeof exportPrefs.bundleJava === "boolean" ? exportPrefs.bundleJava : true,
  );
  const [stubFound, setStubFound] = useState(true);
  // 模板缺失时在线补下（启动器 Release 的 NekoSolo.Installer.exe 资产）
  const [stubDownloading, setStubDownloading] = useState(false);
  const [stubMessage, setStubMessage] = useState("");
  // 在线安装包（v2）：载荷 zip 上传到 GitHub Releases 等 https 地址，
  // exe 只有小体积安装器 + 远程清单，玩家安装时动态下载
  const [remoteDist, setRemoteDist] = useState(false);
  const [payloadUrl, setPayloadUrl] = useState("");

  const [packing, setPacking] = useState(false);
  // cancelRequested 已点过「取消」：防重入，也让 catch 区分主动取消与真实失败
  const [cancelRequested, setCancelRequested] = useState(false);
  const [packStatus, setPackStatus] = useState("");
  const [statusText, setStatusText] = useState(t("就绪"));
  const [progressCurrent, setProgressCurrent] = useState(0);
  const [progressTotal, setProgressTotal] = useState(0);

  const progressPercent =
    progressTotal > 0 ? (100 * progressCurrent) / progressTotal : 0;

  // 在线补下安装器模板（存储目录 tools/NekoSolo/），成功后导出立即可用
  const downloadStub = async () => {
    setStubDownloading(true);
    setStubMessage("");
    try {
      await DownloadSoloStub();
      setStubFound(true);
    } catch (ex) {
      setStubMessage(
        ex instanceof Error ? ex.message : t("下载失败，请稍后重试。"),
      );
    } finally {
      setStubDownloading(false);
    }
  };

  const instanceSummary = (() => {
    if (!details) return contentDir || t("选择一个实例版本后开始制作");
    const loader = details.LoaderName
      ? `${details.LoaderName} ${details.LoaderVersion}`
      : t("原版");

    return `${details.VersionId || selectedVersion} · Minecraft ${details.BaseGameVersion} · ${loader} · ${contentDir}`;
  })();

  const targetInfo = (() => {
    const base =
      details?.BaseGameVersion && details.BaseGameVersion !== "0"
        ? details.BaseGameVersion
        : selectedVersion;

    if (!details) return t("Minecraft 版本与加载器取自所选实例。");

    return t("打包要求：Minecraft {0}{1}。来源实例：{2}。", {
      "0": base,
      "1": details.LoaderName
        ? ` + ${details.LoaderName} ${details.LoaderVersion}`
        : "（原版）",
      "2": details.VersionId,
    });
  })();

  const contentSummary = (() => {
    const total = rows.length;

    if (total === 0) return t("打包内容");
    const selected = rows.filter((row) => row.included);
    const size = selected.reduce(
      (sum, row) => sum + (row.item.SizeBytes || 0),
      0,
    );

    return t("打包内容 · 已选 {0} / {1} 项（{2}）", {
      "0": selected.length,
      "1": total,
      "2": formatSize(size),
    });
  })();

  const hasContent = rows.length > 0;

  // ------------------------------------------------------------------
  // 内容收集与分组（等价旧版 ModpackView 的同名逻辑）
  // ------------------------------------------------------------------

  function excludedSet(): Set<string> {
    const excluded = profile.current.excludedPaths ?? [];

    return new Set(excluded.map((p) => (p || "").toLowerCase()));
  }

  /**
   * 按搜索框重建分组视图：每个分类一个折叠组，只保留命中条目。
   * resetExpanded=true 时按「首次构建」规则：只展开有勾选内容的分类；搜索时全部展开。
   */
  const applyFilter = useCallback(
    (source: ContentRow[], query: string, resetExpanded = false) => {
      const q = query.trim().toLowerCase();
      const searching = q.length > 0;
      const result: GroupView[] = [];
      const byCategory = new Map<string, ContentRow[]>();

      for (const row of source) {
        const category = CATEGORY_DISPLAY[row.item.Category] || t("其他");

        if (!byCategory.has(category)) byCategory.set(category, []);
        byCategory.get(category)!.push(row);
      }
      for (const category of [...byCategory.keys()].sort()) {
        let visible = byCategory.get(category)!;

        if (searching) {
          visible = visible.filter(
            (row) =>
              (row.item.Name || "").toLowerCase().includes(q) ||
              (row.item.RelativePath || "").toLowerCase().includes(q),
          );
        }
        if (visible.length === 0) continue;

        let isExpanded: boolean;

        if (searching) {
          isExpanded = true;
        } else if (resetExpanded) {
          isExpanded = visible.some((row) => row.included);
        } else {
          isExpanded = expandedCategories.current.has(category);
        }

        const selected = visible.filter((row) => row.included).length;
        const size = visible.reduce(
          (sum, row) => sum + (row.item.SizeBytes || 0),
          0,
        );

        result.push({
          category,
          items: visible,
          isExpanded,
          masterChecked:
            selected === 0 ? false : selected === visible.length ? true : null,
          countDisplay: t("已选 {0} / {1}", {
            "0": selected,
            "1": visible.length,
          }),
          sizeDisplay: formatSize(size),
        });
      }
      setGroups(result);
      expandedCategories.current = new Set(
        result.filter((g) => g.isExpanded).map((g) => g.category),
      );
    },
    [],
  );

  /** 重新读取内容目录：重建行与分组视图，并回滚为档案记住的勾选 */
  const reloadContent = async (dir: string) => {
    if (!dir) return;
    setStatusText(t("正在读取实例内容…"));
    try {
      const items = ((await CollectExportContent(dir)) ??
        []) as modpack.ModpackContentItem[];
      const excluded = excludedSet();
      const nextRows: ContentRow[] = items.map((item) => ({
        item,
        included:
          excluded.size === 0 ||
          !excluded.has((item.RelativePath || "").toLowerCase()),
      }));

      setRows(nextRows);
      applyFilter(nextRows, filterQuery, true);
      const totalSize = items.reduce((sum, item) => sum + item.SizeBytes, 0);

      setStatusText(
        t("已读取 {0} 个内容条目（{1}）", {
          "0": items.length,
          "1": formatSize(totalSize),
        }),
      );
    } catch (ex) {
      setStatusText(t("读取实例内容失败：{0}", { "0": asMessage(ex) }));
    }
  };

  function onFilterInput(text: string) {
    setFilterQuery(text);
    applyFilter(rows, text);
  }

  /** 条目勾选变化后重算分组视图 */
  function updateRows(mutate: (source: ContentRow[]) => void) {
    const next = rows.map((row) => ({ ...row }));

    mutate(next);
    setRows(next);
    applyFilter(next, filterQuery);
  }

  function toggleRow(relativePath: string) {
    updateRows((source) => {
      for (const row of source) {
        if (row.item.RelativePath === relativePath)
          row.included = !row.included;
      }
    });
  }

  function toggleExpand(group: GroupView) {
    const expanded = !group.isExpanded;

    if (expanded) expandedCategories.current.add(group.category);
    else expandedCategories.current.delete(group.category);
    setGroups((prev) =>
      prev.map((g) =>
        g.category === group.category ? { ...g, isExpanded: expanded } : g,
      ),
    );
  }

  function setVisible(included: boolean) {
    // 只作用于当前分组视图里可见的条目，配合搜索可按需批量勾选
    const visibleKeys = new Set(
      groups.flatMap((g) => g.items.map((row) => row.item.RelativePath)),
    );

    updateRows((source) => {
      for (const row of source) {
        if (visibleKeys.has(row.item.RelativePath)) row.included = included;
      }
    });
  }

  function toggleGroup(group: GroupView) {
    // 三态主勾选框：点击时在「全选 / 全不选」间切换（半选状态视为未全选）
    setGroupChecked(group, group.masterChecked !== true);
  }

  function setGroupChecked(group: GroupView, included: boolean) {
    const keys = new Set(group.items.map((row) => row.item.RelativePath));

    updateRows((source) => {
      for (const row of source) {
        if (keys.has(row.item.RelativePath)) row.included = included;
      }
    });
  }

  function restoreDefaults() {
    const excluded = excludedSet();
    const next = rows.map((row) => ({
      ...row,
      included:
        excluded.size === 0 ||
        !excluded.has((row.item.RelativePath || "").toLowerCase()),
    }));

    setRows(next);
    applyFilter(next, filterQuery);
    setStatusText(t("已恢复为上次打包时的勾选列表。"));
  }

  // ------------------------------------------------------------------
  // 实例版本切换与档案回填
  // ------------------------------------------------------------------

  /** 切换实例版本：解析内容目录 → 档案回填 → 识别 MC/加载器 → 收集内容 */
  const loadVersion = async (
    snap: instance.GameInstanceSnapshot,
    versionId: string,
  ) => {
    const layout = await ResolveInstanceIsolation(snap, versionId);
    const dir = layout.ContentDirectory || "";

    if (!dir) throw new Error(t("无法解析该版本的内容目录"));
    const fetched = await GetVersionDetails(versionId).catch(() => null);
    // 导出档案按「版本目录」存取（nya-pack.json）：非隔离实例的内容目录是
    // 共享的游戏根目录，直接用它会让所有共享实例共用一份档案
    const profileDirectory =
      fetched?.VersionDirectory ||
      `${snap.MinecraftDirectory}/versions/${versionId}`;
    const [profileData, defaults] = await Promise.all([
      LoadExportProfile(profileDirectory).catch(() => null),
      NewExportOptions(),
    ]);

    profile.current = profileData ?? {};
    setDetails(fetched);
    setContentDir(dir);
    setProfileDir(profileDirectory);
    setPackName(profile.current.packName || versionId);
    setPackVersion(
      profile.current.packVersion ||
        defaults.PackVersion ||
        (typeof exportPrefs.packVersion === "string"
          ? exportPrefs.packVersion
          : "1.0.0"),
    );
    setAuthor(
      profile.current.author ||
        (typeof exportPrefs.author === "string" ? exportPrefs.author : ""),
    );
    setUpdateLink(
      profile.current.updateLink ||
        (typeof exportPrefs.updateLink === "string"
          ? exportPrefs.updateLink
          : ""),
    );
    setDescription(profile.current.description || "");
    setResolveLinks(
      profile.current.resolveModrinthLinks ??
        (typeof exportPrefs.resolveLinks === "boolean"
          ? exportPrefs.resolveLinks
          : true),
    );
    // 独立入口固定导出 NekoSolo 安装包；格式逐级回落：档案 → 个人偏好 → 默认值
    setFormat(
      soloOnly
        ? 3
        : (profile.current.format ??
            (typeof exportPrefs.format === "number"
              ? exportPrefs.format
              : (defaults.Format ?? 0))),
    );
    setPackStatus("");
    setProgressTotal(0);
    setProgressCurrent(0);
    setRows([]);
    setGroups([]);
    await reloadContent(dir);
  };

  const changeVersion = async (versionId: string) => {
    if (!snapshot || !versionId || versionId === selectedVersion) return;
    setSelectedVersion(versionId);
    setLoadingVersion(true);
    setStatusText(t("正在读取实例 {0}…", { "0": versionId }));
    try {
      await loadVersion(snapshot, versionId);
    } catch (ex) {
      setStatusText(t("读取实例失败：{0}", { "0": asMessage(ex) }));
    } finally {
      setLoadingVersion(false);
    }
  };

  // 打开时加载实例快照与版本列表（当前实例未就绪时兜底扫描游戏目录）
  useEffect(() => {
    if (!isOpen) return;
    let alive = true;
    const bootstrap = async () => {
      setLoadingVersion(true);
      setStatusText(t("正在读取实例列表…"));
      setPackStatus("");
      setProgressTotal(0);
      setProgressCurrent(0);
      try {
        const snap = await GetCurrentInstanceSnapshot();
        let gameDir = snap?.GameDirectory || "";

        if (!gameDir) gameDir = await GetGameDirectory();
        if (!gameDir) gameDir = await EnsureDefaultMinecraftDirectory();
        // 安装器模板状态决定 exe 导出是否可用（未就绪时在界面上提示）
        void GetSoloStubStatus()
          .then((status) => setStubFound(status?.Found ?? false))
          .catch(() => setStubFound(false));
        let list = snap?.VersionIds ?? [];

        if (list.length === 0 && gameDir) {
          list = (await GetInstalledVersionIds(gameDir)) ?? [];
        }
        if (!alive) return;
        setSnapshot(snap);
        setVersions(list);
        if (snap && list.length > 0) {
          setSelectedVersion(list[0]);
          await loadVersion(snap, list[0]);
        } else {
          setStatusText(t("未找到任何实例版本，请先在「下载」页安装游戏。"));
        }
      } catch (ex) {
        setStatusText(t("初始化失败：{0}", { "0": asMessage(ex) }));
      } finally {
        setLoadingVersion(false);
      }
    };

    void bootstrap();

    return () => {
      alive = false;
    };
    // loadVersion 只在打开弹窗时对首个版本调用一次
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [isOpen]);

  // 导出进度：后端阶段名即中文描述，直接展示
  useEffect(() => {
    if (!isOpen) return;
    EventsOn("modpack:exportProgress", (payload: ExportProgress) => {
      setProgressTotal(payload?.Total ?? 0);
      setProgressCurrent(payload?.Current ?? 0);
      setStatusText(
        (payload?.Total ?? 0) > 0
          ? `${payload.Phase}（${payload.Current} / ${payload.Total}）…`
          : `${payload.Phase ?? ""}…`,
      );
    });

    return () => {
      EventsOff("modpack:exportProgress");
    };
  }, [isOpen]);

  // ------------------------------------------------------------------
  // 打包（等价旧版 startPack）
  // ------------------------------------------------------------------

  const startExport = async () => {
    if (packing) return;
    if (!contentDir) {
      setStatusText(t("请先选择一个实例版本。"));

      return;
    }
    if (rows.every((row) => !row.included)) {
      setStatusText(t("请至少勾选一项要打包的内容。"));

      return;
    }
    const name = packName.trim();

    if (!name) {
      setStatusText(t("整合包名称不能为空。"));

      return;
    }
    const version = packVersion.trim() || "1.0.0";
    const isMrpack = format === 0;
    const isSolo = format === 3;
    const formatLabel = isSolo
      ? t("NekoSolo 安装包")
      : isMrpack
        ? t("Modrinth 整合包")
        : format === 2
          ? t("CurseForge 整合包")
          : t("MultiMC 整合包");
    const suggested = isSolo
      ? `${sanitizeFileName(name)}-${version}-Setup.exe`
      : `${sanitizeFileName(name)}.${isMrpack ? "mrpack" : "zip"}`;

    // 打包前经原生 SaveFile 对话框确认保存位置
    let outputPath = "";

    try {
      outputPath = await SaveFile(
        t("选择整合包保存位置"),
        suggested,
        formatLabel,
        isSolo ? "*.exe" : `*.${isMrpack ? "mrpack" : "zip"}`,
      );
    } catch {
      /* 用户取消 */
    }
    if (!outputPath) return;

    const baseVersion =
      details?.BaseGameVersion && details.BaseGameVersion !== "0"
        ? details.BaseGameVersion
        : selectedVersion;
    const includedPaths = rows
      .filter((row) => row.included)
      .map((row) => row.item.RelativePath);
    // 档案使用打包那一刻的排除快照：导出期间用户可能已改动勾选
    const packedExclusions = rows
      .filter((row) => !row.included)
      .map((row) => row.item.RelativePath);

    setPacking(true);
    setCancelRequested(false);
    setProgressTotal(0);
    setProgressCurrent(0);
    setPackStatus(t("准备打包…"));
    try {
      if (isSolo) {
        // NekoSolo：启动器 + 可选 Java + 整合包三合一安装包
        const soloOptions = new solo.SoloExportOptions({
          PackName: name,
          PackVersion: version,
          Author: author.trim(),
          UpdateLink: updateLink.trim(),
          Description: description.trim(),
          IconPngPath: iconPngPath,
          MinecraftVersion: baseVersion,
          LoaderName: details?.LoaderName || "",
          LoaderVersion: details?.LoaderVersion || "",
          IncludedPaths: includedPaths,
          ContentDirectory: contentDir,
          VersionDirectory:
            details?.VersionDirectory ||
            `${snapshot?.MinecraftDirectory || ""}/versions/${selectedVersion}`,
          VersionID: selectedVersion,
          BundleJava: bundleJava,
          SimpleMode: true,
          RemoteDistribution: remoteDist,
          PayloadURL: payloadUrl.trim(),
        });
        const result = await ExportSoloPack(soloOptions, outputPath);

        setPackStatus(
          result.PayloadPath
            ? t(
                "已保存：{0}（在线安装包）\n请把载荷 {1} 上传到下载地址后再分发安装包。",
                {
                  "0": result.OutputPath,
                  "1": result.PayloadPath,
                },
              )
            : t("已保存：{0}（版本文件 {1} 个、整合包内容 {2} 个）", {
                "0": result.OutputPath,
                "1": result.DeclaredFiles,
                "2": result.OverrideFiles,
              }),
        );
        setStatusText(
          result.Warnings && result.Warnings.length > 0
            ? t("打包完成，{0} 条提示：{1}", {
                "0": result.Warnings.length,
                "1": result.Warnings.slice(0, 3).join("；"),
              })
            : t("打包完成：{0}", { "0": result.OutputPath }),
        );
      } else {
        const options = new modpack.ModpackExportOptions({
          Format: format,
          PackName: name,
          PackVersion: version,
          Author: author.trim(),
          UpdateLink: updateLink.trim(),
          Description: description.trim(),
          IconPngPath: iconPngPath,
          MinecraftVersion: baseVersion,
          LoaderName: details?.LoaderName || "",
          LoaderVersion: details?.LoaderVersion || "",
          IncludedPaths: includedPaths,
          ResolveModrinthLinks: isMrpack && resolveLinks,
        });
        const result = await ExportModpack(options, contentDir, outputPath);

        setPackStatus(
          t("已保存：{0}（声明直链 {1} 个、overrides {2} 个）", {
            "0": result.OutputPath,
            "1": result.DeclaredFiles,
            "2": result.OverrideFiles,
          }),
        );
        setStatusText(
          result.Warnings && result.Warnings.length > 0
            ? t("打包完成，{0} 条提示：{1}", {
                "0": result.Warnings.length,
                "1": result.Warnings.slice(0, 3).join("；"),
              })
            : t("打包完成：{0}", { "0": result.OutputPath }),
        );
      }

      // 把勾选列表与元数据写回实例目录，供下次导出沿用
      void SaveExportProfile(
        profileDir || contentDir,
        new modpack.ModpackExportProfile({
          packName: name,
          packVersion: version,
          author: author.trim(),
          description: description.trim(),
          updateLink: updateLink.trim(),
          format: format,
          resolveModrinthLinks: isMrpack && resolveLinks,
          excludedPaths: packedExclusions,
        }),
      ).catch(() => undefined);

      // 跨实例记住个人偏好（格式/作者/链接/选项），下次打开对话框自动带出
      try {
        localStorage.setItem(
          EXPORT_PREFS_KEY,
          JSON.stringify({
            format,
            packVersion: version,
            author: author.trim(),
            updateLink: updateLink.trim(),
            resolveLinks,
            bundleJava,
          }),
        );
      } catch {
        /* 存储满或被禁用时静默失败 */
      }
    } catch (ex) {
      setPackStatus("");
      // 主动取消（后端 ctx 中断，错误消息为 Go 的 context canceled）提示「已取消」
      const message = asMessage(ex);

      if (cancelRequested || /context canceled/i.test(message)) {
        setStatusText(t("导出已取消。"));
      } else {
        setStatusText(t("打包失败：{0}", { "0": message }));
      }
    } finally {
      setPacking(false);
      setCancelRequested(false);
    }
  };

  // 打包中点「取消」：调后端 CancelExport 中断导出（后端逐文件检查 ctx），
  // 真正的完成/失败回落在 startExport 的 Promise 上。
  async function cancelPack() {
    if (!packing || cancelRequested) return;
    setCancelRequested(true);
    setStatusText(t("正在取消导出…"));
    try {
      await CancelExport();
    } catch {
      /* 无进行中的导出时后端报错，忽略 */
    }
  }

  const pickIcon = async () => {
    try {
      const path = await SelectFile(
        t("选择打包图标（png）"),
        t("图片文件"),
        "*.png",
      );

      if (path) setIconPngPath(path);
    } catch {
      /* 用户取消 */
    }
  };

  const settingLabel = (text: string) => (
    <span className="text-[13px] text-gray-600 dark:text-gray-300">{text}</span>
  );

  const inputClassNames = { inputWrapper: "h-9" };

  return (
    <Modal isOpen={isOpen} size="5xl" onClose={onClose} {...modalBehaviorProps}>
      <ModalContent className="h-[85vh] max-h-[85vh]">
        <ModalShell
          icon={<FolderZip20Regular />}
          subtitle={instanceSummary}
          title={soloOnly ? t("NekoSolo 安装包") : t("整合包制作")}
          onClose={onClose}
        >
          <div className="flex h-full min-h-0 flex-col">
            {/* 工具行：实例版本选择（本次制作的唯一新增）+ 重新读取内容 */}
            <div className="mb-3 flex flex-shrink-0 items-center gap-2">
              <Select
                aria-label={t("选择要打包的实例版本")}
                className="min-w-0 max-w-xs [&_*]:min-w-0"
                disabledKeys={packing || loadingVersion ? versions : []}
                items={versions.map((id) => ({ id }))}
                popoverProps={{ motionProps: popoverMotionProps }}
                selectedKeys={selectedVersion ? [selectedVersion] : []}
                size="sm"
                onSelectionChange={(keys) => {
                  const next = Array.from(keys)[0] as string;

                  if (next) void changeVersion(next);
                }}
              >
                {({ id }) => <SelectItem key={id}>{id}</SelectItem>}
              </Select>
              <Button
                isDisabled={packing || loadingVersion || !contentDir}
                isLoading={loadingVersion}
                size="sm"
                variant="flat"
                onPress={() => void reloadContent(contentDir)}
              >
                {t("重新读取内容")}
              </Button>
            </div>

            {!contentDir ? (
              <div className="flex min-h-0 flex-1 items-center justify-center text-sm text-gray-400">
                {loadingVersion ? (
                  <Progress isIndeterminate className="max-w-xs" size="sm" />
                ) : (
                  t("未找到任何实例版本，请先在「下载」页安装游戏")
                )}
              </div>
            ) : (
              <div className="grid min-h-0 flex-1 gap-4 md:grid-cols-[minmax(0,1fr)_minmax(240px,430px)]">
                {/* 左：打包内容（主工作区，主色底最深一层） */}
                <div className="grid min-h-0 grid-rows-[auto_auto_1fr] rounded-xl border border-primary/20 bg-primary/[0.04] p-3.5 dark:border-primary/25 dark:bg-primary/[0.08]">
                  <div className="flex items-center gap-1.5 px-0.5 pb-2">
                    <span className="min-w-0 flex-1 truncate text-[13px] font-semibold text-primary-600 dark:text-primary-300">
                      {contentSummary}
                    </span>
                    <Button
                      isDisabled={packing || !hasContent}
                      size="sm"
                      variant="light"
                      onPress={() => setVisible(true)}
                    >
                      {t("全选")}
                    </Button>
                    <Button
                      isDisabled={packing || !hasContent}
                      size="sm"
                      variant="light"
                      onPress={() => setVisible(false)}
                    >
                      {t("全不选")}
                    </Button>
                    <Button
                      isDisabled={packing || !hasContent}
                      size="sm"
                      title={t("恢复为上次打包时记住的勾选列表")}
                      variant="light"
                      onPress={restoreDefaults}
                    >
                      {t("恢复默认")}
                    </Button>
                  </div>

                  <Input
                    aria-label={t("搜索内容")}
                    classNames={{
                      inputWrapper:
                        "h-8 rounded-lg bg-primary/10 dark:bg-primary/15",
                      input: "text-[13px]",
                    }}
                    isDisabled={packing}
                    placeholder={t("搜索名称或路径…")}
                    size="sm"
                    value={filterQuery}
                    onValueChange={onFilterInput}
                  />

                  <div className="mt-2 min-h-0 overflow-y-auto pr-1">
                    {groups.map((group) => (
                      <div
                        key={group.category}
                        className="mb-1.5 rounded-lg bg-primary/[0.06] p-1.5 dark:bg-primary/10"
                      >
                        <div className="flex items-center gap-2 py-1">
                          {/* 三态主勾选框：点击在「全选 / 全不选」间切换（半选视为未全选） */}
                          <Checkbox
                            aria-label={t("全选 / 全不选 {0}", {
                              "0": group.category,
                            })}
                            classNames={{ wrapper: "before:hidden" }}
                            isDisabled={packing}
                            isIndeterminate={group.masterChecked === null}
                            isSelected={group.masterChecked === true}
                            size="sm"
                            onValueChange={() => toggleGroup(group)}
                          />
                          <button
                            className="flex min-w-0 flex-1 cursor-pointer items-center gap-1.5 text-left"
                            type="button"
                            onClick={() => toggleExpand(group)}
                          >
                            <span className="text-[13px] font-semibold text-gray-800 dark:text-gray-200">
                              {group.category}
                            </span>
                            <span className="rounded-md bg-primary/15 px-1.5 py-px text-[9px] text-primary-600 dark:text-primary-300">
                              {group.countDisplay}
                            </span>
                          </button>
                          <span className="mr-2 flex-shrink-0 text-[10px] text-gray-400 dark:text-gray-500">
                            {group.sizeDisplay}
                          </span>
                          <Button
                            isDisabled={packing}
                            size="sm"
                            variant="light"
                            onPress={() => setGroupChecked(group, true)}
                          >
                            {t("全选")}
                          </Button>
                          <Button
                            isDisabled={packing}
                            size="sm"
                            variant="light"
                            onPress={() => setGroupChecked(group, false)}
                          >
                            {t("全不选")}
                          </Button>
                        </div>

                        {group.isExpanded ? (
                          <div className="pb-1 pl-7">
                            {group.items.map((row) => (
                              <div
                                key={row.item.RelativePath}
                                className="my-[3px] flex items-center gap-2"
                                title={row.item.RelativePath}
                              >
                                <Checkbox
                                  aria-label={
                                    row.item.Name || row.item.RelativePath
                                  }
                                  classNames={{ wrapper: "before:hidden" }}
                                  isDisabled={packing}
                                  isSelected={row.included}
                                  size="sm"
                                  onValueChange={() =>
                                    toggleRow(row.item.RelativePath)
                                  }
                                />
                                <div className="flex min-w-0 flex-1 flex-col gap-px">
                                  <span className="truncate text-xs text-gray-800 dark:text-gray-200">
                                    {row.item.Name}
                                  </span>
                                  <span className="truncate text-[9px] text-gray-400 dark:text-gray-500">
                                    {row.item.RelativePath}
                                  </span>
                                </div>
                                <span className="min-w-[52px] flex-none text-right text-[9px] text-gray-400 dark:text-gray-500">
                                  {row.item.IsDirectory
                                    ? t("目录")
                                    : formatSize(row.item.SizeBytes)}
                                </span>
                              </div>
                            ))}
                          </div>
                        ) : null}
                      </div>
                    ))}
                    {!hasContent ? (
                      <div className="py-6 text-center text-[13px] text-gray-400">
                        {t("没有可打包的内容")}
                      </div>
                    ) : null}
                  </div>
                </div>

                {/* 右：打包设置 */}
                {/* 右：打包设置（表单区，主色底比左侧浅一层） */}
                <div className="min-h-0 overflow-y-auto rounded-xl border border-primary/20 bg-primary/[0.03] p-[18px] dark:border-primary/25 dark:bg-primary/[0.06]">
                  <div className="flex flex-col gap-3">
                    <div className="flex flex-col gap-1">
                      {settingLabel(t("打包格式"))}
                      {soloOnly ? (
                        <div className="flex h-9 items-center rounded-lg bg-default-100/80 px-2.5 text-[13px] text-gray-600 dark:bg-default/20 dark:text-gray-300">
                          NekoSolo（.exe）
                        </div>
                      ) : (
                        <RadioGroup
                          aria-label={t("打包格式")}
                          classNames={{ wrapper: "gap-4" }}
                          isDisabled={packing}
                          orientation="horizontal"
                          size="sm"
                          value={String(format)}
                          onValueChange={(value) => setFormat(Number(value))}
                        >
                          <Radio value="0">Modrinth（.mrpack）</Radio>
                          <Radio value="1">MultiMC（.zip）</Radio>
                          <Radio value="2">CurseForge（.zip）</Radio>
                        </RadioGroup>
                      )}
                    </div>

                    {format === 2 ? (
                      <div className="break-words rounded-lg bg-default-100/80 p-2 px-2.5 text-[11px] text-gray-500 dark:text-gray-400">
                        {t(
                          "CurseForge 导出需要 API Key（设置 → 下载页配置）：能识别归属的模组会声明为 CurseForge 文件由导入方下载，识别不了的（Modrinth 独占等）直接打包进整合包。",
                        )}
                      </div>
                    ) : null}

                    {format === 3 ? (
                      <>
                        <div className="break-words rounded-lg bg-default-100/80 p-2 px-2.5 text-[11px] text-gray-500 dark:text-gray-400">
                          {t(
                            "NekoSolo 安装包（仅 Windows）：把启动器、整合包与可选的 Java 打进单个 exe，玩家双击即玩；安装的启动器默认开启 NekoLauncher-S 简洁模式。Minecraft 客户端本体不随包分发，玩家首次启动联网补全。",
                          )}
                        </div>
                        <div className="break-words rounded-lg bg-warning-50 p-2 px-2.5 text-[11px] text-warning-600 dark:bg-warning-50/10 dark:text-warning-400">
                          {t(
                            "打包的模组、资源包等第三方内容会随安装包一起分发：请确认你有权再分发它们（CurseForge 上标记为「不允许第三方分发」的模组尤其需要注意）。",
                          )}
                        </div>
                        {!stubFound ? (
                          <div className="flex flex-col gap-1.5 break-words rounded-lg bg-danger-50 p-2 px-2.5 text-[11px] text-danger-500 dark:bg-danger-50/10">
                            <span>
                              {t(
                                "未找到 NekoSolo 安装器模板（NekoSolo/build/NekoSolo.Installer.exe），导出会失败。可以从启动器的 Release 自动下载（约 10 MB）。",
                              )}
                            </span>
                            {stubDownloading ? (
                              <span className="flex items-center gap-1.5">
                                <Spinner size="sm" />
                                {t("正在下载安装器模板…")}
                              </span>
                            ) : (
                              <Button
                                className="self-start"
                                color="primary"
                                size="sm"
                                variant="flat"
                                onPress={() => void downloadStub()}
                              >
                                {t("自动下载安装器模板")}
                              </Button>
                            )}
                            {stubMessage ? (
                              <span className="break-words">{stubMessage}</span>
                            ) : null}
                          </div>
                        ) : null}
                        <Checkbox
                          classNames={{ wrapper: "before:hidden" }}
                          isDisabled={packing}
                          isSelected={bundleJava}
                          size="sm"
                          onValueChange={setBundleJava}
                        >
                          <span className="text-[13px] text-gray-600 dark:text-gray-300">
                            {t(
                              "捆绑当前 Java 运行时（推荐，玩家无需自备 Java）",
                            )}
                          </span>
                        </Checkbox>
                        <Checkbox
                          classNames={{ wrapper: "before:hidden" }}
                          isDisabled={packing}
                          isSelected={remoteDist}
                          size="sm"
                          onValueChange={setRemoteDist}
                        >
                          <span className="text-[13px] text-gray-600 dark:text-gray-300">
                            {t(
                              "在线安装包（小体积，安装时从下方地址动态下载内容）",
                            )}
                          </span>
                        </Checkbox>
                        {remoteDist ? (
                          <>
                            <Input
                              aria-label={t("载荷下载地址")}
                              classNames={{
                                inputWrapper:
                                  "bg-default-100/80 data-[hover=true]:bg-default-200",
                              }}
                              isDisabled={packing}
                              placeholder="https://github.com/用户名/仓库/releases/download/标签/payload.zip"
                              size="sm"
                              value={payloadUrl}
                              onValueChange={setPayloadUrl}
                            />
                            <div className="break-words rounded-lg bg-default-100/80 p-2 px-2.5 text-[11px] text-gray-500 dark:text-gray-400">
                              {t(
                                "导出会同时生成一个 payload.zip：把它作为资产上传到上面的 GitHub Release，玩家安装时即从此地址下载并校验。推荐先建好 Release 再导出。",
                              )}
                            </div>
                          </>
                        ) : null}
                      </>
                    ) : null}

                    <div className="flex flex-col gap-1">
                      {settingLabel(t("整合包名称"))}
                      <Input
                        aria-label={t("整合包名称")}
                        classNames={inputClassNames}
                        isDisabled={packing}
                        placeholder={t("例如：我的究极生存包")}
                        size="sm"
                        value={packName}
                        onValueChange={setPackName}
                      />
                    </div>

                    <div className="grid grid-cols-[1fr_10px_1fr]">
                      <div className="flex flex-col gap-1">
                        {settingLabel(t("版本号"))}
                        <Input
                          aria-label={t("版本号")}
                          classNames={inputClassNames}
                          isDisabled={packing}
                          size="sm"
                          value={packVersion}
                          onValueChange={setPackVersion}
                        />
                      </div>
                      <div className="col-start-3 flex flex-col gap-1">
                        {settingLabel(t("作者"))}
                        <Input
                          aria-label={t("作者")}
                          classNames={inputClassNames}
                          isDisabled={packing}
                          placeholder={t("你的名字")}
                          size="sm"
                          value={author}
                          onValueChange={setAuthor}
                        />
                      </div>
                    </div>

                    <div className="flex flex-col gap-1">
                      {settingLabel(t("更新链接（主页 / 发布页）"))}
                      <Input
                        aria-label={t("更新链接")}
                        classNames={inputClassNames}
                        isDisabled={packing}
                        placeholder="https://…"
                        size="sm"
                        value={updateLink}
                        onValueChange={setUpdateLink}
                      />
                    </div>

                    <div className="flex flex-col gap-1">
                      {settingLabel(t("整合包描述"))}
                      <Textarea
                        aria-label={t("整合包描述")}
                        classNames={{ input: "text-[13px]" }}
                        isDisabled={packing}
                        maxRows={4}
                        minRows={3}
                        placeholder={t("介绍一下你的整合包…")}
                        size="sm"
                        value={description}
                        onValueChange={setDescription}
                      />
                    </div>

                    <div className="flex flex-col gap-1">
                      {settingLabel(t("图标（png）"))}
                      <div className="flex items-center gap-2">
                        <Button
                          isDisabled={packing}
                          size="sm"
                          variant="flat"
                          onPress={() => void pickIcon()}
                        >
                          {t("选择图标…")}
                        </Button>
                        {iconPngPath ? (
                          <span
                            className="min-w-0 flex-1 truncate text-[11px] text-gray-800 dark:text-gray-200"
                            title={iconPngPath}
                          >
                            {iconPngPath}
                          </span>
                        ) : null}
                        {iconPngPath ? (
                          <Button
                            isDisabled={packing}
                            size="sm"
                            variant="light"
                            onPress={() => setIconPngPath("")}
                          >
                            {t("清除")}
                          </Button>
                        ) : null}
                      </div>
                    </div>

                    {format === 0 ? (
                      <Checkbox
                        classNames={{ wrapper: "before:hidden" }}
                        isDisabled={packing}
                        isSelected={resolveLinks}
                        size="sm"
                        onValueChange={setResolveLinks}
                      >
                        <span className="text-[13px] text-gray-600 dark:text-gray-300">
                          {t("优先通过 Modrinth 直链收录模组")}
                        </span>
                      </Checkbox>
                    ) : null}

                    <div className="break-words rounded-lg border border-primary/25 bg-primary/10 p-2 px-2.5 text-[11px] text-primary-600 dark:border-primary/30 dark:bg-primary/15 dark:text-primary-300">
                      {targetInfo}
                    </div>

                    {/* 打包进度：确定态用数值进度，不确定态走 indeterminate */}
                    {packing && progressTotal > 0 ? (
                      <Progress
                        aria-label={t("打包进度")}
                        className="h-1"
                        size="sm"
                        value={progressPercent}
                      />
                    ) : packing ? (
                      <Progress
                        isIndeterminate
                        aria-label={t("打包进度")}
                        size="sm"
                      />
                    ) : null}

                    {packStatus ? (
                      <div className="break-words text-[11px] text-primary-600 dark:text-primary-300">
                        {packStatus}
                      </div>
                    ) : null}

                    <div className="flex gap-2.5">
                      <Button
                        color="primary"
                        isDisabled={packing}
                        isLoading={packing}
                        onPress={() => void startExport()}
                      >
                        {t("开始打包并保存…")}
                      </Button>
                      {packing ? (
                        <Button variant="flat" onPress={cancelPack}>
                          {t("取消")}
                        </Button>
                      ) : null}
                    </div>
                  </div>
                </div>
              </div>
            )}

            <div className="mt-3 min-h-[1em] flex-shrink-0 break-words text-[11px] text-gray-500 dark:text-gray-400">
              {statusText}
            </div>
          </div>
        </ModalShell>
      </ModalContent>
    </Modal>
  );
};

export default ModpackExportDialog;
