/*
 * Java 下载标签页（从 download.tsx 拆出并重构布局）：
 * 左列「下载新运行时」= 提供商切换 + 版本搜索/筛选 + 候选列表（带 MC 适配标签）；
 * 右列「已安装」= 托管运行时管理（使用中标记 / 一键启用 / 删除）。
 * 顶部常驻「当前全局 Java」状态条，直观回答"现在用的是哪个 Java"。
 * 查询/安装仍走 DownloadAPI：QueryAvailableJavaVersions / InstallJavaRuntime 等。
 */
import type { download } from "../../../wailsjs/go/models";

import React, { useEffect, useMemo, useState } from "react";
import { Button, Chip, Input, Spinner } from "@heroui/react";
import {
  ArrowClockwise20Regular as RefreshIcon,
  CheckmarkCircle20Regular,
  Checkmark20Regular,
  Dismiss20Regular,
  Search20Regular,
  WindowDevTools20Regular,
} from "@fluentui/react-icons";

import {
  DeleteJavaRuntime,
  GetInstalledJavaRuntimes,
  InstallJavaRuntime,
  QueryAvailableJavaVersions,
} from "../../../wailsjs/go/bindings/DownloadAPI";
import {
  GetJavaExecutable,
  GetJavaVersion,
  SaveJava,
} from "../../../wailsjs/go/bindings/ConfigAPI";
import { EventsOn } from "../../../wailsjs/runtime/runtime";
import { notify } from "../overlay/dialog";
import { t } from "../../i18n";

// 顺序必须与 Go JavaVendor 枚举一致：Zulu=0 / Oracle=1 / Temurin=2
const JAVA_VENDORS = ["Azul Zulu", "Oracle OpenJDK", "Eclipse Temurin"];

// 后端模型未序列化 DisplayName/DetailText 方法，按扩展形状读取
type JavaCandidate = download.JavaDownloadCandidate & {
  DisplayName?: string;
  DetailText?: string;
};

/** 候选版本对 Minecraft 的适配说明（对应原「版本选择建议」，直接标注到每一行） */
function compatLabel(major: number | undefined): string {
  if (!major) return "";
  if (major <= 8) return "MC 1.8 ~ 1.16.x";
  if (major <= 16) return t("MC 1.17（部分环境）");
  if (major <= 20) return "MC 1.17 ~ 1.20.4";
  if (major === 21) return "MC 1.20.5+";

  return t("最新快照");
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

/** 路径归一化（大小写 / 分隔符 / 尾空白），用于比对"使用中"的运行时 */
function normalizePath(path: string | undefined | null): string {
  return (path ?? "")
    .replace(/[\\/]+/g, "\\")
    .trim()
    .toLowerCase();
}

const JavaDownloadTab: React.FC = () => {
  // ---------- 可下载候选 ----------
  const [javaVendor, setJavaVendor] = useState(JAVA_VENDORS[0]);
  const [javaCandidates, setJavaCandidates] = useState<JavaCandidate[]>([]);
  const [javaSelection, setJavaSelection] = useState<JavaCandidate | null>(
    null,
  );
  const [javaLoading, setJavaLoading] = useState(false);
  const [javaStatusText, setJavaStatusText] = useState("");
  const [query, setQuery] = useState("");
  const [majorFilter, setMajorFilter] = useState<number | null>(null);

  // ---------- 已安装与全局 Java ----------
  const [javaRuntimes, setJavaRuntimes] = useState<
    download.InstalledJavaRuntime[]
  >([]);
  const [globalJavaPath, setGlobalJavaPath] = useState("");
  const [globalJavaVersion, setGlobalJavaVersion] = useState("");
  const [javaInstalling, setJavaInstalling] = useState(false);

  // ---------- 查询与安装 ----------

  const loadJavaCandidates = async () => {
    setJavaLoading(true);
    setJavaStatusText(t("正在获取可用版本…"));
    try {
      // JavaVendor 枚举按索引传给后端
      const vendorIndex = Math.max(0, JAVA_VENDORS.indexOf(javaVendor));
      const list = ((await QueryAvailableJavaVersions(vendorIndex as never)) ??
        []) as JavaCandidate[];

      setJavaCandidates(list);
      setJavaSelection(null);
      setJavaStatusText("");
    } catch (ex) {
      setJavaStatusText(
        t("获取失败：{0}", { "0": (ex as Error)?.message ?? ex }),
      );
      console.error(t("获取 Java 版本失败"), ex);
    } finally {
      setJavaLoading(false);
    }
  };

  const loadJavaRuntimes = async () => {
    try {
      setJavaRuntimes((await GetInstalledJavaRuntimes()) ?? []);
      setGlobalJavaPath((await GetJavaExecutable()) ?? "");
      setGlobalJavaVersion((await GetJavaVersion()) ?? "");
    } catch (ex) {
      console.error(t("读取已安装 Java 失败"), ex);
    }
  };

  const installJava = async () => {
    if (!javaSelection) return;
    setJavaInstalling(true);
    setJavaStatusText(
      t("开始安装 {0}…", {
        "0": javaSelection.DisplayName ?? `Java ${javaSelection.MajorVersion}`,
      }),
    );
    // 进度统一在右下角下载中心（Java 安装在 Go 侧注册为 kind=java 内容任务），
    // 页面内不再渲染独立进度条，只给一条轻提示。
    notify.info(t("已开始下载，进度见右下角的下载中心"));
    try {
      await InstallJavaRuntime(javaSelection);
      setJavaStatusText(t("安装完成。"));
      await loadJavaRuntimes();
    } catch (ex) {
      setJavaStatusText(
        t("安装失败：{0}", { "0": (ex as Error)?.message ?? ex }),
      );
      console.error(t("安装 Java 失败"), ex);
    } finally {
      setJavaInstalling(false);
    }
  };

  // 不是 React Hook，只是恰以 use 开头的普通回调；改用 apply 前缀避免 hooks 规则误判
  const applyJavaRuntime = async (runtime: download.InstalledJavaRuntime) => {
    try {
      await SaveJava(
        runtime.JavaExecutablePath,
        runtime.MajorVersion ? `Java ${runtime.MajorVersion}` : "Java",
      );
      setJavaStatusText(t("已设为全局 Java。"));
      await loadJavaRuntimes();
    } catch (ex) {
      console.error(t("设置全局 Java 失败"), ex);
    }
  };

  const removeJavaRuntime = async (runtime: download.InstalledJavaRuntime) => {
    try {
      await DeleteJavaRuntime(runtime.DirectoryPath);
      await loadJavaRuntimes();
    } catch (ex) {
      console.error(t("删除 Java 失败"), ex);
    }
  };

  // 初始加载（运行时列表 + 全局 Java 状态）+ 安装进度事件
  useEffect(() => {
    void loadJavaRuntimes();
    // 逐个退订：EventsOff(事件名) 会清掉该事件的全部监听
    const offProgress = EventsOn(
      "download:javaProgress",
      (p: { Percentage?: number; Detail?: string } | null) => {
        if (p) {
          setJavaInstalling(true);
          setJavaStatusText(p.Detail ?? "");
        }
      },
    );

    return () => {
      offProgress();
    };
  }, []);

  // 候选版本随提供商变化重查（首挂载也会执行一次）
  useEffect(() => {
    void loadJavaCandidates();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [javaVendor]);

  // 大版本快捷筛选（候选里出现过的，从新到旧）
  const majorOptions = useMemo(() => {
    const majors = new Set<number>();

    for (const candidate of javaCandidates) {
      if (candidate.MajorVersion) majors.add(candidate.MajorVersion);
    }

    return [...majors].sort((a, b) => b - a);
  }, [javaCandidates]);

  // 搜索 + 大版本过滤
  const filteredCandidates = useMemo(() => {
    const keyword = query.trim().toLowerCase();

    return javaCandidates.filter((candidate) => {
      if (majorFilter && candidate.MajorVersion !== majorFilter) return false;
      if (!keyword) return true;
      const haystack =
        `${candidate.DisplayName ?? ""} Java ${candidate.MajorVersion} ${candidate.DetailText ?? ""}`.toLowerCase();

      return haystack.includes(keyword);
    });
  }, [javaCandidates, query, majorFilter]);

  const isGlobalRuntime = (runtime: download.InstalledJavaRuntime): boolean =>
    Boolean(globalJavaPath) &&
    normalizePath(runtime.JavaExecutablePath) === normalizePath(globalJavaPath);

  const sectionTitle =
    "text-[11px] font-semibold tracking-wider text-gray-400 uppercase";

  return (
    <div className="flex flex-col gap-4">
      {/* 当前全局 Java 状态条（无卡：一行图标 + 文字，不铺面板） */}
      <div className="flex items-center gap-3">
        <span className="flex size-9 flex-none items-center justify-center rounded-lg bg-primary/15 text-primary">
          <WindowDevTools20Regular />
        </span>
        <div className="flex min-w-0 flex-1 flex-col gap-0.5">
          <span className="text-[13px] font-semibold">
            {globalJavaPath
              ? t("当前全局 Java：{0}", { "0": globalJavaVersion || "Java" })
              : t("尚未配置全局 Java（启动时会自动选择合适的运行时）")}
          </span>
          <span className="truncate text-[11px] text-gray-400">
            {globalJavaPath}
          </span>
        </div>
        {globalJavaPath ? (
          <Chip className="flex-none" color="success" size="sm" variant="flat">
            {t("已配置")}
          </Chip>
        ) : null}
      </div>

      {/* 双列：下载新运行时 / 已安装（无卡：窄窗口用横线分隔，宽窗口左右隔一条竖线） */}
      <div className="grid grid-cols-1 items-start gap-x-6 gap-y-5 xl:grid-cols-2">
        {/* 左列：下载新运行时 */}
        <div className="nya-border flex flex-col gap-3 border-b pb-5 xl:border-b-0 xl:pb-0">
          <div className="flex items-center justify-between gap-3">
            <div className={sectionTitle}>{t("下载新运行时")}</div>
            <Button
              isDisabled={javaLoading}
              radius="full"
              size="sm"
              startContent={<RefreshIcon />}
              variant="flat"
              onPress={() => void loadJavaCandidates()}
            >
              {t("刷新")}
            </Button>
          </div>

          {/* 提供商切换（无卡：文字切换，不再铺胶囊轨道） */}
          <div className="flex flex-wrap items-center gap-4">
            {JAVA_VENDORS.map((vendor) => (
              <button
                key={vendor}
                className={`cursor-pointer text-[13px] transition-colors ${
                  vendor === javaVendor
                    ? "font-semibold text-gray-900 dark:text-gray-100"
                    : "text-gray-400 hover:text-gray-600 dark:hover:text-gray-300"
                }`}
                type="button"
                onClick={() => setJavaVendor(vendor)}
              >
                {vendor}
              </button>
            ))}
          </div>

          {/* 搜索 + 大版本快捷筛选 */}
          <div className="flex items-center gap-2">
            <Input
              aria-label={t("搜索 Java 版本")}
              className="min-w-0 flex-1"
              classNames={{
                inputWrapper:
                  "bg-default-100/80 data-[hover=true]:bg-default-200",
              }}
              placeholder={t("搜索版本号…")}
              radius="full"
              size="sm"
              startContent={<Search20Regular className="text-gray-400" />}
              value={query}
              onValueChange={setQuery}
            />
          </div>
          {majorOptions.length > 1 ? (
            <div className="flex flex-wrap items-center gap-1.5">
              <button
                className={`cursor-pointer rounded-full px-2.5 py-1 text-[11px] font-medium transition-colors ${
                  majorFilter === null
                    ? "bg-primary text-white"
                    : "bg-default-100/80 text-gray-500 hover:bg-default-200"
                }`}
                onClick={() => setMajorFilter(null)}
              >
                {t("全部")}
              </button>
              {majorOptions.map((major) => (
                <button
                  key={major}
                  className={`cursor-pointer rounded-full px-2.5 py-1 text-[11px] font-medium tabular-nums transition-colors ${
                    majorFilter === major
                      ? "bg-primary text-white"
                      : "bg-default-100/80 text-gray-500 hover:bg-default-200"
                  }`}
                  onClick={() =>
                    setMajorFilter(majorFilter === major ? null : major)
                  }
                >
                  {major}
                </button>
              ))}
            </div>
          ) : null}

          {/* 候选版本列表（无卡：不铺底色方框，行靠 hover 色带与选中带区分） */}
          <div className="nya-scroll flex max-h-[300px] min-h-[140px] flex-col gap-1 overflow-y-auto pr-1">
            {javaLoading ? (
              <div className="flex items-center justify-center gap-2 py-8 text-xs text-gray-400">
                <Spinner size="sm" /> {t("正在获取")} {javaVendor}{" "}
                {t("的可用版本…")}
              </div>
            ) : filteredCandidates.length === 0 ? (
              <div className="py-8 text-center text-xs text-gray-400">
                {javaCandidates.length === 0
                  ? t("暂无可用版本")
                  : t("没有匹配的版本")}
              </div>
            ) : (
              filteredCandidates.map((candidate, index) => {
                const selected = javaSelection === candidate;
                const title =
                  candidate.DisplayName ?? `Java ${candidate.MajorVersion}`;

                return (
                  <button
                    key={`${candidate.MajorVersion}-${candidate.BuildVersion}-${index}`}
                    className={`flex cursor-pointer items-center gap-2.5 rounded-lg px-3 py-2 text-left transition-colors ${
                      selected
                        ? "bg-primary/10"
                        : "hover:bg-default-100/70 dark:hover:bg-white/5"
                    }`}
                    onClick={() => setJavaSelection(candidate)}
                  >
                    <span className="flex min-w-0 flex-1 flex-col gap-0.5">
                      <span className="truncate text-[13px] font-semibold">
                        {title}
                      </span>
                      <span className="truncate text-[11px] text-gray-400">
                        {[
                          candidate.DetailText,
                          formatBytes(candidate.SizeBytes),
                        ]
                          .filter(Boolean)
                          .join(" · ")}
                      </span>
                    </span>
                    <span className="flex-none rounded-full bg-primary/10 px-2 py-0.5 text-[10px] font-medium text-primary">
                      {compatLabel(candidate.MajorVersion)}
                    </span>
                  </button>
                );
              })
            )}
          </div>

          {/* 选中摘要 + 安装按钮 + 进度 */}
          <div className="flex items-center justify-between gap-3">
            <Chip className="max-w-[65%] truncate" size="sm" variant="flat">
              {javaSelection
                ? (javaSelection.DisplayName ??
                  `Java ${javaSelection.MajorVersion}`)
                : t("未选择版本")}
            </Chip>
            <Button
              color="primary"
              isDisabled={!javaSelection || javaInstalling}
              isLoading={javaInstalling}
              radius="full"
              size="sm"
              onPress={() => void installJava()}
            >
              {t("下载安装")}
            </Button>
          </div>
          {javaInstalling ? (
            <div className="text-[11px] text-gray-400">
              {t("实时进度与剩余时间见右下角的下载中心")}
            </div>
          ) : null}
          {javaStatusText ? (
            <div className="truncate text-xs text-primary">
              {javaStatusText}
            </div>
          ) : null}
        </div>

        {/* 右列：已安装的运行时（无卡：宽窗口只与左列隔一条竖线） */}
        <div className="nya-border flex flex-col gap-2 xl:border-l xl:pl-6">
          <div className={sectionTitle}>{t("已安装的运行时")}</div>
          {javaRuntimes.length === 0 ? (
            <p className="py-2 text-xs text-gray-400">
              {t("暂无托管 Java 运行时")}
            </p>
          ) : (
            <div className="nya-scroll flex max-h-[430px] flex-col gap-2 overflow-y-auto pr-0.5">
              {javaRuntimes.map((runtime) => {
                const inUse = isGlobalRuntime(runtime);

                return (
                  <div
                    key={runtime.DirectoryPath}
                    className={`flex items-center gap-3 rounded-lg px-3 py-2.5 ${
                      inUse ? "bg-primary/10" : ""
                    }`}
                  >
                    <span className="flex-none items-center gap-1 rounded-full bg-primary/15 px-2.5 py-0.5 text-[12px] font-semibold text-primary">
                      {runtime.MajorVersion
                        ? `Java ${runtime.MajorVersion}`
                        : "Java"}
                    </span>
                    <span
                      className="min-w-0 flex-1 truncate text-[11px] text-gray-400"
                      title={runtime.JavaExecutablePath}
                    >
                      {runtime.JavaExecutablePath}
                    </span>
                    {inUse ? (
                      <span className="flex flex-none items-center gap-1 text-[11px] font-medium text-success-600 dark:text-success-400">
                        <CheckmarkCircle20Regular className="h-4 w-4" />{" "}
                        {t("使用中")}
                      </span>
                    ) : (
                      <>
                        <Button
                          className="flex-none"
                          radius="full"
                          size="sm"
                          variant="flat"
                          onPress={() => void applyJavaRuntime(runtime)}
                        >
                          {t("使用")}
                        </Button>
                        <Button
                          isIconOnly
                          aria-label={t("删除此运行时")}
                          className="flex-none"
                          color="danger"
                          radius="full"
                          size="sm"
                          variant="light"
                          onPress={() => void removeJavaRuntime(runtime)}
                        >
                          <Dismiss20Regular />
                        </Button>
                      </>
                    )}
                  </div>
                );
              })}
            </div>
          )}
          <div className="flex items-center gap-1.5 text-[11px] text-gray-400">
            <Checkmark20Regular className="h-3.5 w-3.5 flex-none text-success-500" />

            {t(
              "「使用中」即启动游戏默认采用的 Java，可在设置页按实例单独覆盖。",
            )}
          </div>
        </div>
      </div>
    </div>
  );
};

export default JavaDownloadTab;
