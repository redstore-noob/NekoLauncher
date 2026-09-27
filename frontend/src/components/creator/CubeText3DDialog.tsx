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
 * 3D 体素文字生成器（创作中心的二级界面）：文本 → 方块文字，实时 3D 预览、
 * 拖拽旋转 / 滚轮缩放，可选题材色、渐变 / 彩虹、程序化纹理、描边与透明背景，
 * 一键截图导出 PNG。
 *
 * 灵感与功能对齐自 EaseCation/cube-3d-text（MIT）。Three.js 预览经 React.lazy
 * 动态加载，保持主包体积。
 */
import type {
  CubeText3DHandle,
  CubeTextConfig,
} from "./cube/CubeText3DPreview";

import React, { Suspense, lazy, useRef, useState } from "react";
import {
  Button,
  Modal,
  ModalContent,
  Select,
  SelectItem,
  Slider,
  Switch,
  Textarea,
} from "@heroui/react";
import {
  ArrowClockwise20Regular,
  BoxMultiple20Regular,
  Save20Regular,
} from "@fluentui/react-icons";

import { ModalShell, modalBehaviorProps } from "../modal-shell";
import { notify } from "../overlay/dialog";
import { popoverMotionProps } from "../../lib/motion";
import { SaveFile, WritePngFile } from "../../../wailsjs/go/bindings/SystemAPI";
import { t } from "../../i18n";

const CubeText3DPreview = lazy(() => import("./cube/CubeText3DPreview"));

interface CubeText3DDialogProps {
  isOpen: boolean;
  onClose: () => void;
}

const FONT_OPTIONS = [
  { value: '"Microsoft YaHei", sans-serif', label: t("微软雅黑") },
  { value: '"SimSun", serif', label: t("宋体") },
  { value: '"KaiTi", serif', label: t("楷体") },
  { value: "sans-serif", label: t("无衬线") },
  { value: "serif", label: t("衬线") },
  { value: "monospace", label: t("等宽") },
];

const COLOR_MODES: Array<{
  value: CubeTextConfig["colorMode"];
  label: string;
}> = [
  { value: "gradient", label: t("上下渐变") },
  { value: "solid", label: t("纯色") },
  { value: "rainbow", label: t("彩虹") },
];

const TEXTURES: Array<{ value: CubeTextConfig["texture"]; label: string }> = [
  { value: "grid", label: t("方块边框") },
  { value: "noise", label: t("噪点石纹") },
  { value: "none", label: t("无纹理") },
];

const DEFAULT_CONFIG: CubeTextConfig = {
  text: "Neko\nLauncher",
  font: '"Microsoft YaHei", sans-serif',
  bold: true,
  resolution: 5,
  depth: 1,
  spacing: 0.06,
  colorMode: "gradient",
  colorA: "#38BDF8",
  colorB: "#A855F7",
  texture: "grid",
  outline: true,
  outlineColor: "#0F172A",
  background: "#0B1220",
};

const CubeText3DDialog: React.FC<CubeText3DDialogProps> = ({
  isOpen,
  onClose,
}) => {
  const [config, setConfig] = useState<CubeTextConfig>(DEFAULT_CONFIG);
  const [autoRotate, setAutoRotate] = useState(true);
  const [exporting, setExporting] = useState(false);
  const previewRef = useRef<CubeText3DHandle>(null);

  const patch = (partial: Partial<CubeTextConfig>) =>
    setConfig((prev) => ({ ...prev, ...partial }));

  const exportPng = async () => {
    const dataUri = previewRef.current?.screenshot();

    if (!dataUri) {
      notify.error(t("预览尚未就绪"));

      return;
    }
    setExporting(true);
    try {
      const target = await SaveFile(
        t("导出 3D 文字"),
        "cube-3d-text.png",
        t("PNG 图片"),
        "*.png",
      );

      if (!target) return;
      await WritePngFile(target, dataUri);
      notify.success(t("已导出 PNG"));
    } catch (error) {
      notify.error(error instanceof Error ? error.message : String(error));
    } finally {
      setExporting(false);
    }
  };

  const field = (label: string, node: React.ReactNode) => (
    <div className="flex flex-col gap-1">
      <span className="text-[12px] text-gray-600 dark:text-gray-300">
        {label}
      </span>
      {node}
    </div>
  );

  return (
    <Modal isOpen={isOpen} size="5xl" onClose={onClose} {...modalBehaviorProps}>
      <ModalContent className="h-[85vh] max-h-[85vh]">
        <ModalShell
          icon={<BoxMultiple20Regular />}
          title={t("3D 文字生成器")}
          onClose={onClose}
        >
          <div className="flex h-full min-h-0 gap-4">
            {/* 左：参数 */}
            <div className="flex w-[330px] flex-none flex-col gap-3 overflow-y-auto pr-1">
              {field(
                t("文本（支持换行）"),
                <Textarea
                  aria-label={t("文本")}
                  classNames={{ input: "min-h-16" }}
                  size="sm"
                  value={config.text}
                  onValueChange={(value) => patch({ text: value })}
                />,
              )}

              <div className="flex items-end gap-2">
                <div className="min-w-0 flex-1">
                  {field(
                    t("字体"),
                    <Select
                      aria-label={t("字体")}
                      className="[&_*]:min-w-0"
                      classNames={{ trigger: "h-8 min-h-8" }}
                      popoverProps={{ motionProps: popoverMotionProps }}
                      selectedKeys={[config.font]}
                      size="sm"
                      onSelectionChange={(keys) =>
                        patch({ font: String(Array.from(keys)[0] ?? "") })
                      }
                    >
                      {FONT_OPTIONS.map((option) => (
                        <SelectItem key={option.value}>
                          {option.label}
                        </SelectItem>
                      ))}
                    </Select>,
                  )}
                </div>
                <span className="flex items-center gap-1.5 pb-1 text-[12px] text-gray-500 dark:text-gray-400">
                  {t("加粗")}
                  <Switch
                    aria-label={t("加粗")}
                    color="primary"
                    isSelected={config.bold}
                    size="sm"
                    onValueChange={(value) => patch({ bold: value })}
                  />
                </span>
              </div>

              {field(
                t("分辨率（{0}px/方块）", { "0": config.resolution }),
                <Slider
                  aria-label={t("分辨率")}
                  maxValue={10}
                  minValue={2}
                  size="sm"
                  step={1}
                  value={config.resolution}
                  onChange={(value) =>
                    patch({
                      resolution: Array.isArray(value) ? value[0] : value,
                    })
                  }
                />,
              )}
              {field(
                t("厚度（{0}）", { "0": config.depth.toFixed(1) }),
                <Slider
                  aria-label={t("厚度")}
                  maxValue={3}
                  minValue={0.2}
                  size="sm"
                  step={0.1}
                  value={config.depth}
                  onChange={(value) =>
                    patch({ depth: Array.isArray(value) ? value[0] : value })
                  }
                />,
              )}
              {field(
                t("间隙（{0}）", { "0": config.spacing.toFixed(2) }),
                <Slider
                  aria-label={t("间隙")}
                  maxValue={0.4}
                  minValue={0}
                  size="sm"
                  step={0.02}
                  value={config.spacing}
                  onChange={(value) =>
                    patch({ spacing: Array.isArray(value) ? value[0] : value })
                  }
                />,
              )}

              {field(
                t("着色"),
                <Select
                  aria-label={t("着色模式")}
                  classNames={{ trigger: "h-8 min-h-8" }}
                  popoverProps={{ motionProps: popoverMotionProps }}
                  selectedKeys={[config.colorMode]}
                  size="sm"
                  onSelectionChange={(keys) =>
                    patch({
                      colorMode: Array.from(
                        keys,
                      )[0] as CubeTextConfig["colorMode"],
                    })
                  }
                >
                  {COLOR_MODES.map((option) => (
                    <SelectItem key={option.value}>{option.label}</SelectItem>
                  ))}
                </Select>,
              )}

              <div className="flex items-center gap-3">
                <span className="flex items-center gap-1.5 text-[12px] text-gray-500 dark:text-gray-400">
                  {t("颜色 A")}
                  <input
                    aria-label={t("颜色 A")}
                    className="h-7 w-9 cursor-pointer rounded border border-default-300 bg-transparent"
                    type="color"
                    value={config.colorA}
                    onChange={(event) => patch({ colorA: event.target.value })}
                  />
                </span>
                {config.colorMode === "gradient" ? (
                  <span className="flex items-center gap-1.5 text-[12px] text-gray-500 dark:text-gray-400">
                    {t("颜色 B")}
                    <input
                      aria-label={t("颜色 B")}
                      className="h-7 w-9 cursor-pointer rounded border border-default-300 bg-transparent"
                      type="color"
                      value={config.colorB}
                      onChange={(event) =>
                        patch({ colorB: event.target.value })
                      }
                    />
                  </span>
                ) : null}
              </div>

              {field(
                t("表面纹理"),
                <Select
                  aria-label={t("表面纹理")}
                  classNames={{ trigger: "h-8 min-h-8" }}
                  popoverProps={{ motionProps: popoverMotionProps }}
                  selectedKeys={[config.texture]}
                  size="sm"
                  onSelectionChange={(keys) =>
                    patch({
                      texture: Array.from(keys)[0] as CubeTextConfig["texture"],
                    })
                  }
                >
                  {TEXTURES.map((option) => (
                    <SelectItem key={option.value}>{option.label}</SelectItem>
                  ))}
                </Select>,
              )}

              <div className="flex items-center justify-between">
                <span className="text-[12px] text-gray-600 dark:text-gray-300">
                  {t("黑色描边")}
                </span>
                <div className="flex items-center gap-2">
                  {config.outline ? (
                    <input
                      aria-label={t("描边颜色")}
                      className="h-7 w-9 cursor-pointer rounded border border-default-300 bg-transparent"
                      type="color"
                      value={config.outlineColor}
                      onChange={(event) =>
                        patch({ outlineColor: event.target.value })
                      }
                    />
                  ) : null}
                  <Switch
                    aria-label={t("描边")}
                    color="primary"
                    isSelected={config.outline}
                    size="sm"
                    onValueChange={(value) => patch({ outline: value })}
                  />
                </div>
              </div>

              <div className="flex items-center justify-between">
                <span className="text-[12px] text-gray-600 dark:text-gray-300">
                  {t("透明背景")}
                </span>
                <div className="flex items-center gap-2">
                  {config.background ? (
                    <input
                      aria-label={t("背景颜色")}
                      className="h-7 w-9 cursor-pointer rounded border border-default-300 bg-transparent"
                      type="color"
                      value={config.background}
                      onChange={(event) =>
                        patch({ background: event.target.value })
                      }
                    />
                  ) : null}
                  <Switch
                    aria-label={t("透明背景")}
                    color="primary"
                    isSelected={config.background === null}
                    size="sm"
                    onValueChange={(value) =>
                      patch({ background: value ? null : "#0B1220" })
                    }
                  />
                </div>
              </div>

              <div className="flex items-center justify-between">
                <span className="text-[12px] text-gray-600 dark:text-gray-300">
                  {t("自动旋转")}
                </span>
                <Switch
                  aria-label={t("自动旋转")}
                  color="primary"
                  isSelected={autoRotate}
                  size="sm"
                  onValueChange={setAutoRotate}
                />
              </div>
            </div>

            {/* 右：预览 */}
            <div className="flex min-w-0 flex-1 flex-col gap-2">
              <div className="relative min-h-0 flex-1 overflow-hidden rounded-xl border border-default-200 bg-[repeating-conic-gradient(#00000010_0%_25%,transparent_0%_50%)] bg-[length:20px_20px] dark:border-gray-700">
                {isOpen ? (
                  <Suspense
                    fallback={
                      <div className="flex h-full items-center justify-center text-xs text-gray-400">
                        {t("正在加载 3D 渲染器…")}
                      </div>
                    }
                  >
                    <CubeText3DPreview
                      ref={previewRef}
                      autoRotate={autoRotate}
                      config={config}
                    />
                  </Suspense>
                ) : null}
              </div>

              <div className="flex flex-none items-center justify-between gap-2">
                <span className="truncate text-[11px] text-gray-400" />
                <div className="flex gap-2">
                  <Button
                    size="sm"
                    startContent={<ArrowClockwise20Regular />}
                    variant="flat"
                    onPress={() => previewRef.current?.resetCamera()}
                  >
                    {t("重置视角")}
                  </Button>
                  <Button
                    color="primary"
                    isLoading={exporting}
                    size="sm"
                    startContent={<Save20Regular />}
                    onPress={() => void exportPng()}
                  >
                    {t("截图导出 PNG")}
                  </Button>
                </div>
              </div>
            </div>
          </div>
        </ModalShell>
      </ModalContent>
    </Modal>
  );
};

export default CubeText3DDialog;
