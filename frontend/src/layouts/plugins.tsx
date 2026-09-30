/*
 * 插件页：顶栏（添加插件 / 重新加载 / 打开插件目录）+ 左侧插件列表 + 右侧插件详情。
 *
 * 数据来自两处，按插件 id 拼起来：
 *   - 磁盘与清单：Go 侧 PluginAPI（ListPlugins / InstallPlugin / UninstallPlugin / SetPluginDisabled）
 *   - 运行时加载状态：前端插件加载器（usePluginRuntimeStates）
 * 这样"装坏了"（清单异常）与"加载失败"（API 版本不符、入口报错）能分开显示。
 *
 * 注意：Wails WebView2 不支持 window.confirm/alert，卸载确认用弹层（与 account/instances 一致）。
 */
import type { bindings } from "../../wailsjs/go/models";

import React, { useCallback, useEffect, useMemo, useState } from "react";
import {
  Button,
  Chip,
  Divider,
  Input,
  Modal,
  ModalBody,
  ModalContent,
  Switch,
  Spinner,
  Textarea,
} from "@heroui/react";
import {
  Add20Regular,
  ArrowClockwise20Regular,
  Box20Regular,
  Delete20Regular,
  Folder20Regular,
  FolderOpen20Regular,
  PuzzleCube20Regular,
  Save20Regular,
  Search20Regular,
} from "@fluentui/react-icons";
import { AnimatePresence, motion } from "framer-motion";

import {
  EnsurePluginsDirectory,
  InstallPluginArchive,
  InstallPluginDirectory,
  ListPlugins,
  PackagePlugin,
  SavePluginManifest,
  UninstallPlugin,
} from "../../wailsjs/go/bindings/PluginAPI";
import {
  OpenPath,
  SaveFile,
  SelectDirectory,
  SelectFile,
} from "../../wailsjs/go/bindings/SystemAPI";
import { ModalShell, modalBehaviorProps } from "../components/modal-shell";
import { listItemVariants } from "../lib/motion";
import {
  isPluginActive,
  reloadPlugins,
  setPluginEnabled,
  unloadPluginRuntime,
  usePluginRuntimeStates,
  type PluginRuntimeState,
} from "../plugin";
import { t } from "../i18n";

/** 状态呈现：磁盘上的清单问题与运行时加载失败分开，便于定位 */
interface PluginStatus {
  label: string;
  color: "success" | "danger" | "default" | "warning";
  detail?: string;
}

function describeStatus(
  info: bindings.PluginInfo,
  runtime: PluginRuntimeState | undefined,
): PluginStatus {
  if (info.ManifestError) {
    return {
      label: t("清单异常"),
      color: "danger",
      detail: info.ManifestError,
    };
  }
  if (info.Disabled || runtime?.status === "disabled") {
    return { label: t("已停用"), color: "default" };
  }
  if (runtime?.status === "failed") {
    return { label: t("加载失败"), color: "danger", detail: runtime.error };
  }
  if (runtime?.status === "loaded") {
    return { label: t("已启用"), color: "success" };
  }

  return { label: t("未加载"), color: "warning" };
}

function formatBytes(bytes: number): string {
  if (!bytes) return "0 B";
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;

  return `${(bytes / (1024 * 1024)).toFixed(2)} MB`;
}

function formatTime(unixSeconds: number): string {
  if (!unixSeconds) return "—";

  return new Date(unixSeconds * 1000).toLocaleString();
}

function messageOf(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

/**
 * 插件图标 URL：走 /plugins 路由。带上修改时间做查询参数——插件重装后图标换了，
 * 不带参数会命中浏览器内存里的旧图。
 */
function pluginIconUrl(info: bindings.PluginInfo): string {
  if (!info.IconFile) return "";
  const relative = info.IconFile.split("/")
    .map((segment) => encodeURIComponent(segment))
    .join("/");

  return `/plugins/${encodeURIComponent(info.ID)}/${relative}?v=${info.ModifiedAt}`;
}

/** 权限键 → 展示名（与 docs/Extensions_Guide.md §5 的权限表对应）。 */
const PERMISSION_LABELS: Record<string, string> = {
  storage: "存储",
  launch: "启动游戏",
  instances: "实例（只读）",
  "instances-write": "实例（写入）",
  accounts: "账号",
  "launcher-config": "启动器设置（只读）",
  "launcher-config-write": "启动器设置（写入）",
  notifications: "通知",
  clipboard: "剪贴板",
  "open-url": "打开链接",
  "open-path": "打开文件",
  "server-status": "服务器状态",
};

const PluginsPage: React.FC = () => {
  const [plugins, setPlugins] = useState<bindings.PluginInfo[]>([]);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState("");
  const [status, setStatus] = useState("");
  const [selectedId, setSelectedId] = useState("");
  const [searchQuery, setSearchQuery] = useState("");
  const [pendingUninstall, setPendingUninstall] =
    useState<bindings.PluginInfo | null>(null);
  // 清单编辑表单（改已安装插件的元数据；新建骨架在创作中心）
  const [manifestOpen, setManifestOpen] = useState(false);
  const [draftName, setDraftName] = useState("");
  const [draftVersion, setDraftVersion] = useState("");
  const [draftAuthor, setDraftAuthor] = useState("");
  const [draftDescription, setDraftDescription] = useState("");
  const runtimeStates = usePluginRuntimeStates();

  const refresh = useCallback(async () => {
    setLoading(true);
    try {
      const list = (await ListPlugins()) ?? [];

      setPlugins(list);
      setSelectedId((current) =>
        current && list.some((item) => item.ID === current)
          ? current
          : (list[0]?.ID ?? ""),
      );
    } catch (error) {
      setStatus(t("读取插件失败：{0}", { "0": messageOf(error) }));
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  const selected = useMemo(
    () => plugins.find((item) => item.ID === selectedId) ?? null,
    [plugins, selectedId],
  );

  // 列表搜索：按名称 / id / 作者过滤（大小写不敏感）
  const filteredPlugins = useMemo(() => {
    const query = searchQuery.trim().toLowerCase();

    if (!query) return plugins;

    return plugins.filter((item) =>
      `${item.Name ?? ""} ${item.ID} ${item.Author ?? ""}`
        .toLowerCase()
        .includes(query),
    );
  }, [plugins, searchQuery]);

  // 选中项变化时把清单字段填进表单（用户改动在下次切换时才被覆盖）
  useEffect(() => {
    if (!selected) return;
    setDraftName(selected.Name ?? "");
    setDraftVersion(selected.Version ?? "");
    setDraftAuthor(selected.Author ?? "");
    setDraftDescription(selected.Description ?? "");
  }, [selected]);

  const install = async () => {
    let archive = "";

    try {
      archive = await SelectFile(
        t("选择插件包（.nekoex）"),
        t("NekoLauncher 插件包"),
        "*.nekoex",
      );
    } catch {
      /* 对话框被取消 */
    }
    if (!archive) return;

    setBusy("install");
    setStatus(t("正在安装…"));
    try {
      const id = await InstallPluginArchive(archive);

      setStatus(t("已安装插件「{0}」", { "0": id }));
      await reloadPlugins();
      await refresh();
      setSelectedId(id);
    } catch (error) {
      setStatus(t("安装失败：{0}", { "0": messageOf(error) }));
    } finally {
      setBusy("");
    }
  };

  // 打包：选插件源码目录 → 选保存位置 → 生成 .nekoex（实为 zip）
  const packagePlugin = async () => {
    let source = "";

    try {
      source = await SelectDirectory(t("选择插件源码目录（需含 plugin.yaml）"));
    } catch {
      /* 对话框被取消 */
    }
    if (!source) return;

    const segments = source.replace(/[\\/]+$/, "").split(/[\\/]/);
    const suggested = `${segments[segments.length - 1] || "plugin"}.nekoex`;
    let target = "";

    try {
      target = await SaveFile(
        t("保存插件包"),
        suggested,
        t("NekoLauncher 插件包"),
        "*.nekoex",
      );
    } catch {
      /* 对话框被取消 */
    }
    if (!target) return;

    setBusy("package");
    setStatus(t("正在打包…"));
    try {
      const written = await PackagePlugin(source, target);

      setStatus(t("已生成插件包：{0}", { "0": written }));
    } catch (error) {
      setStatus(t("打包失败：{0}", { "0": messageOf(error) }));
    } finally {
      setBusy("");
    }
  };

  const reload = async () => {
    setBusy("reload");
    setStatus(t("正在重新加载插件…"));
    try {
      const results = await reloadPlugins();
      const failed = results.filter((result) => !result.ok);

      setStatus(
        failed.length === 0
          ? t("已重新加载 {0} 个插件", { "0": results.length })
          : t("重新加载完成，{0} 个插件失败", { "0": failed.length }),
      );
      await refresh();
    } finally {
      setBusy("");
    }
  };

  const openDirectory = async () => {
    try {
      const directory = await EnsurePluginsDirectory();

      await OpenPath(directory);
    } catch (error) {
      setStatus(t("打开插件目录失败：{0}", { "0": messageOf(error) }));
    }
  };

  /** 从文件夹安装：开发期直接指向源码目录，不必先打包成 .nekoex */
  const installFromFolder = async () => {
    let source = "";

    try {
      source = await SelectDirectory(t("选择插件文件夹（需含 plugin.yaml）"));
    } catch {
      /* 对话框被取消 */
    }
    if (!source) return;

    setBusy("install-folder");
    setStatus(t("正在安装…"));
    try {
      const id = await InstallPluginDirectory(source);

      setStatus(t("已安装插件「{0}」", { "0": id }));
      await reloadPlugins();
      await refresh();
      setSelectedId(id);
    } catch (error) {
      setStatus(t("安装失败：{0}", { "0": messageOf(error) }));
    } finally {
      setBusy("");
    }
  };

  /**
   * 保存当前选中插件的清单编辑（名称/版本/作者/描述；id 不可改）。
   *
   * 新建骨架的图形化流程在「创作中心 → 插件制作」里（PluginManifestDialog），
   * 这里只补"改已安装插件的元数据"这一半——否则改个名字只能手动编辑 plugin.yaml。
   */
  const saveManifest = async () => {
    if (!selected) return;
    const name = draftName.trim();

    if (!name) {
      setStatus(t("插件名称不能为空"));

      return;
    }
    setBusy("save-manifest");
    setStatus(t("正在保存清单…"));
    try {
      const manifest = JSON.stringify({
        id: selected.ID,
        name,
        version: draftVersion.trim() || selected.Version,
        apiVersion: selected.APIVersion,
        author: draftAuthor.trim(),
        description: draftDescription.trim(),
        entry: selected.Entry,
        icon: selected.IconFile,
      });

      await SavePluginManifest(selected.Directory, manifest);
      setStatus(t("清单已保存"));
      await reloadPlugins();
      await refresh();
      setManifestOpen(false);
    } catch (error) {
      setStatus(t("保存失败：{0}", { "0": messageOf(error) }));
    } finally {
      setBusy("");
    }
  };

  const toggleEnabled = async (info: bindings.PluginInfo, enabled: boolean) => {
    setBusy(info.ID);
    try {
      const result = await setPluginEnabled(info.ID, enabled);

      // 开关是受控的：只写状态串不更新列表，拨过去会被"弹"回原位
      setPlugins((prev) =>
        prev.map((item) =>
          item.ID === info.ID ? { ...item, Disabled: !enabled } : item,
        ),
      );
      // 启用可能加载失败（清单坏 / 入口抛错）：不能照常报"已启用"
      if (enabled && result && !result.ok) {
        setStatus(
          t("启用「{0}」失败：{1}", {
            "0": info.Name || info.ID,
            "1": result.error ?? "",
          }),
        );

        return;
      }
      setStatus(
        enabled
          ? t("已启用「{0}」", { "0": info.Name || info.ID })
          : t("已停用「{0}」", { "0": info.Name || info.ID }),
      );
    } catch (error) {
      setStatus(t("切换失败：{0}", { "0": messageOf(error) }));
    } finally {
      setBusy("");
    }
  };

  const confirmUninstall = async () => {
    const target = pendingUninstall;

    if (!target) return;
    setPendingUninstall(null);
    setBusy(target.ID);
    try {
      // 先摘掉前端运行时再删磁盘：中间窗口里插件的事件订阅不再响应，
      // 不会去 fetch 已被删除的资源
      if (isPluginActive(target.ID)) unloadPluginRuntime(target.ID);
      await UninstallPlugin(target.ID);
      await reloadPlugins();
      await refresh();
      setStatus(t("已卸载「{0}」", { "0": target.Name || target.ID }));
    } catch (error) {
      // 运行时已经摘了但磁盘删除失败（文件被占用等）：重新加载一次，
      // 让插件恢复运行，状态徽章与磁盘真实情况对齐
      await reloadPlugins().catch(() => undefined);
      setStatus(t("卸载失败：{0}", { "0": messageOf(error) }));
    } finally {
      setBusy("");
    }
  };

  const selectedStatus = selected
    ? describeStatus(selected, runtimeStates[selected.ID])
    : null;
  const detailIcon = selected ? pluginIconUrl(selected) : "";

  return (
    <div className="relative h-full w-full flex flex-col overflow-hidden">
      {/* 标题区：顶栏放插件管理动作 */}
      <div className="px-6 pt-5 pb-3 flex flex-col gap-2 flex-shrink-0">
        <h1 className="text-xl font-semibold text-gray-800 dark:text-gray-200">
          {t("插件")}
        </h1>
        <div className="flex flex-wrap items-center gap-2">
          <Button
            color="primary"
            isDisabled={busy !== ""}
            size="sm"
            startContent={<Add20Regular />}
            variant="flat"
            onPress={() => void install()}
          >
            {t("添加插件")}
          </Button>
          <Button
            isDisabled={busy !== ""}
            size="sm"
            startContent={<FolderOpen20Regular />}
            variant="flat"
            onPress={() => void installFromFolder()}
          >
            {t("从文件夹安装")}
          </Button>
          <Divider className="h-6 w-px mx-1" orientation="vertical" />
          <Button
            isDisabled={busy !== "" || !selected}
            size="sm"
            startContent={<PuzzleCube20Regular />}
            variant="flat"
            onPress={() => setManifestOpen(true)}
          >
            {t("编辑清单")}
          </Button>
          <Button
            isDisabled={busy !== ""}
            size="sm"
            startContent={<Box20Regular />}
            variant="flat"
            onPress={() => void packagePlugin()}
          >
            {t("打包插件")}
          </Button>
          <Divider className="h-6 w-px mx-1" orientation="vertical" />
          <Button
            isDisabled={busy !== ""}
            size="sm"
            startContent={<ArrowClockwise20Regular />}
            variant="flat"
            onPress={() => void reload()}
          >
            {t("重新加载")}
          </Button>
          <Button
            size="sm"
            startContent={<Folder20Regular />}
            variant="flat"
            onPress={() => void openDirectory()}
          >
            {t("打开插件目录")}
          </Button>
          {busy !== "" ? <Spinner size="sm" /> : null}
        </div>
      </div>

      {/* 左列表 + 右详情（窄窗口改为上下堆叠：并排时右侧详情会被压成几十像素） */}
      <div className="flex-1 min-h-0 flex flex-col gap-4 px-6 pb-3 md:flex-row">
        {/* 左：插件列表（顶层面板用 nya-panel；nya-panel-inner 是给面板内
            嵌套小面板用的，底色更实会盖住毛玻璃） */}
        <div className="flex max-h-44 w-full flex-shrink-0 flex-col rounded-xl border nya-border nya-panel p-2 md:max-h-none md:w-56">
          <div className="px-1 pb-1.5 pt-1 flex items-center justify-between gap-2">
            <span className="text-[13px] font-semibold text-gray-600 dark:text-gray-300">
              {t("已安装（")}
              {plugins.length}）
            </span>
          </div>
          <Input
            aria-label={t("搜索插件")}
            className="mb-1.5"
            size="sm"
            startContent={<Search20Regular className="h-4 w-4" />}
            value={searchQuery}
            variant="flat"
            onValueChange={setSearchQuery}
          />
          <div className="flex-1 overflow-y-auto flex flex-col gap-0.5">
            <AnimatePresence initial={false}>
              {filteredPlugins.map((info) => {
                const itemStatus = describeStatus(info, runtimeStates[info.ID]);
                const isActive = info.ID === selectedId;
                const icon = pluginIconUrl(info);

                return (
                  <motion.div
                    key={info.ID}
                    layout
                    animate="center"
                    className="overflow-hidden"
                    exit="exit"
                    initial="enter"
                    variants={listItemVariants}
                  >
                    <button
                      className={`flex w-full items-center gap-2 rounded-lg px-2 py-2 text-left transition-colors cursor-pointer ${
                        isActive
                          ? "bg-blue-100 dark:bg-blue-900/40 font-semibold text-blue-600 dark:text-blue-300"
                          : "text-gray-700 dark:text-gray-200 hover:bg-gray-100 dark:hover:bg-gray-800"
                      }`}
                      onClick={() => setSelectedId(info.ID)}
                    >
                      {icon ? (
                        <img
                          alt=""
                          className="h-6 w-6 flex-shrink-0 rounded-md object-cover"
                          src={icon}
                        />
                      ) : (
                        <span className="w-6 h-6 flex-shrink-0 flex items-center justify-center rounded-md bg-gray-100 text-gray-500 dark:bg-gray-700 dark:text-gray-300">
                          <PuzzleCube20Regular />
                        </span>
                      )}
                      <span className="min-w-0 flex-1 leading-tight">
                        <span className="block truncate text-[13px]">
                          {info.Name || info.ID}
                        </span>
                        <span className="block truncate text-[10px] text-gray-400">
                          {info.Version || info.ID}
                        </span>
                      </span>
                      <span
                        className={`h-1.5 w-1.5 flex-shrink-0 rounded-full ${
                          itemStatus.color === "success"
                            ? "bg-emerald-500"
                            : itemStatus.color === "danger"
                              ? "bg-red-500"
                              : itemStatus.color === "warning"
                                ? "bg-amber-500"
                                : "bg-gray-300 dark:bg-gray-600"
                        }`}
                      />
                    </button>
                  </motion.div>
                );
              })}
            </AnimatePresence>

            {!loading && plugins.length === 0 ? (
              <div className="px-2 py-6 text-center text-[11px] leading-relaxed text-gray-400">
                {t("暂无插件")}
              </div>
            ) : null}
            {!loading && plugins.length > 0 && filteredPlugins.length === 0 ? (
              <div className="px-2 py-6 text-center text-[11px] leading-relaxed text-gray-400">
                {t("没有匹配的插件")}
              </div>
            ) : null}
          </div>
        </div>

        {/* 右：插件详情（同上，顶层面板用 nya-panel 保证毛玻璃观感） */}
        <div className="flex-1 min-w-0 flex flex-col rounded-xl border nya-border nya-panel">
          {selected ? (
            <div className="min-h-0 flex-1 overflow-y-auto p-5">
              <div className="flex items-start justify-between gap-4">
                <div className="flex min-w-0 items-start gap-3">
                  {detailIcon ? (
                    <img
                      alt=""
                      className="h-10 w-10 flex-shrink-0 rounded-lg object-cover"
                      src={detailIcon}
                    />
                  ) : null}
                  <div className="min-w-0">
                    <div className="flex items-center gap-2">
                      <span className="truncate text-lg font-semibold text-gray-800 dark:text-gray-200">
                        {selected.Name || selected.ID}
                      </span>
                      {selectedStatus ? (
                        <Chip
                          color={selectedStatus.color}
                          size="sm"
                          variant="flat"
                        >
                          {selectedStatus.label}
                        </Chip>
                      ) : null}
                    </div>
                    <div className="mt-0.5 truncate text-[11px] text-gray-400">
                      {selected.ID}
                    </div>
                  </div>
                </div>
                <div className="flex flex-shrink-0 items-center gap-3">
                  <Switch
                    aria-label={t("启用插件")}
                    color="primary"
                    isDisabled={busy !== "" || !!selected.ManifestError}
                    isSelected={!selected.Disabled}
                    size="sm"
                    onValueChange={(enabled) =>
                      void toggleEnabled(selected, enabled)
                    }
                  />
                  <Button
                    color="danger"
                    isDisabled={busy !== ""}
                    size="sm"
                    startContent={<Delete20Regular />}
                    variant="flat"
                    onPress={() => setPendingUninstall(selected)}
                  >
                    {t("卸载")}
                  </Button>
                </div>
              </div>

              {selectedStatus?.detail ? (
                <div className="mt-4 rounded-lg border border-red-200/70 bg-red-50/70 px-3 py-2 text-[11px] leading-relaxed text-red-600 dark:border-red-900/50 dark:bg-red-950/40 dark:text-red-300">
                  {selectedStatus.detail}
                </div>
              ) : null}

              {selected.Description ? (
                <p className="mt-4 text-[13px] leading-relaxed text-gray-600 dark:text-gray-300">
                  {selected.Description}
                </p>
              ) : null}

              {/* 信任模型一级警示：插件系统没有沙箱，权限列表只是用途声明 */}
              <div className="mt-3 rounded-lg border border-amber-200/70 bg-amber-50/70 px-3 py-2 text-[11px] leading-relaxed text-amber-700 dark:border-amber-900/50 dark:bg-amber-950/40 dark:text-amber-300">
                {t(
                  "插件以与启动器相同的权限运行（无沙箱），可访问本机文件与网络。请只安装信任来源的插件。",
                )}
              </div>

              {selected.Capabilities && selected.Capabilities.length > 0 ? (
                <div className="mt-3 flex flex-wrap items-center gap-1.5">
                  <span className="text-[11px] text-gray-400">{t("权限")}</span>
                  {selected.Capabilities.map((permission) => (
                    <Chip
                      key={permission}
                      className="text-[11px]"
                      color="default"
                      size="sm"
                      variant="flat"
                    >
                      {t(PERMISSION_LABELS[permission] ?? permission)}
                    </Chip>
                  ))}
                </div>
              ) : (
                <p className="mt-3 text-[11px] text-gray-400">
                  {t("未声明（受限 API 调用会直接报错）")}
                </p>
              )}

              <Divider className="my-4" />

              <dl className="grid grid-cols-[92px_1fr] gap-x-4 gap-y-2 text-[12px]">
                {[
                  [t("版本"), selected.Version || "—"],
                  [t("作者"), selected.Author || "—"],
                  [t("API 版本"), selected.APIVersion || "—"],
                  [t("入口"), selected.Entry || "index.js"],
                  [
                    t("体积"),
                    t("{0} · {1} 个文件", {
                      "0": formatBytes(selected.SizeBytes),
                      "1": selected.FileCount,
                    }),
                  ],
                  [t("最近修改"), formatTime(selected.ModifiedAt)],
                  [t("目录"), selected.Directory],
                ].map(([label, value]) => (
                  <React.Fragment key={label}>
                    <dt className="text-gray-400">{label}</dt>
                    <dd className="min-w-0 break-all text-gray-700 dark:text-gray-200">
                      {value}
                    </dd>
                  </React.Fragment>
                ))}
              </dl>
            </div>
          ) : (
            <div className="flex flex-1 flex-col items-center justify-center gap-1.5 text-[13px] text-gray-400">
              {loading ? (
                t("正在读取插件…")
              ) : (
                <>
                  <span className="font-semibold">{t("未选择插件")}</span>
                  <span className="text-xs">
                    {t("从左侧选择一个插件，查看它的详情")}
                  </span>
                </>
              )}
            </div>
          )}
        </div>
      </div>

      {status ? (
        <div className="px-6 pb-3 break-all text-[11px] text-gray-400 flex-shrink-0">
          {status}
        </div>
      ) : null}

      {/* 插件制作：新建骨架 + 编辑当前选中插件的清单
          （Wails WebView2 没有原生 prompt，全部走应用内弹层） */}
      <Modal
        isOpen={manifestOpen}
        size="lg"
        onClose={() => setManifestOpen(false)}
        {...modalBehaviorProps}
      >
        <ModalContent>
          <ModalShell
            title={t("编辑插件清单")}
            onClose={() => setManifestOpen(false)}
          >
            <ModalBody className="max-h-[70vh] overflow-y-auto px-6 pb-5 pt-0">
              <div className="flex flex-col gap-3">
                <div className="text-[11px] leading-relaxed text-gray-400">
                  {selected
                    ? t("正在编辑「{0}」（{1}）；id 不可修改。", {
                        "0": selected.Name || selected.ID,
                        "1": selected.ID,
                      })
                    : t("请先在左侧选择一个插件。")}
                </div>
                <div className="flex flex-col gap-1.5">
                  <span className="text-[12px] font-medium text-gray-600 dark:text-gray-300">
                    {t("名称")}
                  </span>
                  <Input
                    size="sm"
                    value={draftName}
                    variant="bordered"
                    onValueChange={setDraftName}
                  />
                </div>
                <div className="flex flex-wrap gap-3">
                  <div className="flex min-w-[160px] flex-1 flex-col gap-1.5">
                    <span className="text-[12px] font-medium text-gray-600 dark:text-gray-300">
                      {t("版本")}
                    </span>
                    <Input
                      placeholder="0.1.0"
                      size="sm"
                      value={draftVersion}
                      variant="bordered"
                      onValueChange={setDraftVersion}
                    />
                  </div>
                  <div className="flex min-w-[160px] flex-1 flex-col gap-1.5">
                    <span className="text-[12px] font-medium text-gray-600 dark:text-gray-300">
                      {t("作者")}
                    </span>
                    <Input
                      size="sm"
                      value={draftAuthor}
                      variant="bordered"
                      onValueChange={setDraftAuthor}
                    />
                  </div>
                </div>
                <div className="flex flex-col gap-1.5">
                  <span className="text-[12px] font-medium text-gray-600 dark:text-gray-300">
                    {t("简介")}
                  </span>
                  <Textarea
                    minRows={2}
                    size="sm"
                    value={draftDescription}
                    variant="bordered"
                    onValueChange={setDraftDescription}
                  />
                </div>

                <div className="text-[11px] leading-relaxed text-gray-400">
                  {t(
                    "保存只改当前选中插件的清单元数据，id 与入口文件不可修改；要新建插件请到「创作中心 → 插件制作」。",
                  )}
                </div>

                <div className="flex flex-wrap justify-end gap-2">
                  <Button
                    isDisabled={busy !== ""}
                    size="sm"
                    variant="light"
                    onPress={() => setManifestOpen(false)}
                  >
                    {t("关闭")}
                  </Button>
                  <Button
                    color="primary"
                    isDisabled={busy !== "" || !selected}
                    size="sm"
                    startContent={<Save20Regular />}
                    variant="flat"
                    onPress={() => void saveManifest()}
                  >
                    {t("保存清单")}
                  </Button>
                </div>
              </div>
            </ModalBody>
          </ModalShell>
        </ModalContent>
      </Modal>

      {/* 卸载确认：WebView2 不支持 window.confirm，用弹层 */}
      <Modal
        isOpen={!!pendingUninstall}
        size="sm"
        onClose={() => setPendingUninstall(null)}
        {...modalBehaviorProps}
      >
        <ModalContent>
          <ModalShell
            title={t("卸载插件")}
            onClose={() => setPendingUninstall(null)}
          >
            <ModalBody className="px-6 pb-5 pt-0">
              <p className="text-[13px] leading-relaxed text-gray-600 dark:text-gray-300">
                {t("将删除插件")}
                <span className="mx-1 font-medium text-gray-800 dark:text-gray-100">
                  {pendingUninstall?.Name || pendingUninstall?.ID}
                </span>

                {t(
                  "的整个目录，无法撤销。它注册的小组件在主页上会消失（布局里的位置会保留）。",
                )}
              </p>
              <div className="mt-4 flex justify-end gap-2">
                <Button
                  size="sm"
                  variant="light"
                  onPress={() => setPendingUninstall(null)}
                >
                  {t("取消")}
                </Button>
                <Button
                  color="danger"
                  size="sm"
                  variant="flat"
                  onPress={() => void confirmUninstall()}
                >
                  {t("卸载")}
                </Button>
              </div>
            </ModalBody>
          </ModalShell>
        </ModalContent>
      </Modal>
    </div>
  );
};

export default PluginsPage;
