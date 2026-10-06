/*
 * Mod/资源包/光影包版本管理弹窗：列出资源站上的全部版本，支持升级与降级
 * （选择任意版本 → 后端 ApplyContentUpdate：下载 → SHA-1 校验 → 备份 → 替换）。
 * 数据来源 ContentAPI.GetContentVersionOptions（哈希反查 Modrinth 归属项目）；
 * 识别不了的文件（自建包 / CurseForge 独占）展示 Notice 说明而不是报错。
 */
import type { download, models } from "../../../wailsjs/go/models";

import React, { useEffect, useMemo, useState } from "react";
import { Button, Chip, Modal, ModalContent, Switch } from "@heroui/react";
import {
  ArrowDownload20Regular,
  CheckmarkCircle20Regular,
  FolderOpen20Regular,
  History20Regular,
  Open20Regular,
} from "@fluentui/react-icons";

import { ModalShell, modalBehaviorProps } from "../modal-shell";
import {
  ApplyContentUpdate,
  GetContentVersionOptions,
} from "../../../wailsjs/go/bindings/ContentAPI";
import { OpenPath } from "../../../wailsjs/go/bindings/SystemAPI";
import { EventsOn } from "../../../wailsjs/runtime/runtime";
import { asArray, asObject } from "../../lib/guards";
import { t } from "../../i18n";

// searchKeyword 从文件名猜资源站搜索关键词：去掉扩展名与版本号尾巴
// （sodium-0.6.0.jar → sodium）。
function searchKeyword(fileName: string): string {
  return fileName
    .replace(/\.(jar|zip|litemod)$/i, "")
    .replace(/[-_+ ]?\d[\w.~-]*$/, "")
    .trim();
}

export interface ModVersionTarget {
  FilePath: string;
  FileName: string;
  ProjectName?: string;
  KindLabel?: string;
}

interface ModVersionDialogProps {
  open: boolean;
  target: ModVersionTarget | null;
  sourcePath: string;
  minecraftDirectory: string;
  versionId: string;
  onClose: () => void;
  /** 切换成功后回调（message 已含备份路径说明），父级负责刷新列表 */
  onSwitched?: (message: string) => void;
}

function channelTone(releaseType: string): string {
  switch (releaseType) {
    case "beta":
      return "bg-warning-500/15 text-warning-600 dark:text-warning-400";
    case "alpha":
      return "bg-secondary/15 text-secondary-600 dark:text-secondary-400";
    default:
      return "bg-success-500/15 text-success-600 dark:text-success-400";
  }
}

const ModVersionDialog: React.FC<ModVersionDialogProps> = ({
  open,
  target,
  sourcePath,
  minecraftDirectory,
  versionId,
  onClose,
  onSwitched,
}) => {
  const [options, setOptions] = useState<download.ContentVersionOptions | null>(
    null,
  );
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");
  const [onlyCompatible, setOnlyCompatible] = useState(true);
  const [busyKey, setBusyKey] = useState("");
  const [confirmKey, setConfirmKey] = useState("");
  const [progressText, setProgressText] = useState("");

  useEffect(() => {
    if (!open || !target?.FilePath) return;
    let alive = true;

    setLoading(true);
    setError("");
    setOptions(null);
    setConfirmKey("");
    setBusyKey("");
    setOnlyCompatible(true);
    GetContentVersionOptions(
      sourcePath,
      minecraftDirectory,
      versionId,
      target.FilePath,
    )
      .then((result: unknown) => {
        if (alive) setOptions(asObject<download.ContentVersionOptions>(result));
      })
      .catch((ex: unknown) => {
        if (alive)
          setError(
            t("读取版本列表失败：{0}", {
              "0": (ex as Error)?.message ?? String(ex),
            }),
          );
      })
      .finally(() => {
        if (alive) setLoading(false);
      });

    return () => {
      alive = false;
    };
  }, [open, target?.FilePath, sourcePath, minecraftDirectory, versionId]);

  // 下载进度（后端 content:updateDownload 事件）在按钮旁给一行提示
  useEffect(() => {
    if (!open || !busyKey) return;
    const off = EventsOn(
      "content:updateDownload",
      (payload: { downloaded?: number; total?: number }) => {
        const total = payload?.total ?? 0;
        const downloaded = payload?.downloaded ?? 0;

        setProgressText(
          total > 0
            ? `${(downloaded / 1048576).toFixed(1)} / ${(total / 1048576).toFixed(1)} MB`
            : `${(downloaded / 1048576).toFixed(1)} MB`,
        );
      },
    );

    return () => off?.();
  }, [open, busyKey]);

  const versions = useMemo(
    () => asArray<models.ResourceVersion>(options?.Versions ?? []),
    [options],
  );
  const visible = useMemo(
    () =>
      onlyCompatible
        ? versions.filter(
            (item) =>
              item.matchesInstance ||
              item.versionId === options?.CurrentVersionID,
          )
        : versions,
    [versions, onlyCompatible, options?.CurrentVersionID],
  );

  if (!open || !target) return null;

  const switchTo = async (version: models.ResourceVersion) => {
    // 两步确认（WebView2 不支持 window.confirm）
    const key = version.versionId;

    if (confirmKey !== key) {
      setConfirmKey(key);

      return;
    }
    setConfirmKey("");
    setBusyKey(key);
    setProgressText("");
    try {
      const applied = await ApplyContentUpdate(
        version.fileUrl,
        target.FilePath,
        version.sha1 ?? "",
      );
      const message =
        applied?.Message ||
        t("已切换到版本 {0}。", { "0": version.versionNumber });

      onSwitched?.(message);
      onClose();
    } catch (ex) {
      setError(
        t("切换版本失败：{0}", { "0": (ex as Error)?.message ?? String(ex) }),
      );
    } finally {
      setBusyKey("");
      setProgressText("");
    }
  };

  return (
    <Modal isOpen size="lg" onClose={onClose} {...modalBehaviorProps}>
      <ModalContent>
        <ModalShell
          icon={<History20Regular />}
          title={t("版本管理")}
          onClose={onClose}
        >
          <div className="flex flex-col gap-2 text-[13px]">
            <div className="flex flex-wrap items-center gap-2">
              <span className="font-semibold">{target.FileName}</span>
              {options?.ProjectName ? (
                <span className="text-xs text-gray-400">
                  {options.ProjectName}
                </span>
              ) : null}
              {options?.CurrentVersionNumber ? (
                <Chip color="primary" radius="full" size="sm" variant="flat">
                  {t("当前 {0}", { "0": options.CurrentVersionNumber })}
                </Chip>
              ) : null}
            </div>

            {loading ? (
              <div className="py-8 text-center text-sm text-gray-400">
                {t("正在读取版本列表…")}
              </div>
            ) : error ? (
              <div className="rounded-md bg-danger-50 px-3 py-2 text-xs break-all text-danger dark:bg-danger-500/10">
                {error}
              </div>
            ) : options?.Notice ? (
              <div className="flex flex-col gap-2">
                <div className="rounded-md bg-default-100/80 px-3 py-2 text-xs text-gray-500 dark:text-gray-400">
                  {options.Notice}
                </div>
                {/* 识别失败的降级操作：手工路线图（打开目录 + 资源站搜索） */}
                <div className="flex flex-wrap items-center justify-center gap-2">
                  <Button
                    radius="full"
                    size="sm"
                    startContent={<FolderOpen20Regular />}
                    variant="flat"
                    onPress={() => {
                      const directory =
                        target.FilePath.replace(/[\\/][^\\/]+$/, "") ||
                        target.FilePath;

                      void OpenPath(directory).catch(() => undefined);
                    }}
                  >
                    {t("打开所在目录")}
                  </Button>
                  {(() => {
                    const keyword = encodeURIComponent(
                      searchKeyword(target.FileName),
                    );

                    return keyword ? (
                      <>
                        <button
                          className="flex items-center gap-1 text-xs text-primary hover:underline"
                          onClick={() =>
                            window.open(
                              `https://modrinth.com/mods?q=${keyword}`,
                              "_blank",
                            )
                          }
                        >
                          <Open20Regular className="h-3 w-3" />

                          {t("在 Modrinth 搜索")}
                        </button>
                        <button
                          className="flex items-center gap-1 text-xs text-primary hover:underline"
                          onClick={() =>
                            window.open(
                              `https://www.curseforge.com/minecraft/search?search=${keyword}`,
                              "_blank",
                            )
                          }
                        >
                          <Open20Regular className="h-3 w-3" />

                          {t("在 CurseForge 搜索")}
                        </button>
                      </>
                    ) : null;
                  })()}
                </div>
              </div>
            ) : (
              <>
                <div className="flex flex-wrap items-center justify-between gap-2">
                  <Switch
                    color="primary"
                    isSelected={onlyCompatible}
                    size="sm"
                    onValueChange={setOnlyCompatible}
                  >
                    <span className="text-xs text-gray-500 dark:text-gray-400">
                      {t("仅显示与当前实例兼容的版本")}
                    </span>
                  </Switch>
                  <span className="text-xs text-gray-400">
                    {t("{0} 个版本", { "0": visible.length })}
                  </span>
                </div>

                <div className="nya-scroll nya-scroll-area flex max-h-[45vh] flex-col gap-1.5 pr-1">
                  {visible.map((version) => {
                    const isCurrent =
                      version.versionId === options?.CurrentVersionID;
                    const busy = busyKey === version.versionId;
                    const confirming = confirmKey === version.versionId;
                    const disabled =
                      isCurrent || busy || !version.downloadAllowed;

                    return (
                      <div
                        key={version.versionId}
                        className={`rounded-md border px-3 py-2 ${
                          isCurrent
                            ? "border-primary/40 bg-primary/5"
                            : "nya-border border-transparent bg-default-100/50"
                        }`}
                      >
                        <div className="flex flex-wrap items-center gap-2">
                          <span className="text-sm font-semibold">
                            {version.versionNumber || version.displayName}
                          </span>
                          {version.releaseTypeDisplay ? (
                            <span
                              className={`rounded-full px-2 py-0.5 text-[10px] font-medium ${channelTone(version.releaseType)}`}
                            >
                              {version.releaseTypeDisplay}
                            </span>
                          ) : null}
                          {isCurrent ? (
                            <span className="flex items-center gap-0.5 rounded-full bg-primary/15 px-2 py-0.5 text-[10px] font-medium text-primary">
                              <CheckmarkCircle20Regular className="h-3 w-3" />

                              {t("当前版本")}
                            </span>
                          ) : null}
                          <span className="ml-auto text-[11px] text-gray-400">
                            {version.dateDisplay}
                            {version.fileSizeDisplay
                              ? ` · ${version.fileSizeDisplay}`
                              : ""}
                          </span>
                        </div>
                        <div className="mt-1 flex flex-wrap items-center gap-x-3 gap-y-0.5 text-[11px] text-gray-400">
                          {version.gameVersionsDisplay ? (
                            <span>{version.gameVersionsDisplay}</span>
                          ) : null}
                          {version.loaderDisplay ? (
                            <span>{version.loaderDisplay}</span>
                          ) : null}
                          {version.matchesInstance ? (
                            <span className="text-success-600 dark:text-success-400">
                              {t("兼容当前实例")}
                            </span>
                          ) : version.matchNote ? (
                            <span className="text-warning-600 dark:text-warning-400">
                              {version.matchNote}
                            </span>
                          ) : null}
                        </div>
                        <div className="mt-2 flex flex-wrap items-center justify-end gap-2">
                          {version.summary || version.changelog ? (
                            <span
                              className="mr-auto line-clamp-1 text-[11px] text-gray-400"
                              title={version.changelog || ""}
                            >
                              {version.summary || version.changelog}
                            </span>
                          ) : null}
                          <Button
                            color={confirming ? "danger" : "primary"}
                            isDisabled={disabled}
                            isLoading={busy}
                            radius="full"
                            size="sm"
                            startContent={
                              confirming ? undefined : (
                                <ArrowDownload20Regular />
                              )
                            }
                            variant={confirming ? "solid" : "flat"}
                            onPress={() => void switchTo(version)}
                          >
                            {isCurrent
                              ? t("当前版本")
                              : busy
                                ? progressText || t("下载中…")
                                : confirming
                                  ? t("确认切换？")
                                  : t("切换到此版本")}
                          </Button>
                        </div>
                      </div>
                    );
                  })}
                </div>

                {options?.ProjectPageURL ? (
                  <button
                    className="mt-1 flex items-center justify-center gap-1 text-xs text-primary hover:underline"
                    onClick={() =>
                      window.open(options.ProjectPageURL, "_blank")
                    }
                  >
                    <Open20Regular className="h-3.5 w-3.5" />

                    {options.Source === "curseforge"
                      ? t("在 CurseForge 打开项目主页")
                      : t("在 Modrinth 打开项目主页")}
                  </button>
                ) : null}
              </>
            )}
          </div>
        </ModalShell>
      </ModalContent>
    </Modal>
  );
};

export default ModVersionDialog;
