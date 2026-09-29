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
 * 创作中心：面向整合包作者等创作者的工具入口页。
 * 每张入口卡片打开一个二级界面（整合包制作 / 插件制作 / 皮肤编辑）；
 * 之后新增创作工具时往 entries 里加一项即可。
 */
import React, { useMemo, useState } from "react";
import { Input } from "@heroui/react";
import {
  ArrowRight20Regular,
  Box20Regular,
  BoxMultiple20Regular,
  Color20Regular,
  FolderZip20Regular,
  Person20Regular,
  PuzzleCube20Regular,
  Search20Regular,
  TextFont20Regular,
  Options20Regular,
} from "@fluentui/react-icons";

import CubeText3DDialog from "../components/creator/CubeText3DDialog";
import CommandGeneratorDialog from "../components/creator/CommandGeneratorDialog";
import GradientTextDialog from "../components/creator/GradientTextDialog";
import ModpackExportDialog from "../components/creator/ModpackExportDialog";
import PluginManifestDialog from "../components/creator/PluginManifestDialog";
import ResourcePackDialog from "../components/creator/ResourcePackDialog";
import SkinEditDialog from "../components/creator/SkinEditDialog";
import { t } from "../i18n";

interface CreatorEntry {
  key: string;
  title: string;
  description: string;
  icon: React.ReactNode;
  /** 图标磁贴渐变（与主页小组件同风格） */
  tileClass: string;
  open: () => void;
}

const CreatorPage: React.FC = () => {
  const [modpackOpen, setModpackOpen] = useState(false);
  const [soloOpen, setSoloOpen] = useState(false);
  const [pluginOpen, setPluginOpen] = useState(false);
  const [skinOpen, setSkinOpen] = useState(false);
  const [resourcePackOpen, setResourcePackOpen] = useState(false);
  const [gradientTextOpen, setGradientTextOpen] = useState(false);
  const [cubeTextOpen, setCubeTextOpen] = useState(false);
  const [commandOpen, setCommandOpen] = useState(false);
  const [keyword, setKeyword] = useState("");
  const entries: CreatorEntry[] = [
    {
      key: "modpack",
      title: t("整合包制作"),
      description: t(
        "选择实例版本，打包为 Modrinth (.mrpack) 或 MultiMC (.zip) 整合包",
      ),
      icon: <FolderZip20Regular />,
      tileClass: "from-sky-400 via-blue-500 to-indigo-500 shadow-blue-500/30",
      open: () => setModpackOpen(true),
    },
    {
      key: "solo",
      title: t("NekoSolo 安装包"),
      description: t(
        "把启动器、Java 与整合包打进单个 exe，玩家双击即玩（仅 Windows）",
      ),
      icon: <Box20Regular />,
      tileClass: "from-rose-400 via-red-500 to-orange-500 shadow-rose-500/30",
      open: () => setSoloOpen(true),
    },
    {
      key: "resourcepack",
      title: t("资源包制作"),
      description: t(
        "像素画布绘制贴图、自动生成 pack.mcmeta，导出可安装的资源包 (.zip)",
      ),
      icon: <Color20Regular />,
      tileClass: "from-emerald-400 via-teal-500 to-cyan-500 shadow-teal-500/30",
      open: () => setResourcePackOpen(true),
    },
    {
      key: "gradienttext",
      title: t("渐变文字"),
      description: t(
        "多色渐变生成 Minecraft 彩色字，支持 &#RRGGBB / §x… / MiniMessage",
      ),
      icon: <TextFont20Regular />,
      tileClass:
        "from-pink-400 via-fuchsia-500 to-purple-500 shadow-fuchsia-500/30",
      open: () => setGradientTextOpen(true),
    },
    {
      key: "cubetext3d",
      title: t("3D 文字生成器"),
      description: t(
        "文本转 Minecraft 体素方块文字，可旋转预览、描边并用透明背景导出 PNG",
      ),
      icon: <BoxMultiple20Regular />,
      tileClass:
        "from-cyan-400 via-blue-500 to-indigo-500 shadow-indigo-500/30",
      open: () => setCubeTextOpen(true),
    },
    {
      key: "commandgen",
      title: t("指令生成器"),
      description: t(
        "图形化点选给物品 / 给效果 / 传送等命令，实时拼出可粘贴的指令",
      ),
      icon: <Options20Regular />,
      tileClass:
        "from-lime-400 via-green-500 to-emerald-500 shadow-green-500/30",
      open: () => setCommandOpen(true),
    },
    {
      key: "plugin",
      title: t("插件制作"),
      description: t(
        "从零创建插件骨架（plugin.yaml + 入口模板），打包为 .nekoex",
      ),
      icon: <PuzzleCube20Regular />,
      tileClass:
        "from-violet-400 via-purple-500 to-fuchsia-500 shadow-purple-500/30",
      open: () => setPluginOpen(true),
    },
    {
      key: "skin",
      title: t("皮肤编辑"),
      description: t("2D 画布绘制 + 3D 实时预览，导出 PNG 或直接设为账号皮肤"),
      icon: <Person20Regular />,
      tileClass:
        "from-amber-400 via-orange-500 to-rose-500 shadow-orange-500/30",
      open: () => setSkinOpen(true),
    },
  ];

  // 搜索：标题 + 描述匹配（不区分大小写），直接过滤入口卡片
  const filtered = useMemo(() => {
    const needle = keyword.trim().toLowerCase();

    if (!needle) return entries;

    return entries.filter((entry) =>
      `${entry.title}\n${entry.description}`.toLowerCase().includes(needle),
    );
    // entries 每次渲染都重建（open 回调闭包），故意不列依赖：按 keyword 过滤即可
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [keyword]);

  return (
    <div className="h-full w-full overflow-y-auto">
      <div className="mx-auto max-w-2xl px-6 py-8">
        <div className="flex flex-wrap items-center gap-3">
          <h1 className="min-w-0 flex-1 text-xl font-semibold text-gray-800 dark:text-gray-200">
            {t("创作中心")}
          </h1>
          <Input
            aria-label={t("搜索创作工具")}
            className="w-full max-w-[220px]"
            placeholder={t("搜索工具，例如 整合包 / 皮肤")}
            size="sm"
            startContent={<Search20Regular className="text-gray-400" />}
            value={keyword}
            variant="bordered"
            onValueChange={setKeyword}
          />
        </div>

        {/* 入场用 CSS(nya-card-in)：fill 只 backwards，结束后不锁 transform，
            卡片自身的 hover:scale-[1.02] 不受影响（framer 版已随切换动画
            一起去掉，避免 JS 动画状态机的偶发挂起） */}
        <div className="mt-6 grid grid-cols-1 gap-3 sm:grid-cols-2">
          {filtered.map((entry, index) => (
            <div
              key={entry.key}
              className="nya-card-in"
              style={{ animationDelay: `${index * 0.06}s` }}
            >
              <button
                aria-label={t("打开{0}", { "0": entry.title })}
                className="nya-panel nya-border group flex h-full w-full cursor-pointer items-center gap-3 rounded-xl border p-4 text-left transition-transform hover:scale-[1.02]"
                type="button"
                onClick={entry.open}
              >
                <span
                  className={`flex h-10 w-10 flex-shrink-0 items-center justify-center rounded-lg bg-gradient-to-br text-white shadow-lg ${entry.tileClass}`}
                >
                  {entry.icon}
                </span>
                <span className="min-w-0 flex-1">
                  <span className="block text-sm font-medium text-gray-800 dark:text-gray-200">
                    {entry.title}
                  </span>
                  <span className="mt-0.5 block truncate text-xs text-gray-400">
                    {entry.description}
                  </span>
                </span>
                <ArrowRight20Regular className="flex-shrink-0 text-gray-300 transition-transform group-hover:translate-x-0.5 group-hover:text-gray-400 dark:text-gray-600" />
              </button>
            </div>
          ))}
          {filtered.length === 0 ? (
            <div className="col-span-full rounded-2xl border border-dashed border-gray-300/80 px-5 py-8 text-center text-xs text-gray-400 dark:border-gray-700">
              {t("没有匹配的创作工具，换个关键词试试。")}
            </div>
          ) : null}
        </div>
      </div>

      <ModpackExportDialog
        isOpen={modpackOpen}
        onClose={() => setModpackOpen(false)}
      />
      <ModpackExportDialog
        soloOnly
        isOpen={soloOpen}
        onClose={() => setSoloOpen(false)}
      />
      <PluginManifestDialog
        isOpen={pluginOpen}
        onClose={() => setPluginOpen(false)}
      />
      <SkinEditDialog isOpen={skinOpen} onClose={() => setSkinOpen(false)} />
      <ResourcePackDialog
        isOpen={resourcePackOpen}
        onClose={() => setResourcePackOpen(false)}
      />
      <GradientTextDialog
        isOpen={gradientTextOpen}
        onClose={() => setGradientTextOpen(false)}
      />
      <CubeText3DDialog
        isOpen={cubeTextOpen}
        onClose={() => setCubeTextOpen(false)}
      />
      <CommandGeneratorDialog
        isOpen={commandOpen}
        onClose={() => setCommandOpen(false)}
      />
    </div>
  );
};

export default CreatorPage;
