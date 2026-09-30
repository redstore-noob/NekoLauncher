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
 * 插件制作（创作中心的二级界面）：从零创建插件骨架并打包为 .nekoex。
 * 表单填写 plugin.yaml 的清单字段（图形化配置），后端 CreatePluginScaffold
 * 在所选位置生成 <id>/ 目录（plugin.yaml + 入口模板 + assets/），随后可
 * 直接打包分享。不加载、不编辑已安装的他人插件。
 */
import React, { useState } from "react";
import { Button, Input, Modal, ModalContent } from "@heroui/react";
import { PuzzleCube20Regular } from "@fluentui/react-icons";

import {
  PackagePlugin,
  CreatePluginScaffold,
} from "../../../wailsjs/go/bindings/PluginAPI";
import {
  OpenInExplorer,
  SaveFile,
  SelectDirectory,
} from "../../../wailsjs/go/bindings/SystemAPI";
import { ModalShell, modalBehaviorProps } from "../modal-shell";
import { t } from "../../i18n";

/** 宿主插件 API 版本（与 plugin/api.ts 的 PLUGIN_API_VERSION 一致） */
const HOST_API_VERSION = "1";

function asMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

/** id 输入的即时校验（与后端 validatePluginID 规则一致） */
function validateId(id: string): string {
  if (!id) return "";
  if (id.length > 64) return t("id 过长（上限 64 字符）");
  if (!/^[a-zA-Z0-9_-]+$/.test(id))
    return t("只允许字母、数字、连字符与下划线");

  return "";
}

/** 新插件的入口文件模板：一个可运行的最小示例，作者在此基础上改 */
const ENTRY_TEMPLATE = t(
  '// NekoLauncher 插件入口：宿主加载插件时会调用默认导出的 activate 函数。\n// 可用能力见 api：react / h / ui（HeroUI 组件白名单）/ icons / registerWidget /\n// registerPage / config（插件私有配置）/ log。\nexport default function activate(api) {\n  const { react: React, h, ui, icons, registerWidget, log } = api;\n\n  registerWidget({\n    id: "hello",\n    title: "你好世界",\n    description: "我的第一个插件小组件",\n    icon: h(icons.Sparkle20Regular ?? icons.PuzzleCube20Regular),\n    tileClass: "from-pink-400 via-rose-500 to-red-500 shadow-rose-500/30",\n    render: (context) =>\n      h(\n        ui.Button,\n        {\n          color: "primary",\n          size: "sm",\n          variant: "flat",\n          onPress: () => log("你好，NekoLauncher！"),\n        },\n        "来自插件的问候",\n      ),\n  });\n}\n',
);

interface ManifestForm {
  id: string;
  name: string;
  version: string;
  apiVersion: string;
  author: string;
  description: string;
  entry: string;
  icon: string;
  /** 逗号分隔的样式文件列表（相对插件目录、限 .css），留空 = 无样式 */
  styles: string;
}

type DialogPhase = "edit" | "created";

const PluginManifestDialog: React.FC<{
  isOpen: boolean;
  onClose: () => void;
}> = ({ isOpen, onClose }) => {
  const [phase, setPhase] = useState<DialogPhase>("edit");
  const [form, setForm] = useState<ManifestForm>({
    id: "",
    name: "",
    version: "1.0.0",
    apiVersion: HOST_API_VERSION,
    author: "",
    description: "",
    entry: "index.js",
    icon: "icon.png",
    styles: "",
  });
  const [creating, setCreating] = useState(false);
  const [packaging, setPackaging] = useState(false);
  const [createdDir, setCreatedDir] = useState("");
  const [packedPath, setPackedPath] = useState("");
  const [error, setError] = useState("");

  const idError = validateId(form.id);
  const canCreate =
    !!form.id && !idError && !!form.name.trim() && !!form.apiVersion.trim();

  const patch = (key: keyof ManifestForm, value: string) =>
    setForm((prev) => ({ ...prev, [key]: value }));

  const reset = () => {
    setPhase("edit");
    setCreatedDir("");
    setPackedPath("");
    setError("");
  };

  const createPlugin = async () => {
    setError("");
    let parent: string;

    try {
      parent = await SelectDirectory(t("选择插件创建位置"));
    } catch {
      return; // 用户取消
    }
    if (!parent) return;
    setCreating(true);
    try {
      const manifest = {
        id: form.id,
        name: form.name.trim(),
        version: form.version.trim() || "1.0.0",
        apiVersion: form.apiVersion.trim(),
        description: form.description.trim(),
        author: form.author.trim(),
        entry: form.entry.trim() || "index.js",
        icon: form.icon.trim() || "icon.png",
        styles: form.styles
          .split(",")
          .map((file) => file.trim())
          .filter(Boolean),
      };
      const target = await CreatePluginScaffold(
        parent,
        JSON.stringify(manifest),
        ENTRY_TEMPLATE,
      );

      setCreatedDir(target);
      setPackedPath("");
      setPhase("created");
    } catch (err) {
      setError(asMessage(err));
    } finally {
      setCreating(false);
    }
  };

  const packagePlugin = async () => {
    if (!createdDir) return;
    setPackaging(true);
    setError("");
    try {
      const defaultName = `${form.id}-${form.version || "1.0.0"}.nekoex`;
      const output = await SaveFile(
        t("打包插件"),
        defaultName,
        t("NekoLauncher 插件包"),
        "*.nekoex",
      );

      if (!output) return; // 用户取消
      const packed = await PackagePlugin(createdDir, output);

      setPackedPath(packed);
    } catch (err) {
      setError(asMessage(err));
    } finally {
      setPackaging(false);
    }
  };

  // 弹窗内统一的紧凑设置行
  const row = (label: string, node: React.ReactNode, hint?: string) => (
    <div className="flex flex-wrap items-center justify-between gap-x-3 gap-y-1.5 py-1.5">
      <div className="min-w-0">
        <div className="text-sm text-gray-800 dark:text-gray-200">{label}</div>
        {hint ? (
          <div className="mt-0.5 text-xs text-gray-400">{hint}</div>
        ) : null}
      </div>
      <div className="max-w-full flex-shrink-0">{node}</div>
    </div>
  );

  const textField = (
    key: keyof ManifestForm,
    label: string,
    placeholder?: string,
    hint?: string,
  ) =>
    row(
      label,
      <Input
        aria-label={label}
        className="w-56"
        placeholder={placeholder}
        size="sm"
        value={form[key]}
        onValueChange={(value) => patch(key, value)}
      />,
      hint,
    );

  return (
    <Modal isOpen={isOpen} size="2xl" onClose={onClose} {...modalBehaviorProps}>
      <ModalContent className="max-h-[85vh]">
        <ModalShell
          icon={<PuzzleCube20Regular />}
          title={t("插件制作")}
          onClose={onClose}
        >
          {phase === "created" ? (
            <div className="space-y-3 py-2">
              <div className="text-sm text-gray-800 dark:text-gray-200">
                {t("插件骨架创建成功 🎉")}
              </div>
              <div className="nya-panel-inner nya-border rounded-lg border p-3 text-xs text-gray-500 dark:text-gray-400">
                <div className="truncate">{createdDir}</div>
                <div className="mt-1">
                  {t("已生成 plugin.yaml、入口文件与 assets 目录。")}
                </div>
              </div>
              {packedPath && (
                <div className="nya-panel-inner nya-border rounded-lg border p-3 text-xs text-gray-500 dark:text-gray-400">
                  <div className="truncate">
                    {t("已打包：")}
                    {packedPath}
                  </div>
                  <Button
                    className="mt-2"
                    size="sm"
                    variant="flat"
                    onPress={() => void OpenInExplorer(packedPath)}
                  >
                    {t("打开所在文件夹")}
                  </Button>
                </div>
              )}
              {error && <div className="text-xs text-danger">{error}</div>}
              <div className="flex justify-end gap-2">
                <Button size="sm" variant="light" onPress={onClose}>
                  {t("关闭")}
                </Button>
                <Button size="sm" variant="flat" onPress={reset}>
                  {t("再做一个")}
                </Button>
                <Button
                  color="primary"
                  isLoading={packaging}
                  size="sm"
                  onPress={() => void packagePlugin()}
                >
                  {t("打包 .nekoex")}
                </Button>
              </div>
            </div>
          ) : (
            <div className="space-y-1 py-1">
              {textField(
                "id",
                t("插件 id"),
                "my-plugin",
                idError || t("创建后不可修改"),
              )}
              {textField("name", t("名称"), t("我的插件"))}
              {textField("version", t("版本号"), "1.0.0")}
              {textField("apiVersion", t("API 版本"), HOST_API_VERSION)}
              {textField("author", t("作者"))}
              {textField("description", t("简介"))}
              {textField("entry", t("入口文件"), "index.js")}
              {textField("icon", t("图标文件"), "icon.png")}
              {textField(
                "styles",
                t("样式文件"),
                "theme.css",
                t("逗号分隔，加载时自动注入为全局 CSS，可自定义控件样式"),
              )}

              {error && <div className="mt-3 text-xs text-danger">{error}</div>}

              <div className="mt-3 flex justify-end gap-2 border-t border-gray-100 pt-3 dark:border-gray-800/60">
                <Button size="sm" variant="light" onPress={onClose}>
                  {t("关闭")}
                </Button>
                <Button
                  color="primary"
                  isDisabled={!canCreate}
                  isLoading={creating}
                  size="sm"
                  onPress={() => void createPlugin()}
                >
                  {t("创建插件")}
                </Button>
              </div>
            </div>
          )}
        </ModalShell>
      </ModalContent>
    </Modal>
  );
};

export default PluginManifestDialog;
