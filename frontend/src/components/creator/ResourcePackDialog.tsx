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
 * 资源包制作（创作中心的二级界面）：
 * - 文件树：任意包内文件（PNG / 文本 / 二进制）的新建、导入、搜索与删除；
 * - 导入现有资源包（.zip 或目录），直接二次创作；
 * - 整包模板一键铺开后修改；
 * - PNG 走像素编辑器（画笔 / 图形 / 选区 / 撤销重做 / 镜像）；
 * - 文本 / JSON 走代码编辑器，pack.mcmeta 与 pack.png 有结构化面板；
 * - 导出时经 SystemAPI.ExportResourcePack 原子打包为 .zip。
 */
import type { bindings } from "../../../wailsjs/go/models";
import type { CatalogEntry } from "./resourcepack/catalog";
import type {
  PackFile,
  PackFileKind,
  PackMeta,
  TextureHistory,
} from "./resourcepack/types";
import type { TextureTool } from "./resourcepack/TextureEditor";

import React, {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import {
  Button,
  Dropdown,
  DropdownItem,
  DropdownMenu,
  DropdownSection,
  DropdownTrigger,
  Input,
  Modal,
  ModalContent,
  Select,
  SelectItem,
} from "@heroui/react";
import {
  Add20Regular,
  ArrowImport20Regular,
  Document20Regular,
  FolderZip20Regular,
  Save20Regular,
  Search20Regular,
} from "@fluentui/react-icons";

import { ModalShell, modalBehaviorProps } from "../modal-shell";
import SegmentedTabs from "../segmented-tabs";
import { confirm } from "../overlay/dialog";
import { popoverMotionProps } from "../../lib/motion";
import {
  ClearResourcePackDraft,
  ExportResourcePack,
  ImportResourcePack,
  LoadResourcePackDraft,
  SaveFile,
  SaveResourcePackDraft,
  SelectDirectory,
  SelectFile,
} from "../../../wailsjs/go/bindings/SystemAPI";
import { t } from "../../i18n";

import AssetFinderModal from "./resourcepack/AssetFinderModal";
import PackFileTree from "./resourcepack/PackFileTree";
import PackMetaPanel from "./resourcepack/PackMetaPanel";
import TextFileEditor from "./resourcepack/TextFileEditor";
import TextureEditor from "./resourcepack/TextureEditor";
import { defaultTextForPath } from "./resourcepack/catalog";
import { seedVanillaCanvas } from "./resourcepack/vanilla";
import {
  RESOURCE_PACK_TEMPLATES,
  buildTemplateFiles,
} from "./resourcepack/templates";
import {
  buildPackMetaText,
  canvasFromPngBase64,
  canvasFromUrl,
  canvasToPngDataUri,
  createId,
  DEFAULT_PACK_FORMAT,
  estimateProjectBytes,
  formatBytes,
  makeCanvas,
  normalizePath,
  parsePackMeta,
  TEXTURE_SIZE_OPTIONS,
} from "./resourcepack/types";

function asMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

/** ISO 时间 → 本地 HH:MM:SS（草稿保存时间显示） */
function formatClock(iso: string): string {
  const date = new Date(iso);

  if (Number.isNaN(date.getTime())) return "";

  return date.toLocaleTimeString("zh-CN", { hour12: false });
}

const PNG_PATH_PRESETS = [
  "assets/minecraft/textures/block/stone.png",
  "assets/minecraft/textures/item/diamond_sword.png",
  "assets/minecraft/textures/entity/steve.png",
  "assets/minecraft/textures/gui/sprites/hud/hotbar.png",
  "pack.png",
];

const TEXT_PATH_PRESETS = [
  "assets/minecraft/lang/zh_cn.json",
  "assets/minecraft/models/item/example.json",
  "assets/minecraft/blockstates/example.json",
  "assets/minecraft/sounds.json",
];

/** 生成 pack.png 占位图标：渐变底 + 首字母 */
function paintIcon(canvas: HTMLCanvasElement, letter: string) {
  const ctx = canvas.getContext("2d");

  if (!ctx) return;
  const gradient = ctx.createLinearGradient(0, 0, 0, canvas.height);

  gradient.addColorStop(0, "#38bdf8");
  gradient.addColorStop(1, "#6366f1");
  ctx.globalAlpha = 1;
  ctx.fillStyle = gradient;
  ctx.fillRect(0, 0, canvas.width, canvas.height);
  ctx.fillStyle = "rgba(255,255,255,0.92)";
  ctx.font = `bold ${Math.round(canvas.width * 0.56)}px "Microsoft YaHei", sans-serif`;
  ctx.textAlign = "center";
  ctx.textBaseline = "middle";
  ctx.fillText(letter, canvas.width / 2, canvas.height * 0.54);
}

/** 首次打开时的空白工程 */
function createDefaultFiles(): PackFile[] {
  const meta: PackMeta = {
    description: t("我的资源包"),
    packFormat: DEFAULT_PACK_FORMAT,
    supportedFormats: null,
  };
  const icon = makeCanvas(128, 128);

  paintIcon(icon, "N");

  return [
    {
      id: createId("meta"),
      path: "pack.mcmeta",
      kind: "text",
      text: buildPackMetaText(meta),
    },
    { id: createId("png"), path: "pack.png", kind: "png", canvas: icon },
  ];
}

interface ResourcePackDialogProps {
  isOpen: boolean;
  onClose: () => void;
}

const ResourcePackDialog: React.FC<ResourcePackDialogProps> = ({
  isOpen,
  onClose,
}) => {
  const [files, setFiles] = useState<PackFile[]>([]);
  const [selectedId, setSelectedId] = useState("");
  const [packName, setPackName] = useState("My Resource Pack");
  const [filter, setFilter] = useState("");
  const [sidebarTab, setSidebarTab] = useState<"files" | "meta">("files");

  const [tool, setTool] = useState<TextureTool>("pencil");
  const [color, setColor] = useState("#ef4444");
  const [recentColors, setRecentColors] = useState<string[]>([]);
  const [iconVersion, setIconVersion] = useState(0);

  const [creating, setCreating] = useState<"png" | "text" | null>(null);
  const [finderOpen, setFinderOpen] = useState(false);
  const [newPath, setNewPath] = useState("");
  const [newSize, setNewSize] = useState(16);

  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [status, setStatus] = useState("");
  const [draftState, setDraftState] = useState<
    "idle" | "saving" | "saved" | "error"
  >("idle");
  const [draftSavedAt, setDraftSavedAt] = useState("");

  const historiesRef = useRef<Map<string, TextureHistory>>(new Map());
  const initializedRef = useRef(false);
  const autosaveTimerRef = useRef<number | null>(null);

  const selected = useMemo(
    () => files.find((file) => file.id === selectedId) ?? null,
    [files, selectedId],
  );
  const metaFile = files.find((file) => file.path === "pack.mcmeta") ?? null;
  const meta = useMemo(() => parsePackMeta(metaFile?.text), [metaFile?.text]);
  const packIcon =
    files.find((file) => file.path === "pack.png")?.canvas ?? null;
  const packIconUri = useMemo(() => {
    // iconVersion 变化（生成 / 导入 / 绘制图标）时强制刷新预览
    if (!packIcon || iconVersion < 0) return "";

    return packIcon.toDataURL("image/png");
  }, [packIcon, iconVersion]);
  const textureCount = files.filter((file) => file.kind === "png").length;
  const bytes = useMemo(() => estimateProjectBytes(files), [files]);
  const existingPaths = useMemo(
    () => new Set(files.map((file) => file.path)),
    [files],
  );

  const getHistory = (id: string): TextureHistory => {
    let history = historiesRef.current.get(id);

    if (!history) {
      history = { undo: [], redo: [] };
      historiesRef.current.set(id, history);
    }

    return history;
  };

  const changeColor = (next: string) => {
    setColor(next);
    setRecentColors((prev) =>
      [next, ...prev.filter((item) => item !== next)].slice(0, 6),
    );
  };

  /** 工程 → Wails 文件集（导出 / 草稿共用） */
  const buildPayload = useCallback(
    (): bindings.ResourcePackFile[] =>
      files.map((file) => {
        if (file.kind === "png") {
          return {
            Path: file.path,
            Kind: "png",
            PngBase64: file.canvas ? canvasToPngDataUri(file.canvas) : "",
            Base64: "",
            Text: "",
          };
        }
        if (file.kind === "binary") {
          return {
            Path: file.path,
            Kind: "binary",
            PngBase64: "",
            Base64: file.binaryBase64 ?? "",
            Text: "",
          };
        }

        return {
          Path: file.path,
          Kind: "text",
          PngBase64: "",
          Base64: "",
          Text: file.text ?? "",
        };
      }) as bindings.ResourcePackFile[],
    [files],
  );

  /** 后端文件集 → 可编辑工程（导入 / 草稿恢复共用） */
  const toPackFiles = useCallback(
    async (source: {
      Files: bindings.ResourcePackFile[];
    }): Promise<PackFile[]> => {
      const result: PackFile[] = [];

      for (const entry of source.Files) {
        const kind = (entry.Kind ||
          (entry.PngBase64
            ? "png"
            : entry.Base64
              ? "binary"
              : "text")) as PackFileKind;

        if (kind === "png") {
          const canvas = await canvasFromPngBase64(entry.PngBase64);

          result.push({ id: createId("imp"), path: entry.Path, kind, canvas });
        } else if (kind === "binary") {
          result.push({
            id: createId("imp"),
            path: entry.Path,
            kind,
            binaryBase64: entry.Base64,
          });
        } else {
          result.push({
            id: createId("imp"),
            path: entry.Path,
            kind: "text",
            text: entry.Text,
          });
        }
      }

      return result;
    },
    [],
  );

  /** 防抖自动保存：把当前工程写进后端草稿（崩溃 / 误关后恢复） */
  const persistDraft = useCallback(async () => {
    if (files.length === 0) return;
    setDraftState("saving");
    try {
      const savedAt = new Date().toISOString();

      await SaveResourcePackDraft({
        Found: true,
        Name: packName,
        SavedAt: savedAt,
        Files: buildPayload(),
      } as unknown as bindings.ResourcePackDraft);
      setDraftSavedAt(savedAt);
      setDraftState("saved");
    } catch {
      setDraftState("error");
    }
  }, [buildPayload, files.length, packName]);

  // 首次打开：优先恢复上次草稿，没有才铺一个空白资源包
  useEffect(() => {
    if (!isOpen || initializedRef.current) return;
    initializedRef.current = true;
    void (async () => {
      try {
        const draft = await LoadResourcePackDraft();

        if (draft.Found && draft.Files.length > 0) {
          const converted = await toPackFiles(draft);

          historiesRef.current.clear();
          setFiles(converted);
          if (draft.Name) setPackName(draft.Name);
          setSelectedId(
            converted.find((file) => file.kind === "png")?.id ?? "",
          );
          setStatus(
            t("已恢复上次的资源包草稿（{0} 个文件）", {
              "0": converted.length,
            }),
          );
          setDraftSavedAt(draft.SavedAt);

          return;
        }
      } catch {
        /* 草稿不可用时按空白工程处理 */
      }
      setFiles(createDefaultFiles());
    })();
  }, [isOpen, toPackFiles]);

  /** 防抖调度一次草稿保存（文件结构变化与画布改动共用） */
  const scheduleDraft = useCallback(() => {
    if (autosaveTimerRef.current) window.clearTimeout(autosaveTimerRef.current);
    autosaveTimerRef.current = window.setTimeout(() => {
      void persistDraft();
    }, 1500);
  }, [persistDraft]);

  // 文件 / 包名变化后自动保存（防抖；拖拽 / 连续输入不会频繁写盘）
  useEffect(() => {
    if (!isOpen || !initializedRef.current || files.length === 0) return;
    scheduleDraft();

    return () => {
      if (autosaveTimerRef.current) {
        window.clearTimeout(autosaveTimerRef.current);
        autosaveTimerRef.current = null;
      }
    };
  }, [files, isOpen, scheduleDraft]);

  useEffect(() => {
    if (!isOpen) return;
    setError("");
    setStatus("");
  }, [isOpen]);

  const setFilePath = (id: string, raw: string) => {
    const path = normalizePath(raw);

    setFiles((prev) =>
      prev.map((file) => (file.id === id ? { ...file, path } : file)),
    );
  };

  const updateText = (id: string, text: string) => {
    setFiles((prev) =>
      prev.map((file) => (file.id === id ? { ...file, text } : file)),
    );
  };

  const writeMeta = (next: PackMeta) => {
    const text = buildPackMetaText(next);

    setFiles((prev) => {
      const index = prev.findIndex((file) => file.path === "pack.mcmeta");

      if (index >= 0) {
        const copy = [...prev];

        copy[index] = { ...copy[index], kind: "text", text, canvas: undefined };

        return copy;
      }

      return [
        ...prev,
        { id: createId("meta"), path: "pack.mcmeta", kind: "text", text },
      ];
    });
  };

  const pathError = (path: string, excludeId: string): string => {
    if (!path) return t("请填写包内路径");
    if (files.some((file) => file.path === path && file.id !== excludeId)) {
      return t("已存在相同路径的文件");
    }

    return "";
  };

  const createPng = () => {
    const path = normalizePath(newPath);

    if (!path.toLowerCase().endsWith(".png")) {
      setError(t("贴图路径需要以 .png 结尾"));

      return;
    }
    const invalid = pathError(path, "");

    if (invalid) {
      setError(invalid);

      return;
    }
    // 新建贴图不再给空白画布：按路径铺一层原版风格模板（如石头噪点），直接改
    const canvas = seedVanillaCanvas(path, newSize);
    const id = createId("png");

    setFiles((prev) => [...prev, { id, path, kind: "png", canvas }]);
    setSelectedId(id);
    setCreating(null);
    setNewPath("");
    setError("");
  };

  const createText = () => {
    const path = normalizePath(newPath);
    const invalid = pathError(path, "");

    if (invalid) {
      setError(invalid);

      return;
    }
    const id = createId("txt");

    setFiles((prev) => [
      ...prev,
      {
        id,
        path,
        kind: "text",
        text: defaultTextForPath(path),
      },
    ]);
    setSelectedId(id);
    setCreating(null);
    setNewPath("");
    setError("");
  };

  const importPng = async () => {
    try {
      setError("");
      const path = await SelectFile(t("选择 PNG 贴图"), t("PNG 图片"), "*.png");

      if (!path) return;
      const canvas = await canvasFromUrl(
        `/localfile?path=${encodeURIComponent(path)}`,
      );

      if (canvas.width > 512 || canvas.height > 512) {
        throw new Error(t("贴图尺寸过大（上限 512×512）"));
      }
      const name = path.replace(/\\/g, "/").split("/").pop() || "texture.png";
      let target = `assets/minecraft/textures/${name}`;

      if (files.some((file) => file.path === target)) {
        target = `assets/minecraft/textures/${Date.now() % 1000}-${name}`;
      }
      const id = createId("png");

      setFiles((prev) => [...prev, { id, path: target, kind: "png", canvas }]);
      setSelectedId(id);
    } catch (err) {
      setError(asMessage(err));
    }
  };

  const loadSource = async (path: string, isDirectory: boolean) => {
    if (!path) return;
    setBusy(true);
    setError("");
    setStatus("");
    try {
      const project = await ImportResourcePack(path);
      const converted = await toPackFiles(project);

      historiesRef.current.clear();
      setFiles(converted);
      setPackName(project.Name || "My Resource Pack");
      setSelectedId(converted.find((file) => file.kind === "png")?.id ?? "");
      setStatus(
        t("已导入 {0} 个文件{1}", {
          "0": converted.length,
          "1": isDirectory ? "（文件夹）" : "",
        }),
      );
    } catch (err) {
      setError(asMessage(err));
    } finally {
      setBusy(false);
    }
  };

  const importZip = async () => {
    const path = await SelectFile(t("导入资源包"), t("资源包 (.zip)"), "*.zip");

    await loadSource(path, false);
  };

  const importFolder = async () => {
    const path = await SelectDirectory(t("选择资源包文件夹"));

    await loadSource(path, true);
  };

  const applyTemplate = async (templateId: string) => {
    const template = RESOURCE_PACK_TEMPLATES.find(
      (item) => item.id === templateId,
    );

    if (!template) return;
    if (files.length > 0) {
      const ok = await confirm(
        t("套用模板"),
        t("当前工程的内容会被模板替换，确定继续吗？"),
        { confirmLabel: t("替换") },
      );

      if (!ok) return;
    }
    const next = buildTemplateFiles(template);

    historiesRef.current.clear();
    setFiles(next);
    setPackName(template.name);
    setSelectedId(next.find((file) => file.kind === "png")?.id ?? "");
    setStatus(t("已套用模板：{0}", { "0": template.name }));
    setError("");
  };

  const ensurePackIcon = (): HTMLCanvasElement => {
    const existing = files.find((file) => file.path === "pack.png")?.canvas;

    if (existing) return existing;
    const canvas = makeCanvas(128, 128);

    paintIcon(canvas, packName.trim().charAt(0) || "N");

    return canvas;
  };

  const commitPackIcon = (canvas: HTMLCanvasElement) => {
    setFiles((prev) => {
      const index = prev.findIndex((file) => file.path === "pack.png");

      if (index >= 0) {
        const copy = [...prev];

        copy[index] = { ...copy[index], kind: "png", canvas, text: undefined };

        return copy;
      }

      return [
        ...prev,
        { id: createId("png"), path: "pack.png", kind: "png", canvas },
      ];
    });
    setIconVersion((value) => value + 1);
  };

  const generateIcon = () => {
    const canvas = ensurePackIcon();

    paintIcon(canvas, packName.trim().charAt(0) || "N");
    commitPackIcon(canvas);
  };

  const importIcon = async () => {
    try {
      setError("");
      const path = await SelectFile(t("选择 pack.png"), t("PNG 图片"), "*.png");

      if (!path) return;
      const source = await canvasFromUrl(
        `/localfile?path=${encodeURIComponent(path)}`,
      );
      const canvas = makeCanvas(128, 128);

      canvas.getContext("2d")?.drawImage(source, 0, 0, 128, 128);
      commitPackIcon(canvas);
    } catch (err) {
      setError(asMessage(err));
    }
  };

  const openIcon = () => {
    const icon = files.find((file) => file.path === "pack.png");

    if (icon) setSelectedId(icon.id);
  };

  const deleteFile = (id: string) => {
    historiesRef.current.delete(id);
    setFiles((prev) => prev.filter((file) => file.id !== id));
    if (selectedId === id) setSelectedId("");
    // 删掉冲突文件后错误提示要跟着消失，否则「导出 .zip」会一直灰着
    setError("");
  };

  /** 重置为空工程并删除已保存的草稿 */
  const resetProject = async () => {
    const ok = await confirm(
      t("重置工程"),
      t("会清空当前所有文件并删除已自动保存的草稿，确定继续吗？"),
      { confirmLabel: t("重置") },
    );

    if (!ok) return;
    historiesRef.current.clear();
    try {
      await ClearResourcePackDraft();
    } catch {
      /* 草稿删除失败不影响重置 */
    }
    setFiles(createDefaultFiles());
    setPackName("My Resource Pack");
    setSelectedId("");
    setDraftSavedAt("");
    setDraftState("idle");
    setStatus(t("已重置为空白工程"));
    setError("");
  };

  /** 关闭前先落一次草稿，避免防抖窗口内的改动丢失 */
  const handleClose = () => {
    if (autosaveTimerRef.current) {
      window.clearTimeout(autosaveTimerRef.current);
      autosaveTimerRef.current = null;
    }
    void persistDraft();
    onClose();
  };

  /** 资源库点击：已存在则直接选中，否则按类型创建并在编辑器打开 */
  const openCatalogEntry = (entry: CatalogEntry) => {
    const existing = files.find((file) => file.path === entry.path);

    if (existing) {
      setSelectedId(existing.id);
      setFinderOpen(false);
      setError("");

      return;
    }

    const id = createId(entry.kind === "png" ? "png" : "txt");

    if (entry.kind === "png") {
      // 资源库创建同样落到原版风格模板上，用户在成品底图上二次创作
      const canvas = seedVanillaCanvas(entry.path, entry.size ?? 16);

      setFiles((prev) => [
        ...prev,
        { id, path: entry.path, kind: "png", canvas },
      ]);
    } else {
      setFiles((prev) => [
        ...prev,
        {
          id,
          path: entry.path,
          kind: "text",
          text: defaultTextForPath(entry.path),
        },
      ]);
    }
    setSelectedId(id);
    setFinderOpen(false);
    setStatus(t("已创建 {0}", { "0": entry.path }));
    setError("");
  };

  const exportPack = async () => {
    if (files.length === 0) {
      setError(t("资源包里还没有任何文件"));

      return;
    }
    setBusy(true);
    setError("");
    setStatus("");
    try {
      const payload = buildPayload();
      const target = await SaveFile(
        t("导出资源包"),
        `${packName.trim() || "resourcepack"}.zip`,
        t("资源包"),
        "*.zip",
      );

      if (!target) return; // 用户取消
      const output = await ExportResourcePack(target, payload);

      setStatus(t("已导出：{0}", { "0": output }));
    } catch (err) {
      setError(asMessage(err));
    } finally {
      setBusy(false);
    }
  };

  /** 新建表单（贴图 / 文本共用路径输入） */
  const renderCreateForm = () => {
    const isPng = creating === "png";

    return (
      <div className="flex flex-col gap-1.5 rounded-lg bg-default-100/70 p-2">
        <Input
          aria-label={t("新文件路径")}
          classNames={{ inputWrapper: "h-8" }}
          placeholder={
            isPng
              ? "assets/minecraft/textures/…png"
              : "assets/minecraft/lang/…json"
          }
          size="sm"
          value={newPath}
          onValueChange={setNewPath}
        />
        <div className="flex items-center gap-1">
          {isPng ? (
            <Select
              aria-label={t("贴图尺寸")}
              className="flex-1 [&_*]:min-w-0"
              classNames={{ trigger: "h-8 min-h-8" }}
              popoverProps={{ motionProps: popoverMotionProps }}
              selectedKeys={[String(newSize)]}
              size="sm"
              onSelectionChange={(keys) =>
                setNewSize(Number(Array.from(keys)[0] ?? 16))
              }
            >
              {TEXTURE_SIZE_OPTIONS.map((size) => (
                <SelectItem key={String(size)}>{`${size}²`}</SelectItem>
              ))}
            </Select>
          ) : (
            <span className="flex-1 text-[11px] text-gray-400">
              {t("文本文件（JSON / mcmeta / lang…）")}
            </span>
          )}
          <Button
            color="primary"
            size="sm"
            variant="flat"
            onPress={isPng ? createPng : createText}
          >
            {t("创建")}
          </Button>
        </div>
        <div className="flex flex-wrap gap-1">
          {(isPng ? PNG_PATH_PRESETS : TEXT_PATH_PRESETS).map((preset) => (
            <button
              key={preset}
              className="max-w-full truncate rounded-full bg-default-200/70 px-2 py-0.5 text-[10px] text-gray-600 transition-colors hover:bg-primary/20 hover:text-primary dark:text-gray-300"
              type="button"
              onClick={() => setNewPath(preset)}
            >
              {preset.replace("assets/minecraft/", "…/")}
            </button>
          ))}
        </div>
      </div>
    );
  };

  return (
    <>
      <Modal
        isOpen={isOpen}
        size="5xl"
        onClose={handleClose}
        {...modalBehaviorProps}
      >
        <ModalContent className="h-[85vh] max-h-[85vh]">
          <ModalShell
            icon={<FolderZip20Regular />}
            title={t("资源包制作")}
            onClose={handleClose}
          >
            <div className="flex h-full min-h-0 flex-col gap-2.5">
              {/* 顶栏：新建 / 导入 / 资源库（紧凑） */}
              <div className="flex flex-shrink-0 flex-wrap items-center gap-2">
                <Dropdown>
                  <DropdownTrigger>
                    <Button
                      size="sm"
                      startContent={<Add20Regular />}
                      variant="flat"
                    >
                      {t("添加")}
                    </Button>
                  </DropdownTrigger>
                  <DropdownMenu
                    aria-label={t("添加文件")}
                    onAction={(key) => {
                      const action = String(key);

                      if (action === "reset") {
                        void resetProject();

                        return;
                      }
                      if (action.startsWith("tpl:")) {
                        void applyTemplate(action.slice(4));

                        return;
                      }
                      setCreating(action as "png" | "text");
                      setNewPath("");
                      setError("");
                    }}
                  >
                    <DropdownSection title={t("新建")}>
                      <DropdownItem
                        key="png"
                        description={t("绘制或导入一张贴图")}
                      >
                        {t("贴图 PNG")}
                      </DropdownItem>
                      <DropdownItem
                        key="text"
                        description={t("模型 / lang / 清单等文本")}
                      >
                        {t("文本文件")}
                      </DropdownItem>
                    </DropdownSection>
                    <DropdownSection title={t("模板")}>
                      {RESOURCE_PACK_TEMPLATES.map((template) => (
                        <DropdownItem
                          key={`tpl:${template.id}`}
                          description={template.description}
                        >
                          {template.name}
                        </DropdownItem>
                      ))}
                    </DropdownSection>
                    <DropdownSection title={t("其它")}>
                      <DropdownItem
                        key="reset"
                        description={t("清空工程并删除已保存的草稿")}
                      >
                        {t("重置工程")}
                      </DropdownItem>
                    </DropdownSection>
                  </DropdownMenu>
                </Dropdown>

                <Dropdown>
                  <DropdownTrigger>
                    <Button
                      isDisabled={busy}
                      size="sm"
                      startContent={<ArrowImport20Regular />}
                      variant="flat"
                    >
                      {t("导入")}
                    </Button>
                  </DropdownTrigger>
                  <DropdownMenu
                    aria-label={t("导入资源包")}
                    onAction={(key) => {
                      if (key === "zip") void importZip();
                      else if (key === "png") void importPng();
                      else void importFolder();
                    }}
                  >
                    <DropdownItem key="zip" description={t("选择 .zip 资源包")}>
                      {t("从 .zip 导入")}
                    </DropdownItem>
                    <DropdownItem
                      key="folder"
                      description={t("选择已解压的资源包目录")}
                    >
                      {t("从文件夹导入")}
                    </DropdownItem>
                    <DropdownItem
                      key="png"
                      description={t("仅追加一张本地贴图")}
                    >
                      {t("导入单张 PNG")}
                    </DropdownItem>
                  </DropdownMenu>
                </Dropdown>

                <Button
                  size="sm"
                  startContent={<Search20Regular />}
                  variant="flat"
                  onPress={() => setFinderOpen(true)}
                >
                  {t("资源库")}
                </Button>
              </div>

              <div className="flex min-h-0 flex-1 gap-2.5">
                {/* 左：文件 / 包信息（标签切换） */}
                <div className="flex w-64 flex-shrink-0 flex-col rounded-xl nya-panel-inner p-2">
                  <SegmentedTabs
                    className="mb-2 flex gap-0.5 rounded-full bg-default-100/70 p-0.5"
                    itemClassName="flex-1 px-2 py-1 text-[12px]"
                    items={[
                      {
                        key: "files",
                        label: t("文件 {0}", { "0": files.length }),
                      },
                      { key: "meta", label: t("包信息") },
                    ]}
                    layoutId="respack-sidebar"
                    value={sidebarTab}
                    onChange={(value) =>
                      setSidebarTab(value as "files" | "meta")
                    }
                  />
                  {sidebarTab === "files" ? (
                    <div className="flex min-h-0 flex-1 flex-col gap-2">
                      <Input
                        aria-label={t("搜索文件")}
                        classNames={{ inputWrapper: "h-8" }}
                        placeholder={t("搜索文件")}
                        size="sm"
                        startContent={
                          <Search20Regular className="text-gray-400" />
                        }
                        value={filter}
                        onValueChange={setFilter}
                      />
                      {creating ? renderCreateForm() : null}
                      <div className="min-h-0 flex-1 overflow-y-auto">
                        <PackFileTree
                          files={files}
                          filter={filter}
                          selectedId={selectedId}
                          onDelete={deleteFile}
                          onSelect={setSelectedId}
                        />
                      </div>
                    </div>
                  ) : (
                    <div className="min-h-0 flex-1 overflow-y-auto pt-1">
                      <PackMetaPanel
                        fileCount={files.length}
                        hasIcon={!!packIcon}
                        iconUri={packIconUri}
                        meta={meta}
                        packName={packName}
                        textureCount={textureCount}
                        onGenerateIcon={generateIcon}
                        onImportIcon={() => void importIcon()}
                        onMetaChange={writeMeta}
                        onOpenIcon={openIcon}
                        onPackNameChange={setPackName}
                      />
                    </div>
                  )}
                </div>

                {/* 中：编辑器 */}
                <div className="flex min-w-0 flex-1 flex-col rounded-xl nya-panel-inner">
                  {selected ? (
                    <>
                      <div className="flex items-center gap-2 px-3 pt-3">
                        <Input
                          aria-label={t("文件路径")}
                          classNames={{ inputWrapper: "h-8" }}
                          placeholder={t("包内路径")}
                          size="sm"
                          value={selected.path}
                          onValueChange={(value) => {
                            setFilePath(selected.id, value);
                            const invalid = pathError(
                              normalizePath(value),
                              selected.id,
                            );

                            setError(invalid);
                          }}
                        />
                        <span className="flex-none text-[11px] text-gray-400">
                          {selected.kind === "png"
                            ? selected.canvas
                              ? `${selected.canvas.width}×${selected.canvas.height}`
                              : "PNG"
                            : selected.kind === "text"
                              ? t("文本")
                              : t("二进制")}
                        </span>
                      </div>
                      {selected.kind === "png" ? (
                        <TextureEditor
                          key={selected.id}
                          color={color}
                          file={selected}
                          history={getHistory(selected.id)}
                          recentColors={recentColors}
                          tool={tool}
                          onColorChange={changeColor}
                          onContentChange={scheduleDraft}
                          onToolChange={setTool}
                        />
                      ) : selected.kind === "text" ? (
                        <TextFileEditor
                          file={selected}
                          onChange={(text) => updateText(selected.id, text)}
                        />
                      ) : (
                        <div className="flex flex-1 flex-col items-center justify-center gap-2 text-gray-400">
                          <Document20Regular className="text-3xl" />
                          <span className="text-sm">
                            {t("二进制文件（导出时原样保留）")}
                          </span>
                          <span className="text-[11px]">
                            {t("约")}{" "}
                            {formatBytes(
                              Math.floor(
                                ((selected.binaryBase64?.length ?? 0) * 3) / 4,
                              ),
                            )}
                          </span>
                        </div>
                      )}
                    </>
                  ) : (
                    <div className="flex flex-1 flex-col items-center justify-center gap-2 text-gray-400">
                      <span className="text-4xl">▦</span>
                      <span className="text-sm">{t("未选择文件")}</span>
                    </div>
                  )}
                </div>
              </div>

              {/* 底部状态行 */}
              <div className="flex flex-shrink-0 items-center gap-3">
                <span className="min-w-0 flex-1 truncate text-[11px] text-gray-400">
                  {error ? (
                    <span className="text-danger">{error}</span>
                  ) : status ? (
                    <span className="text-success">{status}</span>
                  ) : (
                    t("估算体积约 {0} · {1}", {
                      "0": formatBytes(bytes),
                      "1":
                        draftState === "saving"
                          ? "自动保存中…"
                          : draftState === "error"
                            ? "自动保存失败（可直接导出）"
                            : draftSavedAt
                              ? `草稿已保存 ${formatClock(draftSavedAt)}`
                              : "已开启自动保存",
                    })
                  )}
                </span>
                <Button
                  color="primary"
                  isDisabled={files.length === 0 || !!error}
                  isLoading={busy}
                  radius="full"
                  size="sm"
                  startContent={<Save20Regular />}
                  onPress={() => void exportPack()}
                >
                  {t("导出 .zip")}
                </Button>
              </div>
            </div>
          </ModalShell>
        </ModalContent>
      </Modal>

      <AssetFinderModal
        existingPaths={existingPaths}
        isOpen={finderOpen}
        onClose={() => setFinderOpen(false)}
        onPick={openCatalogEntry}
      />
    </>
  );
};

export default ResourcePackDialog;
