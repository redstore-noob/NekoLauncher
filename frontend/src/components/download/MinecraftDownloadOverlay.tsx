/*
 * Minecraft 版本下载确认弹层（移植自旧版 components/overlay/MinecraftDownloadOverlay.tsx，
 * 对应 Avalonia MinecraftDownloadOverlay.axaml）。
 * 职责：选择加载器类型/版本、实例名，确认后回调 onConfirm。
 * 注意：加载器枚举索引必须与 Go ModLoaderType 对齐：Vanilla=0/Fabric=1/Quilt=2/NeoForge=3/Forge=4/OptiFine=5。
 * 本组件不含任何全屏遮罩 —— 全屏 loading 遮罩等待联网请求时永不关闭会把整个界面盖死
 * （正是旧版下载页"遮罩层卡死"的根因），这里只用 HeroUI Modal 自身的背景遮罩。
 *
 * 布局（参考设计稿）：
 *   标题行 = 该 Minecraft 版本的图标头像 + "Minecraft {id}" + 类型徽章；
 *   左栏 = 加载器选择列表（每项左侧带加载器图标，原版单独放在分隔线下）；
 *   右栏 = 加载器版本列表（选原版时不显示），超过一页时底部翻页；
 *   底栏 = 实例名输入 + 下载按钮。
 */
import type { download, models } from "../../../wailsjs/go/models";

import React, { useEffect, useRef, useState } from "react";
import { Button, Input, Modal, ModalContent, Switch } from "@heroui/react";
import { Cube20Regular as CubeIcon } from "@fluentui/react-icons";

import { ModalShell, modalBehaviorProps } from "../modal-shell";
import {
  CreateDefaultInstanceName,
  GetModLoaderVersions,
} from "../../../wailsjs/go/bindings/DownloadAPI";
import { t } from "../../i18n";

export interface MinecraftDownloadConfirmOptions {
  loaderType: number;
  loaderVersion: download.ModLoaderVersion | null;
  instanceName: string;
  skipFabricApi: boolean;
}

interface Props {
  version: models.MinecraftVersion | null;
  onClose: () => void;
  onConfirm: (options: MinecraftDownloadConfirmOptions) => void;
}

// 与 Go ModLoaderType 枚举索引一一对应
const LOADER_TYPES = [
  { value: 0, label: t("原版"), icon: "vanilla" },
  { value: 1, label: "Fabric", icon: "fabric" },
  { value: 2, label: "Quilt", icon: "svg:quilt" },
  { value: 3, label: "NeoForge", icon: "neoforge" },
  { value: 4, label: "Forge", icon: "forge" },
  { value: 5, label: "OptiFine", icon: "svg:optifine" },
];

const TYPE_LABELS: Record<string, string> = {
  release: t("正式版"),
  snapshot: t("快照版"),
  old_alpha: t("远古版本"),
  old_beta: t("远古版本"),
};

// 版本类型徽章配色（与下载页 versionTypeMeta 一致的语义）
const TYPE_CHIP: Record<string, string> = {
  release: "bg-success-500/15 text-success-600 dark:text-success-400",
  snapshot: "bg-warning-500/15 text-warning-600 dark:text-warning-400",
  old_alpha: "bg-default-100 text-gray-500",
  old_beta: "bg-default-100 text-gray-500",
};

// 版本类型 → 头像用的 instance-icons 图标（缺失回落 Fluent 立方体）
const VERSION_TYPE_ICON: Record<string, string> = {
  release: "vanilla",
  snapshot: "snapshot_version",
  old_alpha: "old_version",
  old_beta: "old_version",
};

// 加载器版本列表每页条数（对齐设计稿的分页样式）
const VERSIONS_PER_PAGE = 8;

// 翻页条页码窗口：始终显示首末页，当前页前后各保留 1 页，其余折叠成省略号。
// 返回值里 -1 表示省略号占位。
function pagerPages(current: number, count: number): number[] {
  if (count <= 7) return Array.from({ length: count }, (_, i) => i);
  const pages = new Set<number>([
    0,
    count - 1,
    current - 1,
    current,
    current + 1,
  ]);

  const list = [...pages]
    .filter((p) => p >= 0 && p < count)
    .sort((a, b) => a - b);
  const result: number[] = [];

  for (let i = 0; i < list.length; i++) {
    if (i > 0 && list[i] - list[i - 1] > 1) result.push(-result.length - 1);
    result.push(list[i]);
  }

  return result;
}

// ---------------------------------------------------------------------------
// 内联 SVG 加载器图标：Quilt / OptiFine 没有现成的 instance-icons 资源，
// 用简单的自绘 SVG 兜底（不依赖网络、不引入外部版权素材）。
// ---------------------------------------------------------------------------

// Quilt：四色拼布方块（Quilt 官方 logo 的简化几何版）
const QuiltIcon: React.FC<{ className?: string }> = ({ className }) => (
  <svg aria-hidden="true" className={className} viewBox="0 0 24 24">
    <rect fill="#c98bd4" height="10" rx="2" width="10" x="2" y="2" />
    <rect fill="#8b9fd4" height="10" rx="2" width="10" x="12" y="2" />
    <rect fill="#d4a94a" height="10" rx="2" width="10" x="2" y="12" />
    <rect fill="#6fbf8f" height="10" rx="2" width="10" x="12" y="12" />
    <rect
      fill="#3d2c3e"
      height="20"
      opacity="0.25"
      width="2.5"
      x="10.75"
      y="2"
    />
    <rect
      fill="#3d2c3e"
      height="2.5"
      opacity="0.25"
      width="20"
      x="2"
      y="10.75"
    />
  </svg>
);

// OptiFine：蓝色圆角方块 + "OF" 字样（官方 logo 的简化版）
const OptiFineIcon: React.FC<{ className?: string }> = ({ className }) => (
  <svg aria-hidden="true" className={className} viewBox="0 0 24 24">
    <rect fill="#3b6fd4" height="20" rx="4" width="20" x="2" y="2" />
    <text
      fill="#fff"
      fontFamily="system-ui, sans-serif"
      fontSize="9"
      fontWeight="700"
      textAnchor="middle"
      x="12"
      y="16"
    >
      OF
    </text>
  </svg>
);

// 加载器图标：icon 以 "svg:" 前缀走内联 SVG，否则按 instance-icons 文件名加载图片
const LoaderIcon: React.FC<{ icon: string; className?: string }> = ({
  icon,
  className = "size-5",
}) => {
  const [broken, setBroken] = useState(false);

  if (icon === "svg:quilt") return <QuiltIcon className={className} />;
  if (icon === "svg:optifine") return <OptiFineIcon className={className} />;
  if (!broken)
    return (
      <img
        alt=""
        className={`${className} object-contain`}
        src={`/instance-icons/${icon}.png`}
        onError={() => setBroken(true)}
      />
    );

  return <CubeIcon className={className} />;
};

// OverlayHelpers.IsValidInstanceName：拒绝空 / "." ".." / 非法文件名字符 / 路径分隔符
function validateInstanceName(name: string): string {
  if (!name || !name.trim()) return t("请输入实例名称。");
  if (name === "." || name === "..") return t("实例名称非法。");
  if (/[\\/:*?"<>|]/.test(name))
    return t("实例名包含不安全字符，请换一个名字。");

  return "";
}

const MinecraftDownloadOverlay: React.FC<Props> = ({
  version,
  onClose,
  onConfirm,
}) => {
  const [loaderType, setLoaderType] = useState(0);
  const [loaderVersions, setLoaderVersions] = useState<
    download.ModLoaderVersion[]
  >([]);
  const [loaderVersionIndex, setLoaderVersionIndex] = useState("");
  const [loaderLoading, setLoaderLoading] = useState(false);
  const [loaderHint, setLoaderHint] = useState("");
  const [skipFabricApi, setSkipFabricApi] = useState(false);
  const [instanceName, setInstanceName] = useState("");
  const [instanceNameHint, setInstanceNameHint] = useState("");
  const [statusText, setStatusText] = useState("");
  const [page, setPage] = useState(0);

  // 快速切换加载器时丢弃过期请求
  const loadSeq = useRef(0);

  // version 变化时复位表单并预填实例名
  useEffect(() => {
    if (!version) return;
    setLoaderType(0);
    setLoaderVersions([]);
    setLoaderVersionIndex("");
    setLoaderHint("");
    setSkipFabricApi(false);
    setStatusText("");
    setPage(0);
    setInstanceName(version.id);
    setInstanceNameHint(t("版本将安装至 versions/{0}/", { "0": version.id }));
  }, [version]);

  // 切换加载器：拉取 Loader 元数据，优先选中稳定版
  const applyLoaderType = async (type: number) => {
    setLoaderType(type);
    setPage(0);
    setStatusText("");
    const mcId = version?.id;

    if (!mcId) return;
    if (type === 0) {
      setLoaderVersions([]);
      setLoaderVersionIndex("");
      setLoaderHint("");
      setInstanceName(mcId);
      setInstanceNameHint(t("版本将安装至 versions/{0}/", { "0": mcId }));

      return;
    }
    const seq = ++loadSeq.current;

    setLoaderLoading(true);
    setLoaderVersions([]);
    setLoaderVersionIndex("");
    setLoaderHint(t("正在获取可用版本…"));
    try {
      const list = (await GetModLoaderVersions(type as never, mcId)) ?? [];

      if (seq !== loadSeq.current) return;
      setLoaderVersions(list);
      if (list.length > 0) {
        const preferred = list.findIndex((v) => v.IsStable);

        await selectLoaderVersion(type, preferred >= 0 ? preferred : 0, list);
      } else {
        setLoaderHint(t("该 Minecraft 版本暂无可用的加载器版本。"));
      }
    } catch (ex) {
      if (seq !== loadSeq.current) return;
      setLoaderHint(
        t("获取版本列表失败：{0}", { "0": (ex as Error)?.message ?? ex }),
      );
    } finally {
      if (seq === loadSeq.current) setLoaderLoading(false);
    }
  };

  const selectLoaderVersion = async (
    type: number,
    index: number,
    list: download.ModLoaderVersion[],
  ) => {
    const lv = list[index];

    if (!lv) return;
    setLoaderVersionIndex(String(index));
    setLoaderHint(
      lv.IsStable ? t("推荐版本") : t("非稳定版本，可能存在兼容性问题"),
    );
    try {
      const name = await CreateDefaultInstanceName(
        type as never,
        lv.LoaderVersion,
        version?.id ?? "",
      );

      setInstanceName(name);
      setInstanceNameHint(t("版本将安装至 versions/{0}/", { "0": name }));
    } catch (ex) {
      console.error(t("生成默认实例名失败"), ex);
    }
  };

  const onLoaderVersionChange = (index: string) => {
    setLoaderVersionIndex(index);
    const i = Number(index);

    if (!Number.isNaN(i))
      void selectLoaderVersion(loaderType, i, loaderVersions);
  };

  const pageCount = Math.max(
    1,
    Math.ceil(loaderVersions.length / VERSIONS_PER_PAGE),
  );
  const pageVersions = loaderVersions.slice(
    page * VERSIONS_PER_PAGE,
    (page + 1) * VERSIONS_PER_PAGE,
  );
  const typeLabel = version ? (TYPE_LABELS[version.type] ?? version.type) : "";
  const versionIconKey = version
    ? (VERSION_TYPE_ICON[version.type] ?? VERSION_TYPE_ICON.release)
    : "";

  const onDownload = () => {
    if (!version) return;
    let loaderVersion: download.ModLoaderVersion | null = null;

    if (loaderType !== 0) {
      const i = Number(loaderVersionIndex);

      loaderVersion = loaderVersions[i] ?? null;
      if (!loaderVersion) {
        setStatusText(t("请选择加载器版本。"));

        return;
      }
    }
    const fallback = loaderVersion
      ? `${version.id}-${loaderVersion.LoaderVersion}`
      : version.id;
    const name = (instanceName || "").trim() || fallback;
    const nameError = validateInstanceName(name);

    if (nameError) {
      setStatusText(nameError);

      return;
    }
    onConfirm({ loaderType, loaderVersion, instanceName: name, skipFabricApi });
  };

  return (
    // isDismissable=false（modalBehaviorProps）：点外部不关闭，防止误触——关闭一律走右上角 X
    <Modal
      isOpen={version !== null}
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
          icon={
            version ? (
              <img
                alt=""
                className="size-10 rounded-[10px] object-contain"
                src={`/instance-icons/${versionIconKey}.png`}
                onError={(ev) => {
                  // 头像加载失败时退回默认立方体图标
                  (ev.target as HTMLImageElement).style.display = "none";
                }}
              />
            ) : (
              <CubeIcon />
            )
          }
          subtitle={t("下载 Minecraft")}
          title={version ? `Minecraft ${version.id}` : ""}
          titleExtra={
            version ? (
              <span
                className={`rounded-full px-2.5 py-1 text-[11px] font-semibold ${TYPE_CHIP[version.type] ?? "bg-default-100 text-gray-500"}`}
              >
                {typeLabel}
              </span>
            ) : null
          }
          onClose={onClose}
        >
          <div className="grid grid-cols-[190px_1fr] gap-4">
            {/* 左栏：加载器选择 */}
            <div className="nya-panel-inner flex flex-col gap-1 rounded-2xl p-2">
              {LOADER_TYPES.filter((lt) => lt.value !== 0).map((lt) => (
                <button
                  key={lt.value}
                  className={`flex cursor-pointer items-center gap-2.5 rounded-xl px-3 py-2 text-[13px] font-semibold transition-colors ${
                    loaderType === lt.value
                      ? "bg-primary-100 text-primary-600 dark:bg-primary-900/40 dark:text-primary-300"
                      : "text-gray-600 hover:bg-gray-200 dark:text-gray-300 dark:hover:bg-gray-800"
                  }`}
                  onClick={() => {
                    if (lt.value !== loaderType) void applyLoaderType(lt.value);
                  }}
                >
                  <LoaderIcon icon={lt.icon} />
                  {lt.label}
                </button>
              ))}
              <div className="mx-2 my-1 border-t border-gray-200 dark:border-gray-700/60" />
              {LOADER_TYPES.filter((lt) => lt.value === 0).map((lt) => (
                <button
                  key={lt.value}
                  className={`flex cursor-pointer items-center gap-2.5 rounded-xl px-3 py-2 text-[13px] font-semibold transition-colors ${
                    loaderType === lt.value
                      ? "bg-primary-100 text-primary-600 dark:bg-primary-900/40 dark:text-primary-300"
                      : "text-gray-600 hover:bg-gray-200 dark:text-gray-300 dark:hover:bg-gray-800"
                  }`}
                  onClick={() => {
                    if (lt.value !== loaderType) void applyLoaderType(lt.value);
                  }}
                >
                  <LoaderIcon icon={lt.icon} />
                  {lt.label}
                </button>
              ))}
            </div>

            {/* 右栏：加载器版本列表（原版时隐藏） */}
            <div className="flex min-h-0 min-w-0 flex-col">
              {loaderType !== 0 && (
                <>
                  {loaderLoading ? (
                    <div className="nya-panel-inner flex flex-1 items-center justify-center gap-2 rounded-2xl p-6 text-[13px] text-gray-400">
                      <span className="size-4 animate-spin rounded-full border-2 border-current border-t-transparent" />
                      {t("正在获取可用版本…")}
                    </div>
                  ) : pageVersions.length > 0 ? (
                    <>
                      <div className="nya-panel-inner flex flex-1 flex-col gap-0.5 overflow-y-auto rounded-2xl p-2">
                        {pageVersions.map((lv) => {
                          const globalIndex = loaderVersions.indexOf(lv);
                          const selected =
                            loaderVersionIndex === String(globalIndex);

                          return (
                            <button
                              key={lv.LoaderVersion}
                              className={`flex cursor-pointer items-center gap-2.5 rounded-xl px-3 py-2 text-left text-[13px] transition-colors ${
                                selected
                                  ? "bg-primary-100 font-semibold text-primary-600 dark:bg-primary-900/40 dark:text-primary-300"
                                  : "text-gray-600 hover:bg-gray-200 dark:text-gray-300 dark:hover:bg-gray-800"
                              }`}
                              onClick={() =>
                                onLoaderVersionChange(String(globalIndex))
                              }
                            >
                              <span className="min-w-0 flex-1 truncate font-mono text-[12.5px]">
                                {lv.LoaderVersion}
                              </span>
                            </button>
                          );
                        })}
                      </div>
                      {/* 翻页条：页码多时窗口化显示当前页附近的页码，避免溢出 */}
                      {pageCount > 1 && (
                        <div className="mt-2 flex items-center justify-center gap-1.5 overflow-hidden">
                          {pagerPages(page, pageCount).map((p) =>
                            p < 0 ? (
                              <span
                                key={`ellipsis-${p}`}
                                className="px-0.5 text-[12px] text-gray-500"
                              >
                                …
                              </span>
                            ) : (
                              <button
                                key={p}
                                className={`h-7 min-w-7 flex-shrink-0 cursor-pointer rounded-lg px-1.5 text-[12px] font-semibold transition-colors ${
                                  p === page
                                    ? "bg-primary-500/15 text-primary-600 dark:text-primary-300"
                                    : "text-gray-500 hover:bg-gray-200 dark:hover:bg-gray-800"
                                }`}
                                onClick={() => setPage(p)}
                              >
                                {p + 1}
                              </button>
                            ),
                          )}
                        </div>
                      )}
                    </>
                  ) : (
                    <div className="nya-panel-inner flex flex-1 items-center justify-center rounded-2xl p-6 text-center text-[13px] text-gray-400">
                      {loaderHint ||
                        t("该 Minecraft 版本暂无可用的加载器版本。")}
                    </div>
                  )}
                  {loaderHint && pageVersions.length > 0 && (
                    <span className="mt-1.5 text-[11px] text-gray-400">
                      {loaderHint}
                    </span>
                  )}
                  {loaderType === 1 && (
                    <label className="mt-1.5 flex cursor-pointer items-center gap-2 text-xs text-gray-600 dark:text-gray-300">
                      <Switch
                        isSelected={skipFabricApi}
                        size="sm"
                        onValueChange={setSkipFabricApi}
                      />

                      {t("不主动下载 Fabric API")}
                    </label>
                  )}
                </>
              )}
              {loaderType === 0 && (
                <div className="nya-panel-inner flex flex-1 flex-col items-center justify-center gap-2 rounded-2xl p-6 text-center">
                  <img
                    alt=""
                    className="size-12 object-contain"
                    src="/instance-icons/vanilla.png"
                  />
                  <span className="text-[13px] font-semibold text-gray-600 dark:text-gray-300">
                    {t("原版")}
                  </span>
                  <span className="text-[11px] text-gray-400">
                    {t("版本将安装至 versions/{0}/", {
                      "0": (instanceName || "").trim() || version?.id || "",
                    })}
                  </span>
                </div>
              )}
            </div>
          </div>

          {/* 底栏：实例名 + 状态 + 操作 */}
          <div className="mt-4 flex items-end gap-3">
            <div className="flex min-w-0 flex-1 flex-col gap-1">
              <Input
                aria-label={t("实例名称")}
                placeholder={t("实例名称")}
                size="sm"
                value={instanceName}
                variant="bordered"
                onValueChange={setInstanceName}
              />
              {instanceNameHint ? (
                <span className="truncate text-[11px] text-gray-400">
                  {instanceNameHint}
                </span>
              ) : null}
              {statusText ? (
                <span className="text-xs text-red-500">{statusText}</span>
              ) : null}
            </div>
            <div className="flex flex-shrink-0 gap-2">
              <Button size="sm" variant="light" onPress={onClose}>
                {t("取消")}
              </Button>
              <Button color="primary" size="sm" onPress={onDownload}>
                {t("下载")}
              </Button>
            </div>
          </div>
        </ModalShell>
      </ModalContent>
    </Modal>
  );
};

export default MinecraftDownloadOverlay;
