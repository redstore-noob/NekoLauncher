/*
 * 资源搜索弹层（X-3）：Modrinth / CurseForge 双数据源检索 → 看版本 → 装进当前实例。
 *
 * 与下载页原有的 Modrinth 标签页的区别（也是这个弹层存在的理由）：
 * - 搜索/版本查询全部走 Go 绑定（DownloadAPI.SearchResources / ListResourceVersions），
 *   不再由 WebView 直接 fetch api.modrinth.com：官方域名在国内经常超时，
 *   后端会先试官方、再自动回退国内镜像，并把"已走镜像"如实告诉界面；
 * - 显示串（下载量、类型、版本摘要、文件大小）全部来自 Go 侧已被单测锁定的
 *   展示语义，前端不再自己写一套格式化；
 * - 支持切换 CurseForge：未配置 API Key 时返回的是可读引导（needsApiKey），
 *   界面据此提示去设置里填写，而不是弹一个错误。
 *
 * 下载落点由"目标实例 + 项目类型"决定：mod → mods、光影 → shaderpacks、
 * 材质包 → resourcepacks；整合包要走独立的安装流程，这里只做引导。
 */
import type { instance, models } from "../../../wailsjs/go/models";

import React, { useEffect, useMemo, useRef, useState } from "react";
import {
  Button,
  Input,
  Modal,
  ModalContent,
  Progress,
  Select,
  SelectItem,
  Spinner,
} from "@heroui/react";
import {
  ArrowDownload20Regular,
  ArrowLeft20Regular,
  Box20Regular,
  CheckmarkCircle20Regular,
  CloudArrowDown20Regular,
  Dismiss20Regular,
  FolderOpen20Regular,
  Image20Regular,
  Open20Regular,
  PuzzleCube20Regular,
  Search20Regular,
  Sparkle20Regular,
  Warning20Regular,
} from "@fluentui/react-icons";

import { ModalShell, modalBehaviorProps } from "../modal-shell";
import { popoverMotionProps } from "../../lib/motion";
import { asArray } from "../../lib/guards";
import { subDirectoryForProjectType } from "../../lib/resourceSearch";
import {
  DownloadResourceVersion,
  GetResourceSources,
  ListResourceVersions,
  ResolveContentDirectoryForInstance,
  SaveCurseForgeAPIKey,
  SearchResources,
} from "../../../wailsjs/go/bindings/DownloadAPI";
import { GetCurrentInstanceSnapshot } from "../../../wailsjs/go/bindings/InstanceAPI";
import {
  OpenInExplorer,
  OpenPath,
} from "../../../wailsjs/go/bindings/SystemAPI";
import { EventsOn } from "../../../wailsjs/runtime/runtime";
import { t } from "../../i18n";

interface Props {
  open: boolean;
  onClose: () => void;
  /** 打开时预填的项目类型（从资源标签页进入时带上当前标签），缺省保持上次选择 */
  initialType?: string;
  /** 打开时预填的关键词（从资源标签页进入时带上页内搜索词），undefined 表示保持原样 */
  initialQuery?: string;
}

/** 项目类型（Go 侧统一取值）→ 图标；与 models 的 TypeIcon 语义一致，仅用于未返回图标时兜底 */
const TYPE_ICONS: Record<string, React.ReactNode> = {
  mod: <PuzzleCube20Regular />,
  modpack: <Box20Regular />,
  shader: <Sparkle20Regular />,
  resourcepack: <Image20Regular />,
};

/** 项目类型 → 中文名（Go 侧 typeDisplay 缺失时的兜底，正常不会用到） */
const TYPE_LABELS: Record<string, string> = {
  mod: "Mod",
  modpack: "整合包",
  shader: "光影包",
  resourcepack: "材质包",
};

const TYPE_OPTIONS = ["mod", "modpack", "shader", "resourcepack"];

// 加载器下拉（"不筛选"用空串，Go 侧会把空串/minecraft/any 都当成不过滤）
const LOADER_OPTIONS = ["", "fabric", "forge", "neoforge", "quilt"];

function loaderLabel(loader: string): string {
  if (!loader) return t("全部加载器");
  if (loader === "neoforge") return "NeoForge";

  return loader.charAt(0).toUpperCase() + loader.slice(1);
}

const ResourceSearchDialog: React.FC<Props> = ({
  open,
  onClose,
  initialType,
  initialQuery,
}) => {
  // 数据源
  const [sources, setSources] = useState<models.ResourceSourceInfo[]>([]);
  const [source, setSource] = useState("modrinth");
  const [projectType, setProjectType] = useState("mod");
  const [query, setQuery] = useState("");
  const [gameVersion, setGameVersion] = useState("");
  const [loader, setLoader] = useState("");

  // 结果
  const [hits, setHits] = useState<models.ResourceHit[]>([]);
  const [total, setTotal] = useState(0);
  const [searching, setSearching] = useState(false);
  const [searchError, setSearchError] = useState("");
  const [message, setMessage] = useState("");
  const [needsApiKey, setNeedsApiKey] = useState(false);
  const [usedMirror, setUsedMirror] = useState(false);

  // 项目详情（版本列表）
  const [project, setProject] = useState<models.ResourceHit | null>(null);
  const [versions, setVersions] = useState<models.ResourceVersion[]>([]);
  const [matchedCount, setMatchedCount] = useState(0);
  const [versionLoading, setVersionLoading] = useState(false);
  const [versionError, setVersionError] = useState("");
  const [versionMessage, setVersionMessage] = useState("");

  // 目标实例与下载状态
  const [snapshot, setSnapshot] =
    useState<instance.GameInstanceSnapshot | null>(null);
  const [targetId, setTargetId] = useState("");
  const [downloadVersionId, setDownloadVersionId] = useState("");
  const [downloading, setDownloading] = useState(false);
  const [progressPercent, setProgressPercent] = useState(0);
  const [statusText, setStatusText] = useState("");
  const [savedPath, setSavedPath] = useState("");
  // CurseForge Key 输入框（未配置 Key 时在引导卡片里就地填写）
  const [apiKeyDraft, setApiKeyDraft] = useState("");

  // 防抖与竞态：搜索是"打字即发"，慢的旧请求可能后于新请求返回
  const searchSeq = useRef(0);
  const versionSeq = useRef(0);
  const searchTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const downloadingRef = useRef(false);

  downloadingRef.current = downloading;

  const activeSource = useMemo(
    () => sources.find((item) => item.id === source) ?? null,
    [sources, source],
  );
  const supportsModpack = projectType === "modpack";

  /** 搜索（source/projectType/query/gameVersion/loader 任一变化都会重查） */
  async function runSearch(
    nextSource: string,
    nextType: string,
    nextQuery: string,
    nextGameVersion: string,
    nextLoader: string,
  ) {
    const seq = ++searchSeq.current;

    setSearching(true);
    setSearchError("");
    setMessage("");
    setNeedsApiKey(false);
    setSavedPath("");
    try {
      const request: models.ResourceSearchRequest = {
        source: nextSource,
        projectType: nextType,
        query: nextQuery,
        gameVersion: nextGameVersion,
        loader: nextLoader,
        loaders: [],
        limit: 40,
      };
      const result = await SearchResources(request);

      if (seq !== searchSeq.current) return;
      setHits(asArray<models.ResourceHit>(result?.hits));
      setTotal(result?.total ?? 0);
      setMessage(result?.message ?? "");
      setNeedsApiKey(!!result?.needsApiKey);
      setUsedMirror(!!result?.usedMirror);
    } catch (ex) {
      if (seq !== searchSeq.current) return;
      setHits([]);
      setTotal(0);
      setUsedMirror(false);
      setSearchError((ex as Error)?.message ?? String(ex));
    } finally {
      if (seq === searchSeq.current) setSearching(false);
    }
  }

  /** 保存 CurseForge Key（就地填写，成功后刷新数据源状态并重查） */
  async function saveKey() {
    const key = apiKeyDraft.trim();

    if (!key) {
      setMessage(t("请先粘贴 CurseForge API Key。"));

      return;
    }
    try {
      const ok = await SaveCurseForgeAPIKey(key);

      if (!ok) {
        setMessage(t("保存失败：配置文件不可写。"));

        return;
      }
      setApiKeyDraft("");
      setSources(asArray(await GetResourceSources()));
      setMessage("");
      setNeedsApiKey(false);
      void runSearch(source, projectType, query.trim(), gameVersion, loader);
    } catch (ex) {
      setMessage(t("保存失败：{0}", { "0": (ex as Error)?.message ?? ex }));
    }
  }

  /** 拉某个项目的版本（后端已标记哪些匹配当前实例） */
  async function openProject(hit: models.ResourceHit) {
    const seq = ++versionSeq.current;

    setProject(hit);
    setVersions([]);
    setMatchedCount(0);
    setVersionError("");
    setVersionMessage("");
    setDownloadVersionId("");
    setStatusText("");
    setSavedPath("");
    setVersionLoading(true);
    try {
      const request: models.ResourceVersionRequest = {
        source: hit.source,
        projectId: hit.projectId,
        gameVersion,
        loader,
      };
      const result = await ListResourceVersions(request);

      if (seq !== versionSeq.current) return;
      const list = asArray<models.ResourceVersion>(result?.versions);

      setVersions(list);
      setMatchedCount(result?.matchedCount ?? 0);
      setVersionMessage(result?.message ?? "");
      // 默认选中第一个匹配当前实例的版本（列表已按"匹配优先"排序）
      setDownloadVersionId(list.length > 0 ? list[0].versionId : "");
    } catch (ex) {
      if (seq !== versionSeq.current) return;
      setVersionError((ex as Error)?.message ?? String(ex));
    } finally {
      if (seq === versionSeq.current) setVersionLoading(false);
    }
  }

  /** 下载选中版本到目标实例的内容目录 */
  async function downloadSelected() {
    const version = versions.find(
      (item) => item.versionId === downloadVersionId,
    );

    if (!project || !version || downloading) return;
    setStatusText("");
    setSavedPath("");
    try {
      const snap = snapshot;

      if (!snap?.MinecraftDirectory) {
        setStatusText(t("无法定位游戏目录。"));

        return;
      }
      if (!targetId) {
        setStatusText(t("请选择要安装到的实例。"));

        return;
      }
      const contentDirectory =
        (await ResolveContentDirectoryForInstance(
          snap.MinecraftDirectory,
          snap.SourcePath,
          targetId,
        )) || snap.MinecraftDirectory;

      setDownloading(true);
      setProgressPercent(0);
      setStatusText(
        t("正在下载 {0}…", { "0": version.fileName || version.displayName }),
      );

      const downloadRequest: models.ResourceDownloadRequest = {
        source: project.source,
        projectId: project.projectId,
        versionId: version.versionId,
        contentDirectory,
        subDirectory: subDirectoryForProjectType(project.projectType),
      };
      const result = await DownloadResourceVersion(downloadRequest);

      setProgressPercent(100);
      setSavedPath(result?.savedPath ?? "");
      setStatusText(
        t("已安装 {0}", { "0": result?.fileName || version.fileName }),
      );
    } catch (ex) {
      setStatusText(t("下载失败：{0}", { "0": (ex as Error)?.message ?? ex }));
    } finally {
      setDownloading(false);
    }
  }

  // 打开时：拉数据源清单 + 当前实例快照，并做一次默认搜索
  useEffect(() => {
    if (!open) return;
    let cancelled = false;

    setStatusText("");
    setSavedPath("");
    setProgressPercent(0);
    setProject(null);
    setVersions([]);
    setVersionError("");

    void (async () => {
      try {
        const list = await GetResourceSources();

        if (cancelled) return;
        const items = asArray<models.ResourceSourceInfo>(list);

        setSources(items);
        // 数据源可用性由后端判定：CurseForge 未配置 Key 时仍可选中（会看到引导）
        setSource((current) =>
          items.some((item) => item.id === current)
            ? current
            : (items[0]?.id ?? "modrinth"),
        );
      } catch (ex) {
        console.error(t("读取资源站信息失败"), ex);
      }

      try {
        const snap = await GetCurrentInstanceSnapshot();

        if (cancelled) return;
        setSnapshot(snap);
        const ids = asArray<string>(snap?.VersionIds);

        setTargetId(
          (current) => current || snap?.SelectedVersionId || ids[0] || "",
        );
      } catch (ex) {
        console.error(t("读取实例快照失败"), ex);
      }
    })();

    // 预填：从资源标签页进入时带上该标签的项目类型与页内搜索词。
    // 注意 runSearch 直接用预填值——本 effect 闭包里的 state 还是上次的旧值
    const presetType =
      initialType && TYPE_OPTIONS.includes(initialType)
        ? initialType
        : projectType;
    const presetQuery = initialQuery !== undefined ? initialQuery : query;

    if (presetType !== projectType) setProjectType(presetType);
    if (initialQuery !== undefined) setQuery(presetQuery);

    // 打开时按"预填条件（无则用上次的搜索条件）"重查一次：输入框里还留着
    // 上次的关键词，若这里用空词搜索，列表与输入框就对不上了
    void runSearch(source, presetType, presetQuery.trim(), gameVersion, loader);

    return () => {
      cancelled = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  // 关键词防抖：只重查列表，不动已打开的项目详情
  const firstQueryRender = useRef(true);

  useEffect(() => {
    if (!open) return;
    if (firstQueryRender.current) {
      firstQueryRender.current = false;

      return;
    }
    if (searchTimer.current) clearTimeout(searchTimer.current);
    searchTimer.current = setTimeout(() => {
      void runSearch(source, projectType, query.trim(), gameVersion, loader);
    }, 300);

    return () => {
      if (searchTimer.current) clearTimeout(searchTimer.current);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [query]);

  useEffect(() => {
    firstQueryRender.current = true;
  }, [open]);

  // 下载进度（download:contentProgress 与其它内容下载共用同一事件）
  useEffect(() => {
    if (!open) return;
    const off = EventsOn(
      "download:contentProgress",
      (event: { downloaded?: number; total?: number } | null) => {
        if (!event || !downloadingRef.current) return;
        setProgressPercent(
          event.total && event.total > 0
            ? Math.min(100, ((event.downloaded ?? 0) * 100) / event.total)
            : 0,
        );
      },
    );

    return () => off();
  }, [open]);

  const sourceOptions = sources.length
    ? sources
    : ([
        { id: "modrinth", name: "Modrinth" },
      ] as unknown as models.ResourceSourceInfo[]);

  const targetOptions = asArray<string>(snapshot?.VersionIds).map((id) => ({
    value: id,
    label: id,
  }));

  const canDownload =
    !!project &&
    !!downloadVersionId &&
    !downloading &&
    !supportsModpack &&
    versions.some(
      (item) => item.versionId === downloadVersionId && item.downloadAllowed,
    );

  return (
    <Modal
      isOpen={open}
      onClose={onClose}
      {...modalBehaviorProps}
      // 弹层固定为窗口宽高的二分之一
      classNames={{
        ...modalBehaviorProps.classNames,
        base: `${modalBehaviorProps.classNames?.base ?? ""} h-[50vh]! w-[50vw]! max-w-none`,
      }}
      scrollBehavior="inside"
    >
      <ModalContent className="h-full overflow-y-auto">
        <ModalShell
          subtitle={t("搜索并以镜像回退下载资源，支持 Modrinth 与 CurseForge")}
          title={t("资源搜索")}
          onClose={onClose}
        >
          {/* 数据源 + 类型 + 关键词 */}
          <div className="flex flex-col gap-2">
            <div className="flex items-center gap-2">
              {sourceOptions.map((item) => (
                <Button
                  key={item.id}
                  className="flex-none"
                  color={source === item.id ? "primary" : "default"}
                  radius="full"
                  size="sm"
                  variant={source === item.id ? "solid" : "flat"}
                  onPress={() => {
                    setSource(item.id);
                    setProject(null);
                    void runSearch(
                      item.id,
                      projectType,
                      query.trim(),
                      gameVersion,
                      loader,
                    );
                  }}
                >
                  {item.name}
                  {item.requiresApiKey && !item.apiKeyConfigured ? (
                    <span className="text-[10px] opacity-80">
                      {t("（未配置 Key）")}
                    </span>
                  ) : null}
                </Button>
              ))}
              {activeSource?.mirrorHost ? (
                <span className="truncate text-[11px] text-gray-400">
                  {t("接口 {0}，失败自动回退 {1}", {
                    "0": activeSource.apiHost,
                    "1": activeSource.mirrorHost,
                  })}
                </span>
              ) : null}
            </div>

            <div className="flex items-center gap-2">
              <Input
                aria-label={t("搜索资源")}
                className="min-w-0 flex-1"
                placeholder={t("搜索资源（留空浏览热门）")}
                radius="full"
                size="sm"
                startContent={<Search20Regular className="text-gray-400" />}
                value={query}
                onValueChange={setQuery}
              />
              <Select
                aria-label={t("资源类型")}
                className="w-32 flex-none"
                items={TYPE_OPTIONS.map((type) => ({
                  key: type,
                  label: t(TYPE_LABELS[type]),
                }))}
                popoverProps={{ motionProps: popoverMotionProps }}
                selectedKeys={[projectType]}
                size="sm"
                onSelectionChange={(keys) => {
                  const next = String(Array.from(keys)[0] ?? "mod");

                  setProjectType(next);
                  setProject(null);
                  void runSearch(
                    source,
                    next,
                    query.trim(),
                    gameVersion,
                    loader,
                  );
                }}
              >
                {(item) => <SelectItem key={item.key}>{item.label}</SelectItem>}
              </Select>
            </div>

            <div className="flex items-center gap-2">
              <Input
                aria-label={t("游戏版本过滤")}
                className="w-40 flex-none"
                placeholder={t("游戏版本（可选）")}
                radius="full"
                size="sm"
                value={gameVersion}
                onBlur={() => {
                  setProject(null);
                  void runSearch(
                    source,
                    projectType,
                    query.trim(),
                    gameVersion,
                    loader,
                  );
                }}
                onValueChange={setGameVersion}
              />
              <Select
                aria-label={t("加载器过滤")}
                className="w-40 flex-none"
                items={LOADER_OPTIONS.map((value) => ({
                  key: value || "__all__",
                  label: loaderLabel(value),
                }))}
                popoverProps={{ motionProps: popoverMotionProps }}
                selectedKeys={[loader || "__all__"]}
                size="sm"
                onSelectionChange={(keys) => {
                  const raw = String(Array.from(keys)[0] ?? "__all__");
                  const next = raw === "__all__" ? "" : raw;

                  setLoader(next);
                  setProject(null);
                  void runSearch(
                    source,
                    projectType,
                    query.trim(),
                    gameVersion,
                    next,
                  );
                }}
              >
                {(item) => <SelectItem key={item.key}>{item.label}</SelectItem>}
              </Select>
              {usedMirror ? (
                <span className="truncate text-[11px] text-warning-500">
                  {t("已自动切换国内镜像")}
                </span>
              ) : null}
            </div>
          </div>

          {/* 未配置 Key：引导而不是报错 */}
          {needsApiKey ? (
            <div className="flex flex-col gap-2 rounded-2xl border border-warning-200 nya-panel-inner px-3.5 py-3">
              <span className="flex items-center gap-2 text-xs text-warning-600 dark:text-warning-400">
                <Warning20Regular />
                {message || t("需要先在设置里填写 CurseForge API Key。")}
              </span>
              {activeSource?.apiKeyApplyUrl ? (
                <div className="flex items-center gap-2">
                  <Input
                    aria-label={t("CurseForge API Key")}
                    className="min-w-0 flex-1"
                    placeholder={t("粘贴 CurseForge API Key")}
                    radius="full"
                    size="sm"
                    value={apiKeyDraft}
                    onValueChange={setApiKeyDraft}
                  />
                  <Button
                    className="flex-none"
                    radius="full"
                    size="sm"
                    variant="flat"
                    onPress={() => void saveKey()}
                  >
                    {t("保存 Key")}
                  </Button>
                </div>
              ) : null}
              {activeSource?.apiKeyApplyUrl ? (
                <span className="break-all text-[11px] text-gray-400">
                  {t("申请地址：{0}", { "0": activeSource.apiKeyApplyUrl })}
                </span>
              ) : null}
            </div>
          ) : null}

          {searchError ? (
            <div className="flex flex-col gap-2 rounded-2xl border nya-panel-inner px-3.5 py-3">
              <span className="flex items-center gap-2 text-xs text-danger">
                <Warning20Regular />
                {t("搜索失败：{0}", { "0": searchError })}
              </span>
              <Button
                className="self-start"
                radius="full"
                size="sm"
                variant="flat"
                onPress={() =>
                  void runSearch(
                    source,
                    projectType,
                    query.trim(),
                    gameVersion,
                    loader,
                  )
                }
              >
                {t("重试")}
              </Button>
            </div>
          ) : null}

          {/* 结果列表 / 项目详情 */}
          <div className="nya-scroll flex max-h-[46vh] min-h-0 flex-col gap-2 overflow-y-auto pr-1">
            {project ? (
              <>
                <div className="flex items-center gap-2">
                  <Button
                    isIconOnly
                    aria-label={t("返回搜索结果")}
                    radius="full"
                    size="sm"
                    variant="flat"
                    onPress={() => setProject(null)}
                  >
                    <ArrowLeft20Regular />
                  </Button>
                  <span className="min-w-0 flex-1 truncate text-sm font-semibold">
                    {project.title}
                  </span>
                  {project.pageUrl ? (
                    <Button
                      className="flex-none"
                      radius="full"
                      size="sm"
                      startContent={<Open20Regular />}
                      variant="flat"
                      onPress={() => void OpenPath(project.pageUrl)}
                    >
                      {t("打开项目主页")}
                    </Button>
                  ) : null}
                </div>

                {versionLoading ? (
                  <div className="my-8 flex items-center justify-center gap-2 text-xs text-gray-400">
                    <Spinner size="sm" /> {t("正在加载版本…")}
                  </div>
                ) : versionError ? (
                  <span className="text-xs text-danger">
                    {t("加载版本失败：{0}", { "0": versionError })}
                  </span>
                ) : versions.length === 0 ? (
                  <span className="text-xs text-gray-400">
                    {versionMessage || t("该项目没有可用版本")}
                  </span>
                ) : (
                  <div className="flex flex-col gap-2">
                    <span className="text-[11px] text-gray-400">
                      {t("共 {0} 个版本，其中 {1} 个匹配当前实例", {
                        "0": versions.length,
                        "1": matchedCount,
                      })}
                    </span>
                    {versions.map((version) => (
                      <button
                        key={version.versionId}
                        className={`flex cursor-pointer items-center gap-3 rounded-2xl border px-3.5 py-3 text-left backdrop-blur-md transition-all ${
                          downloadVersionId === version.versionId
                            ? "border-primary/40 bg-primary/[0.08]"
                            : "border-transparent nya-panel hover:border-primary/30"
                        }`}
                        onClick={() => setDownloadVersionId(version.versionId)}
                      >
                        <div className="flex min-w-0 flex-1 flex-col gap-0.5">
                          <span className="truncate text-sm font-semibold">
                            {version.displayName || version.versionNumber}
                          </span>
                          <span className="truncate text-[11px] text-gray-400">
                            {version.summary}
                          </span>
                          {version.matchNote ? (
                            <span className="truncate text-[11px] text-warning-500">
                              {version.matchNote}
                            </span>
                          ) : null}
                        </div>
                        {version.matchesInstance ? (
                          <span className="flex flex-none items-center gap-1 text-[11px] text-success-500">
                            <CheckmarkCircle20Regular />
                            {t("匹配当前实例")}
                          </span>
                        ) : null}
                        {!version.downloadAllowed ? (
                          <span className="flex-none text-[11px] text-gray-400">
                            {t("不可下载")}
                          </span>
                        ) : null}
                      </button>
                    ))}
                  </div>
                )}
              </>
            ) : searching ? (
              <div className="my-10 flex items-center justify-center gap-2 text-xs text-gray-400">
                <Spinner size="sm" /> {t("正在搜索…")}
              </div>
            ) : hits.length === 0 ? (
              <div className="my-10 flex flex-col items-center gap-3 text-center text-gray-400">
                <div className="flex size-16 items-center justify-center rounded-3xl bg-gradient-to-br from-default-200 to-default-100 shadow-inner dark:from-gray-800 dark:to-gray-800/50">
                  <Search20Regular className="h-8 w-8" />
                </div>
                <span className="text-[15px] font-semibold text-gray-500 dark:text-gray-400">
                  {t("没有找到匹配的资源")}
                </span>
              </div>
            ) : (
              <>
                {hits.map((hit) => (
                  <button
                    key={`${hit.source}:${hit.projectId}`}
                    className="group flex cursor-pointer items-center gap-3 rounded-2xl border border-transparent nya-panel px-3.5 py-3 text-left backdrop-blur-md transition-all hover:translate-x-0.5 hover:border-primary/30 hover:bg-primary/[0.06]"
                    onClick={() => void openProject(hit)}
                  >
                    {hit.iconUrl ? (
                      <img
                        alt=""
                        className="size-11 flex-none rounded-xl object-cover shadow-sm"
                        src={hit.iconUrl}
                      />
                    ) : (
                      <span className="flex size-11 flex-none items-center justify-center rounded-xl bg-primary text-primary-foreground shadow-md shadow-primary/25">
                        {TYPE_ICONS[hit.projectType] ?? <Box20Regular />}
                      </span>
                    )}
                    <div className="flex min-w-0 flex-1 flex-col gap-0.5">
                      <span className="truncate text-sm font-semibold">
                        {hit.title}
                      </span>
                      <span className="truncate text-xs text-gray-400">
                        {hit.description}
                      </span>
                    </div>
                    <div className="flex flex-none flex-col items-end gap-0.5 text-[11px] text-gray-400">
                      <span className="flex items-center gap-1.5">
                        {hit.typeIcon ? <span>{hit.typeIcon}</span> : null}
                        <span>{hit.typeDisplay}</span>
                      </span>
                      <span>{hit.downloadsDisplay}</span>
                      {hit.author ? <span>{hit.author}</span> : null}
                    </div>
                  </button>
                ))}
                <span className="text-center text-[11px] text-gray-400">
                  {t("共 {0} 个结果", { "0": total || hits.length })}
                </span>
              </>
            )}
          </div>

          {/* 下载区（回到搜索结果页也保留状态） */}
          <div className="flex flex-col gap-2 rounded-2xl nya-panel-inner px-3.5 py-3">
            {supportsModpack ? (
              <span className="text-[11px] text-gray-400">
                {t(
                  "整合包需要安装为独立实例：请在下载页的「整合包」标签里搜索或导入本地整合包。",
                )}
              </span>
            ) : (
              <div className="flex items-center gap-2">
                <Select
                  aria-label={t("下载目标")}
                  className="min-w-0 flex-1"
                  isDisabled={downloading}
                  items={targetOptions}
                  placeholder={t("选择目标实例…")}
                  popoverProps={{ motionProps: popoverMotionProps }}
                  selectedKeys={targetId ? [targetId] : []}
                  size="sm"
                  onSelectionChange={(keys) =>
                    setTargetId(String(Array.from(keys)[0] ?? ""))
                  }
                >
                  {(item) => (
                    <SelectItem key={item.value}>{item.label}</SelectItem>
                  )}
                </Select>
                <Button
                  className="flex-none"
                  color="primary"
                  isDisabled={!canDownload}
                  radius="full"
                  size="sm"
                  startContent={<ArrowDownload20Regular />}
                  onPress={() => void downloadSelected()}
                >
                  {t("下载到实例")}
                </Button>
              </div>
            )}

            {project ? (
              <span className="truncate text-[11px] text-gray-400">
                {t("目标目录：{0}", {
                  "0": subDirectoryForProjectType(project.projectType),
                })}
              </span>
            ) : null}

            {downloading ? (
              <Progress
                aria-label={t("下载进度")}
                className="max-w-full"
                size="sm"
                value={progressPercent}
              />
            ) : null}

            {statusText ? (
              <span className="flex items-center gap-2 truncate text-xs text-gray-600 dark:text-gray-300">
                {downloading ? <CloudArrowDown20Regular /> : null}
                {statusText}
              </span>
            ) : null}

            {savedPath && !downloading ? (
              <div className="flex items-center gap-2">
                <span className="min-w-0 flex-1 truncate text-[11px] text-success-500">
                  {savedPath}
                </span>
                <Button
                  className="flex-none"
                  radius="full"
                  size="sm"
                  startContent={<FolderOpen20Regular />}
                  variant="flat"
                  onPress={() => void OpenInExplorer(savedPath)}
                >
                  {t("打开文件夹")}
                </Button>
              </div>
            ) : null}

            {project && !downloading ? (
              <span className="text-[11px] text-gray-400">
                {t("下载进度可在下载浮标中查看；取消请用浮标上的取消按钮。")}
              </span>
            ) : null}
          </div>

          <div className="flex justify-end">
            <Button
              radius="full"
              size="sm"
              startContent={<Dismiss20Regular />}
              variant="flat"
              onPress={onClose}
            >
              {t("关闭")}
            </Button>
          </div>
        </ModalShell>
      </ModalContent>
    </Modal>
  );
};

export default ResourceSearchDialog;
