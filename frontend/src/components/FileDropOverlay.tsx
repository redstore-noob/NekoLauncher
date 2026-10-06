/*
 * 全局文件拖放安装：把文件拖到窗口任意页面即可装进当前实例。
 *
 * 路径来源是 Wails 原生 OnFileDrop（main.go 已开 EnableFileDrop）：WebView2 的
 * HTML5 拖放拿不到磁盘路径（File.path 恒为空），浏览器 dev 环境回落 DOM drop。
 *
 * 路由规则（按扩展名 + zip 内部结构嗅探）：
 * - .mrpack / NekoSolo .exe / CurseForge 整合包 .zip → 下载页整合包导入流程；
 * - .zip：含 shaders/ → shaderpacks；pack.mcmeta / assets/ → resourcepacks；
 *   level.dat → saves；都判不出时弹类别选择；
 * - .jar / .litemod → 当前实例的 mods 目录（可一次拖一批）。
 */
import React, { useCallback, useEffect, useRef, useState } from "react";
import { ArrowDownload20Regular } from "@fluentui/react-icons";
import { Button, Modal, ModalContent } from "@heroui/react";

import {
  ReadModpackRequirements,
  ResolveContentDirectoryForInstance,
} from "../../wailsjs/go/bindings/DownloadAPI";
import { GetCurrentInstanceSnapshot } from "../../wailsjs/go/bindings/InstanceAPI";
import {
  CopyFileIntoDirectory,
  ImportSave,
  SniffZipKind,
} from "../../wailsjs/go/bindings/ContentAPI";
import { ImportSoloExe } from "../../wailsjs/go/bindings/ModpackAPI";
import { OnFileDrop, OnFileDropOff } from "../../wailsjs/runtime/runtime";
import { navigateToPage } from "../lib/navigation";
import { errorMessage } from "../lib/home";
import { t } from "../i18n";

import { ModalShell, modalBehaviorProps } from "./modal-shell";
import { notify } from "./overlay/dialog";

/** Wails WebView 里 File 对象附带的磁盘路径（dev 浏览器环境恒为空） */
function droppedFilePath(file: File): string {
  return (file as File & { path?: string }).path ?? "";
}

/** 去掉路径的目录部分，仅用于提示文案 */
const baseName = (path: string) => path.split(/[\\/]/).pop() ?? path;

const FileDropOverlay: React.FC = () => {
  const [dragging, setDragging] = useState(false);
  // dragenter/dragleave 在子元素间穿梭会成对触发，用计数器抵消抖动
  const depthRef = useRef(0);
  // 无法自动识别类别的 zip：弹选择框（一次一批，取第一个的名字做标题）
  const [choiceZip, setChoiceZip] = useState<string | null>(null);
  const pendingZipsRef = useRef<string[]>([]);

  /** 解析当前实例的内容目录（隔离实例 → versions/<id>/，共享 → .minecraft 根） */
  const resolveContentDirectory = useCallback(async () => {
    const snap = await GetCurrentInstanceSnapshot();

    if (!snap?.MinecraftDirectory) {
      notify.warning(t("还没有可用的游戏实例，先去下载页装一个喵"));

      return null;
    }

    return (
      (await ResolveContentDirectoryForInstance(
        snap.MinecraftDirectory,
        snap.SourcePath,
        snap.SelectedVersionId || snap.VersionIds?.[0] || "",
      )) || snap.MinecraftDirectory
    );
  }, []);

  /** 把一个文件复制进当前实例的内容目录 */
  const copyIntoInstance = useCallback(
    async (path: string, subdirectory: string) => {
      try {
        const base = await resolveContentDirectory();

        if (!base) return;
        const name = await CopyFileIntoDirectory(
          path,
          `${base.replace(/[\\/]+$/, "")}/${subdirectory}`,
        );

        notify.success(t("已把 {0} 装进实例", { "0": name || baseName(path) }));
      } catch (ex) {
        notify.error(errorMessage(ex) || t("安装失败"));
      }
    },
    [resolveContentDirectory],
  );

  /** 走下载页的整合包导入流程（exe 先转存为临时 .mrpack） */
  const importModpack = useCallback(async (path: string) => {
    let packPath = path;

    if (/\.exe$/i.test(path)) {
      try {
        packPath = await ImportSoloExe(path);
      } catch (ex) {
        notify.error(
          t("NekoSolo 安装包读取失败：{0}", { "0": errorMessage(ex) }),
        );

        return;
      }
    }
    navigateToPage("download", `import-modpack:${packPath}`);
  }, []);

  /** 装完内容后重扫实例：instance:changed 会带动实例页刷新内容列表 */
  const refreshInstances = useCallback(async () => {
    try {
      const snap = await GetCurrentInstanceSnapshot();

      if (snap?.MinecraftDirectory) {
        const { RefreshInstances } = await import(
          "../../wailsjs/go/bindings/InstanceAPI"
        );

        await RefreshInstances(snap.MinecraftDirectory);
      }
    } catch {
      /* 刷新失败不影响安装结果提示 */
    }
  }, []);

  /** 处理一批绝对路径（Wails OnFileDrop 通道） */
  const handlePaths = useCallback(
    async (paths: string[]) => {
      if (paths.length === 0) return;

      const packs: string[] = [];
      const mods: string[] = [];
      const saves: string[] = [];
      const unknowns: string[] = [];

      for (const path of paths) {
        const lower = path.toLowerCase();

        if (lower.endsWith(".mrpack") || lower.endsWith(".exe")) {
          packs.push(path);
          continue;
        }
        if (lower.endsWith(".jar") || lower.endsWith(".litemod")) {
          mods.push(path);
          continue;
        }
        if (lower.endsWith(".zip")) {
          // zip 可能是 CurseForge 整合包 / 存档 / 资源包 / 光影：按内容分流
          try {
            await ReadModpackRequirements(path);
            packs.push(path);

            continue;
          } catch {
            /* 不是整合包，继续判 */
          }
          switch (await SniffZipKind(path)) {
            case "save":
              saves.push(path);

              break;
            case "shaderpack":
              await copyIntoInstance(path, "shaderpacks");
              await refreshInstances();

              break;
            case "resourcepack":
              await copyIntoInstance(path, "resourcepacks");
              await refreshInstances();

              break;
            default:
              unknowns.push(path);
          }

          continue;
        }
        notify.warning(
          t("暂不支持这类文件：支持拖入 .mrpack / .zip 整合包与 .jar 模组"),
        );
      }

      if (packs.length > 0) {
        await importModpack(packs[0]);

        return;
      }
      for (const path of mods) await copyIntoInstance(path, "mods");
      if (mods.length > 0) await refreshInstances();
      if (saves.length > 0) {
        try {
          const base = await resolveContentDirectory();

          if (base) {
            for (const path of saves) {
              await ImportSave(path, `${base.replace(/[\\/]+$/, "")}/saves`);
            }
            notify.success(t("存档已导入当前实例"));
            await refreshInstances();
          }
        } catch (ex) {
          notify.error(t("导入存档失败：{0}", { "0": errorMessage(ex) }));
        }
      }
      if (unknowns.length > 0) {
        pendingZipsRef.current = unknowns;
        setChoiceZip(baseName(unknowns[0]));
      }
    },
    [
      copyIntoInstance,
      importModpack,
      refreshInstances,
      resolveContentDirectory,
    ],
  );

  /** 用户给无法识别的 zip 选定类别后落位 */
  const finishChoice = useCallback(
    async (kind: "resourcepacks" | "shaderpacks") => {
      const paths = pendingZipsRef.current;

      setChoiceZip(null);
      pendingZipsRef.current = [];
      for (const path of paths) await copyIntoInstance(path, kind);
      if (paths.length > 0) await refreshInstances();
    },
    [copyIntoInstance, refreshInstances],
  );

  // Wails 原生拖放（生产路径，能拿到绝对路径）；dev 浏览器里 runtime 缺失时静默跳过
  useEffect(() => {
    OnFileDrop((_x, _y, paths) => {
      depthRef.current = 0;
      setDragging(false);
      void handlePaths(paths);
    }, false);

    return () => {
      OnFileDropOff();
    };
  }, [handlePaths]);

  // 视觉层：dragenter/dragleave 计数器（Wails 开启 EnableFileDrop 后 DOM 拖放
  // 事件依旧触发——生产路径本身就依赖 window dragover/drop 转发路径）
  useEffect(() => {
    const hasFiles = (event: DragEvent) =>
      Array.from(event.dataTransfer?.types ?? []).includes("Files");
    const onEnter = (event: DragEvent) => {
      if (!hasFiles(event)) return;
      event.preventDefault();
      depthRef.current += 1;
      setDragging(true);
    };
    const onOver = (event: DragEvent) => {
      if (!hasFiles(event)) return;
      event.preventDefault();
      if (event.dataTransfer) event.dataTransfer.dropEffect = "copy";
    };
    const onLeave = (event: DragEvent) => {
      if (!hasFiles(event)) return;
      depthRef.current = Math.max(0, depthRef.current - 1);

      if (depthRef.current === 0) setDragging(false);
    };
    // dev 浏览器兜底：拿 File.path（不存在则提示改用按钮导入）
    const onDrop = (event: DragEvent) => {
      const files = Array.from(event.dataTransfer?.files ?? []);

      event.preventDefault();
      depthRef.current = 0;
      setDragging(false);
      if (files.length === 0) return;
      if ((window as unknown as { go?: unknown }).go) return; // 生产环境走 OnFileDrop，避免双发
      const paths = files.map(droppedFilePath).filter((p) => p !== "");

      if (paths.length === 0) {
        notify.error(t("拿不到拖入文件的路径，请用「导入文件」按钮"));

        return;
      }
      void handlePaths(paths);
    };

    window.addEventListener("dragenter", onEnter);
    window.addEventListener("dragover", onOver);
    window.addEventListener("dragleave", onLeave);
    window.addEventListener("drop", onDrop);

    return () => {
      window.removeEventListener("dragenter", onEnter);
      window.removeEventListener("dragover", onOver);
      window.removeEventListener("dragleave", onLeave);
      window.removeEventListener("drop", onDrop);
    };
  }, [handlePaths]);

  return (
    <>
      {dragging ? (
        <div className="pointer-events-none fixed inset-0 z-[80] flex items-center justify-center bg-primary/10 backdrop-blur-md">
          <div
            className="
              flex flex-col items-center gap-3 rounded-large border-2 border-dashed
              border-primary/60 bg-black/5 px-16 py-12 dark:bg-white/5
            "
          >
            <span className="flex size-16 items-center justify-center rounded-2xl bg-primary text-primary-foreground shadow-lg shadow-primary/40">
              <ArrowDownload20Regular className="h-8 w-8" />
            </span>
            <span className="text-lg font-semibold text-gray-900 dark:text-gray-100">
              {t("松开即可安装")}
            </span>
            <span className="text-xs text-gray-500 dark:text-gray-400">
              {t(
                "整合包（.mrpack / .zip / .exe）进导入流程，模组 / 资源包 / 光影 / 存档装进当前实例",
              )}
            </span>
          </div>
        </div>
      ) : null}

      {/* 拖入 zip 的类别选择弹层（结构嗅探判不出时） */}
      <Modal
        isOpen={choiceZip !== null}
        size="sm"
        onClose={() => {
          setChoiceZip(null);
          pendingZipsRef.current = [];
        }}
        {...modalBehaviorProps}
      >
        <ModalContent>
          {(onClose) => (
            <ModalShell
              subtitle={choiceZip ?? ""}
              title={t("把拖入的文件装到哪？")}
              onClose={() => {
                onClose();
                setChoiceZip(null);
                pendingZipsRef.current = [];
              }}
            >
              <div className="flex flex-col gap-2">
                <Button
                  variant="flat"
                  onPress={() => void finishChoice("resourcepacks")}
                >
                  {t("资源包")}
                </Button>
                <Button
                  variant="flat"
                  onPress={() => void finishChoice("shaderpacks")}
                >
                  {t("光影包")}
                </Button>
              </div>
            </ModalShell>
          )}
        </ModalContent>
      </Modal>
    </>
  );
};

export default FileDropOverlay;
