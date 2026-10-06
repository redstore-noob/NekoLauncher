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
 * 包信息面板：编辑 pack.mcmeta 的结构化字段（描述 / pack_format /
 * supported_formats）与资源包列表图标 pack.png。分区折叠，保持侧栏清爽。
 */
import type { PackMeta } from "./types";

import React, { useEffect, useMemo, useState } from "react";
import { Button, Input, Select, SelectItem, Switch } from "@heroui/react";
import {
  ArrowImport20Regular,
  ChevronDown20Regular,
  ChevronRight20Regular,
  Edit20Regular,
  Sparkle20Regular,
} from "@fluentui/react-icons";

import { selectPopoverProps } from "../../../lib/motion";
import { t } from "../../../i18n";

import { PACK_FORMAT_PRESETS } from "./types";

interface PackMetaPanelProps {
  packName: string;
  meta: PackMeta;
  /** pack.png 预览 data URI（无图标时为空串） */
  iconUri: string;
  hasIcon: boolean;
  fileCount: number;
  textureCount: number;
  onPackNameChange: (name: string) => void;
  onMetaChange: (meta: PackMeta) => void;
  onGenerateIcon: () => void;
  onImportIcon: () => void;
  onOpenIcon: () => void;
}

/** 可折叠分区：标题行 + 内容，默认展开 */
const MetaSection: React.FC<{
  title: string;
  defaultOpen?: boolean;
  children: React.ReactNode;
}> = ({ title, defaultOpen = true, children }) => {
  const [open, setOpen] = useState(defaultOpen);

  return (
    <div className="nya-border overflow-hidden rounded-lg border">
      <button
        className="flex w-full cursor-pointer items-center justify-between px-2.5 py-1.5 text-[12px] font-medium text-gray-600 transition-colors hover:bg-default-100 dark:text-gray-300 dark:hover:bg-gray-800"
        type="button"
        onClick={() => setOpen((value) => !value)}
      >
        {title}
        {open ? (
          <ChevronDown20Regular className="text-gray-400" />
        ) : (
          <ChevronRight20Regular className="text-gray-400" />
        )}
      </button>
      {open ? (
        <div className="flex flex-col gap-2 px-2.5 pb-2.5 pt-0.5">
          {children}
        </div>
      ) : null}
    </div>
  );
};

const PackMetaPanel: React.FC<PackMetaPanelProps> = ({
  packName,
  meta,
  iconUri,
  hasIcon,
  fileCount,
  textureCount,
  onPackNameChange,
  onMetaChange,
  onGenerateIcon,
  onImportIcon,
  onOpenIcon,
}) => {
  const presetKey =
    PACK_FORMAT_PRESETS.find((preset) => preset.format === meta.packFormat)
      ?.format ?? "custom";

  const patch = (partial: Partial<PackMeta>) =>
    onMetaChange({ ...meta, ...partial });

  // pack_format 输入框保留原始字符串：直接写回 number 的话，清空输入框会被
  // 立刻改写成 "1"（NaN || 1），后面敲的数字接在后面 —— 想输 46 会变成 146。
  const [formatText, setFormatText] = useState(String(meta.packFormat));

  // 缓存 selectedKeys 数组，避免每次渲染都创建新数组导致 Select 闪烁
  const presetKeys = useMemo(
    () => (presetKey === "custom" ? [] : [String(presetKey)]),
    [presetKey],
  );

  useEffect(() => {
    setFormatText(String(meta.packFormat));
  }, [meta.packFormat]);

  const onFormatChange = (value: string) => {
    setFormatText(value);
    const parsed = parseInt(value, 10);

    if (Number.isFinite(parsed) && parsed > 0) patch({ packFormat: parsed });
  };

  const field = (label: string, node: React.ReactNode, hint?: string) => (
    <div className="flex flex-col gap-1">
      <span className="text-[11px] text-gray-500 dark:text-gray-400">
        {label}
      </span>
      {node}
      {hint ? <span className="text-[10px] text-gray-400">{hint}</span> : null}
    </div>
  );

  return (
    <div className="flex flex-col gap-2">
      <MetaSection title={t("基本信息")}>
        {field(
          t("名称（导出文件名）"),
          <Input
            aria-label={t("资源包名称")}
            classNames={{ inputWrapper: "h-8" }}
            placeholder="My Resource Pack"
            size="sm"
            value={packName}
            onValueChange={onPackNameChange}
          />,
        )}
        {field(
          t("描述"),
          <Input
            aria-label={t("资源包描述")}
            classNames={{ inputWrapper: "h-8" }}
            placeholder={t("显示在资源包列表")}
            size="sm"
            value={meta.description}
            onValueChange={(value) => patch({ description: value })}
          />,
        )}
      </MetaSection>

      <MetaSection title={t("版本")}>
        <div className="flex items-end gap-2">
          <div className="min-w-0 flex-1">
            {field(
              t("目标版本"),
              <Select
                aria-label={t("目标 Minecraft 版本")}
                className="[&_*]:min-w-0"
                classNames={{ trigger: "h-8 min-h-8" }}
                popoverProps={selectPopoverProps}
                selectedKeys={presetKeys}
                size="sm"
                onSelectionChange={(keys) => {
                  const key = String(Array.from(keys)[0] ?? "");
                  const preset = PACK_FORMAT_PRESETS.find(
                    (item) => String(item.format) === key,
                  );

                  if (preset) patch({ packFormat: preset.format });
                }}
              >
                {PACK_FORMAT_PRESETS.map((preset) => (
                  <SelectItem key={String(preset.format)}>
                    {`${preset.label}（${preset.format}）`}
                  </SelectItem>
                ))}
              </Select>,
            )}
          </div>
          <div className="w-16 flex-none">
            {field(
              "format",
              <Input
                aria-label="pack_format"
                classNames={{ inputWrapper: "h-8" }}
                max={999}
                min={1}
                size="sm"
                type="number"
                value={formatText}
                onBlur={() => {
                  // 失焦时把非法/空输入收回真实值，避免输入框与 meta 长期不一致
                  if (parseInt(formatText, 10) !== meta.packFormat) {
                    setFormatText(String(meta.packFormat));
                  }
                }}
                onValueChange={onFormatChange}
              />,
            )}
          </div>
        </div>

        <div className="flex flex-col gap-1.5">
          <span className="flex items-center justify-between text-[11px] text-gray-500 dark:text-gray-400">
            {t("版本区间")}
            <Switch
              aria-label={t("启用版本区间")}
              color="primary"
              isSelected={meta.supportedFormats !== null}
              size="sm"
              onValueChange={(enabled) =>
                patch({
                  supportedFormats: enabled
                    ? {
                        min: Math.max(1, meta.packFormat - 2),
                        max: meta.packFormat,
                      }
                    : null,
                })
              }
            />
          </span>
          {meta.supportedFormats ? (
            <div className="flex items-center gap-2">
              <Input
                aria-label={t("最低版本格式")}
                classNames={{ inputWrapper: "h-8" }}
                max={999}
                min={1}
                size="sm"
                type="number"
                value={String(meta.supportedFormats.min)}
                onValueChange={(value) =>
                  patch({
                    supportedFormats: {
                      min: parseInt(value, 10) || 1,
                      max: meta.supportedFormats?.max ?? meta.packFormat,
                    },
                  })
                }
              />
              <span className="text-[11px] text-gray-400">{t("至")}</span>
              <Input
                aria-label={t("最高版本格式")}
                classNames={{ inputWrapper: "h-8" }}
                max={999}
                min={1}
                size="sm"
                type="number"
                value={String(meta.supportedFormats.max)}
                onValueChange={(value) =>
                  patch({
                    supportedFormats: {
                      min: meta.supportedFormats?.min ?? meta.packFormat,
                      max: parseInt(value, 10) || meta.packFormat,
                    },
                  })
                }
              />
            </div>
          ) : (
            <span className="text-[10px] text-gray-400">
              {t("1.20.2+ 可用，让同一包覆盖多个版本")}
            </span>
          )}
        </div>
      </MetaSection>

      <MetaSection title={t("图标 pack.png")}>
        <div className="flex items-center gap-2.5">
          {iconUri ? (
            <img
              alt={t("资源包图标")}
              className="size-12 flex-none rounded-md [image-rendering:pixelated]"
              src={iconUri}
            />
          ) : (
            <div className="flex size-12 flex-none items-center justify-center rounded-md bg-default-100 text-[10px] text-gray-400">
              {t("无")}
            </div>
          )}
          <div className="flex min-w-0 flex-1 flex-wrap gap-1">
            <Button
              size="sm"
              startContent={<Sparkle20Regular />}
              variant="flat"
              onPress={onGenerateIcon}
            >
              {t("生成")}
            </Button>
            <Button
              size="sm"
              startContent={<ArrowImport20Regular />}
              variant="light"
              onPress={onImportIcon}
            >
              {t("导入")}
            </Button>
            <Button
              isDisabled={!hasIcon}
              size="sm"
              startContent={<Edit20Regular />}
              variant="light"
              onPress={onOpenIcon}
            >
              {t("绘制")}
            </Button>
          </div>
        </div>
        <span className="text-[10px] text-gray-400">{t("建议 128×128")}</span>
      </MetaSection>

      <div className="px-0.5 text-[11px] text-gray-400">
        {t("共")} {fileCount} {t("个文件 ·")} {textureCount} {t("张贴图")}
      </div>
    </div>
  );
};

export default PackMetaPanel;
