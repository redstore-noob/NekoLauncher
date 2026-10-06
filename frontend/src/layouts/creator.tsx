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
 * 创作中心：面向整合包作者等创作者的工具入口页（创作者工坊）。
 * 点击入口卡片进入对应工具的独立全屏工作区（不再弹窗），
 * 工作区左上角经 CreatorToolShell 的返回按钮回到本页。
 * 之后新增创作工具时往 ENTRIES 里加一项即可。
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
  Wand20Regular,
  Sparkle20Regular,
  PaintBrush20Regular,
  Code20Regular,
  Grid20Regular,
  ShieldCheckmark20Regular,
} from "@fluentui/react-icons";

import CubeText3DDialog from "../components/creator/CubeText3DDialog";
import CommandGeneratorDialog from "../components/creator/CommandGeneratorDialog";
import GradientTextDialog from "../components/creator/GradientTextDialog";
import ModpackExportDialog from "../components/creator/ModpackExportDialog";
import PluginManifestDialog from "../components/creator/PluginManifestDialog";
import ResourcePackDialog from "../components/creator/ResourcePackDialog";
import SkinEditDialog from "../components/creator/SkinEditDialog";
import { t } from "../i18n";

type CreatorToolKey =
  | "modpack"
  | "solo"
  | "resourcepack"
  | "gradienttext"
  | "cubetext3d"
  | "commandgen"
  | "plugin"
  | "skin";

type CreatorCategory = "all" | "packaging" | "visual" | "textart" | "dev";

interface CreatorEntry {
  key: CreatorToolKey;
  title: string;
  description: string;
  icon: React.ReactNode;
  /** 图标磁贴渐变（与主页小组件同风格） */
  tileClass: string;
  category: Exclude<CreatorCategory, "all">;
  /** 工具能力标签（磁贴下的小胶囊） */
  tags: string[];
}

const CATEGORIES: Array<{
  key: CreatorCategory;
  label: string;
  icon: React.ReactNode;
}> = [
  { key: "all", label: "全部工具", icon: <Wand20Regular /> },
  { key: "packaging", label: "整合分发", icon: <FolderZip20Regular /> },
  { key: "visual", label: "材质外观", icon: <PaintBrush20Regular /> },
  { key: "textart", label: "艺术文字", icon: <TextFont20Regular /> },
  { key: "dev", label: "开发指令", icon: <Code20Regular /> },
];

const ENTRIES: CreatorEntry[] = [
  {
    key: "modpack",
    title: "整合包制作",
    description:
      "选择实例版本，打包为 Modrinth (.mrpack) 或 MultiMC (.zip) 整合包",
    icon: <FolderZip20Regular />,
    tileClass: "from-sky-400 via-blue-500 to-indigo-500 shadow-blue-500/30",
    category: "packaging",
    tags: [".mrpack / .zip", "内容清单"],
  },
  {
    key: "solo",
    title: "NekoSolo 安装包",
    description:
      "把启动器、Java 与整合包打进单个 exe，玩家双击即玩（仅 Windows）",
    icon: <Box20Regular />,
    tileClass: "from-rose-400 via-red-500 to-orange-500 shadow-rose-500/30",
    category: "packaging",
    tags: ["单文件 exe", "双击即玩"],
  },
  {
    key: "resourcepack",
    title: "资源包制作",
    description:
      "像素画布绘制贴图、自动生成 pack.mcmeta，导出可安装的资源包 (.zip)",
    icon: <Color20Regular />,
    tileClass: "from-emerald-400 via-teal-500 to-cyan-500 shadow-teal-500/30",
    category: "visual",
    tags: ["像素画布", "贴图绘制"],
  },
  {
    key: "gradienttext",
    title: "渐变文字",
    description:
      "多色渐变生成 Minecraft 彩色字，支持 &#RRGGBB / §x… / MiniMessage",
    icon: <TextFont20Regular />,
    tileClass:
      "from-pink-400 via-fuchsia-500 to-purple-500 shadow-fuchsia-500/30",
    category: "textart",
    tags: ["MiniMessage", "多格式输出"],
  },
  {
    key: "cubetext3d",
    title: "3D 文字生成器",
    description:
      "文本转 Minecraft 体素方块文字，可旋转预览、描边并用透明背景导出 PNG",
    icon: <BoxMultiple20Regular />,
    tileClass: "from-cyan-400 via-blue-500 to-indigo-500 shadow-indigo-500/30",
    category: "textart",
    tags: ["体素方块", "3D 预览"],
  },
  {
    key: "commandgen",
    title: "指令生成器",
    description: "图形化点选给物品 / 给效果 / 传送等命令，实时拼出可粘贴的指令",
    icon: <Options20Regular />,
    tileClass: "from-lime-400 via-green-500 to-emerald-500 shadow-green-500/30",
    category: "dev",
    tags: ["可视化点选", "实时预览"],
  },
  {
    key: "plugin",
    title: "插件制作",
    description: "从零创建插件骨架（plugin.yaml + 入口模板），打包为 .nekoex",
    icon: <PuzzleCube20Regular />,
    tileClass:
      "from-violet-400 via-purple-500 to-fuchsia-500 shadow-purple-500/30",
    category: "dev",
    tags: ["骨架生成", ".nekoex"],
  },
  {
    key: "skin",
    title: "皮肤编辑",
    description: "2D 画布绘制 + 3D 实时预览，导出 PNG 或直接设为账号皮肤",
    icon: <Person20Regular />,
    tileClass: "from-amber-400 via-orange-500 to-rose-500 shadow-orange-500/30",
    category: "visual",
    tags: ["2D+3D 实时画板", "一键应用"],
  },
];

const CreatorPage: React.FC = () => {
  const [activeTool, setActiveTool] = useState<CreatorToolKey | null>(null);
  const [keyword, setKeyword] = useState("");
  const [category, setCategory] = useState<CreatorCategory>("all");

  // 搜索：标题 + 描述 + 标签匹配（不区分大小写），直接过滤入口卡片
  const filtered = useMemo(() => {
    const needle = keyword.trim().toLowerCase();

    return ENTRIES.filter((entry) => {
      if (category !== "all" && entry.category !== category) return false;
      if (!needle) return true;

      return `${entry.title}\n${entry.description}\n${entry.tags.join("\n")}`
        .toLowerCase()
        .includes(needle);
    });
  }, [keyword, category]);

  // 工具全屏工作区：组件内部经 embedded 模式渲染 CreatorToolShell，
  // 返回按钮直接回调 onClose → 置空 activeTool 即回到本页
  if (activeTool) {
    switch (activeTool) {
      case "modpack":
        return (
          <ModpackExportDialog
            embedded
            isOpen
            onClose={() => setActiveTool(null)}
          />
        );
      case "solo":
        return (
          <ModpackExportDialog
            embedded
            isOpen
            soloOnly
            onClose={() => setActiveTool(null)}
          />
        );
      case "resourcepack":
        return (
          <ResourcePackDialog
            embedded
            isOpen
            onClose={() => setActiveTool(null)}
          />
        );
      case "gradienttext":
        return (
          <GradientTextDialog
            embedded
            isOpen
            onClose={() => setActiveTool(null)}
          />
        );
      case "cubetext3d":
        return (
          <CubeText3DDialog
            embedded
            isOpen
            onClose={() => setActiveTool(null)}
          />
        );
      case "commandgen":
        return (
          <CommandGeneratorDialog
            embedded
            isOpen
            onClose={() => setActiveTool(null)}
          />
        );
      case "plugin":
        return (
          <PluginManifestDialog
            embedded
            isOpen
            onClose={() => setActiveTool(null)}
          />
        );
      case "skin":
        return (
          <SkinEditDialog embedded isOpen onClose={() => setActiveTool(null)} />
        );
    }
  }

  return (
    <div className="h-full w-full overflow-y-auto">
      <div className="mx-auto max-w-3xl px-6 py-8">
        {/* Hero 横幅：创作者工坊 */}
        <section className="nya-card-in relative overflow-hidden rounded-lg border nya-border bg-gradient-to-br from-primary/15 via-primary/5 to-transparent p-5">
          <div className="pointer-events-none absolute -right-8 -top-10 h-40 w-40 rounded-full bg-primary/15 blur-3xl" />
          <div className="pointer-events-none absolute -bottom-12 right-16 h-28 w-28 rounded-full bg-fuchsia-500/10 blur-2xl" />
          <div className="flex items-center gap-2">
            <span className="flex h-8 w-8 items-center justify-center rounded-lg bg-gradient-to-br from-primary to-fuchsia-500 text-white shadow-lg shadow-primary/30">
              <Wand20Regular />
            </span>
            <span className="text-xs font-semibold uppercase tracking-widest text-primary">
              {t("创作者工坊")}
            </span>
          </div>
          <h1 className="mt-3 text-xl font-semibold text-gray-800 dark:text-gray-100">
            {t("创作中心")}
          </h1>
          <p className="mt-1 max-w-lg text-xs leading-relaxed text-gray-500 dark:text-gray-400">
            {t(
              "面向整合包作者、像素画师与插件开发者的一站式创作工坊——打包分发、绘制贴图、生成文字与指令，全部开箱即用。",
            )}
          </p>
          <div className="mt-3 flex flex-wrap gap-1.5">
            {[
              { icon: <Grid20Regular />, label: "8 款专属工具" },
              { icon: <Sparkle20Regular />, label: "开箱即用" },
              { icon: <ShieldCheckmark20Regular />, label: "全程本地创作" },
            ].map((chip) => (
              <span
                key={chip.label}
                className="flex items-center gap-1 rounded-full border nya-border bg-background/40 px-2.5 py-1 text-[11px] text-gray-500 dark:text-gray-400"
              >
                <span className="text-primary">{chip.icon}</span>
                {t(chip.label)}
              </span>
            ))}
          </div>
        </section>

        {/* 分类 + 搜索 */}
        <div className="mt-6 flex flex-wrap items-center gap-2">
          {CATEGORIES.map((item) => (
            <button
              key={item.key}
              aria-pressed={category === item.key}
              className={`flex cursor-pointer items-center gap-1.5 rounded-full border px-3 py-1.5 text-xs transition-colors ${
                category === item.key
                  ? "border-primary/40 bg-primary/10 font-semibold text-primary"
                  : "nya-border text-gray-500 hover:bg-default-100 dark:text-gray-400 dark:hover:bg-gray-800"
              }`}
              type="button"
              onClick={() => setCategory(item.key)}
            >
              {item.icon}
              {t(item.label)}
            </button>
          ))}
          <Input
            aria-label={t("搜索创作工具")}
            className="ml-auto w-full max-w-[220px]"
            placeholder={t("搜索工具，例如 整合包 / 皮肤")}
            size="sm"
            startContent={<Search20Regular className="text-gray-400" />}
            value={keyword}
            variant="bordered"
            onValueChange={setKeyword}
          />
        </div>

        {/* 无卡列表：与设置页同款平铺行，图标 + 标题/说明在左，箭头在右 */}
        <div className="mt-2 flex flex-col">
          {filtered.map((entry, index) => (
            <div
              key={entry.key}
              className="nya-card-in"
              style={{ animationDelay: `${index * 0.06}s` }}
            >
              <button
                aria-label={t("打开{0}", { "0": entry.title })}
                className="group flex w-full cursor-pointer items-center gap-3 rounded-lg px-3 py-3 text-left transition-colors hover:bg-default-100/50 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40 dark:hover:bg-default-100/40"
                type="button"
                onClick={() => setActiveTool(entry.key)}
              >
                <span
                  className={`flex h-10 w-10 flex-shrink-0 items-center justify-center rounded-lg bg-gradient-to-br text-white shadow-lg ${entry.tileClass}`}
                >
                  {entry.icon}
                </span>
                <span className="min-w-0 flex-1">
                  <span className="flex items-baseline gap-2">
                    <span className="truncate text-sm font-medium text-gray-800 dark:text-gray-200">
                      {t(entry.title)}
                    </span>
                    <span className="flex-shrink-0 text-xs text-gray-400">
                      {t(
                        CATEGORIES.find((c) => c.key === entry.category)
                          ?.label ?? "",
                      )}
                    </span>
                    <span className="hidden flex-wrap gap-1.5 sm:flex">
                      {entry.tags.slice(0, 2).map((tag) => (
                        <span
                          key={tag}
                          className="rounded-full bg-default-100 px-2 py-0.5 text-[10px] text-gray-500 dark:bg-gray-800 dark:text-gray-400"
                        >
                          {t(tag)}
                        </span>
                      ))}
                    </span>
                  </span>
                  <span className="mt-0.5 block truncate text-xs text-gray-400">
                    {t(entry.description)}
                  </span>
                </span>
                <ArrowRight20Regular className="flex-shrink-0 text-gray-300 transition-transform group-hover:translate-x-0.5 group-hover:text-gray-400 dark:text-gray-600" />
              </button>
            </div>
          ))}
          {filtered.length === 0 ? (
            <div className="col-span-full rounded-lg border border-dashed border-gray-300/80 px-5 py-8 text-center text-xs text-gray-400 dark:border-gray-700">
              {t("没有匹配的创作工具，换个关键词试试。")}
            </div>
          ) : null}
        </div>

        {/* 底部创作贴士 */}
        <section className="nya-panel-inner nya-border mt-6 rounded-medium border p-4">
          <div className="flex items-center gap-2 text-xs font-semibold text-gray-600 dark:text-gray-300">
            <Sparkle20Regular className="text-amber-400" />
            {t("创作小贴士")}
          </div>
          <ul className="mt-2 space-y-1.5 text-xs leading-relaxed text-gray-400">
            <li>
              ·{" "}
              {t(
                "整合包分发前记得检查第三方模组的再分发授权（尤其 CurseForge 资源）。",
              )}
            </li>
            <li>
              ·{" "}
              {t("皮肤绘制完成后可直接一键设为当前账号的皮肤，无需手动上传。")}
            </li>
            <li>
              · {t("NekoSolo 安装包默认开启简洁模式，适合直接发放给玩家。")}
            </li>
          </ul>
        </section>
      </div>
    </div>
  );
};

export default CreatorPage;
