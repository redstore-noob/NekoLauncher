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
 * 启动页的主视觉图标：读取实例自定义图标，缺失时按实例 Id 关键字判断加载器，
 * 回落到 public/instance-icons 下的内置图；图片都取不到时用 Fluent 立方体兜底。
 * 三种来源的判定顺序与实例页保持一致，避免同一个实例两处显示不同图标。
 */
import React, { useEffect, useState } from "react";
import { Cube20Regular } from "@fluentui/react-icons";

import { GetInstanceVisual } from "../../../wailsjs/go/bindings/ContentAPI";

/** 内置图标位于 public/instance-icons，键与实例页 / 下载页共用 */
const BUILTIN_ICON_KEYS = new Set([
  "vanilla",
  "fabric",
  "forge",
  "neoforge",
  "liteloader",
  "command_block",
  "old_version",
  "snapshot_version",
]);

/**
 * 实例 Id 关键字 → 内置图标键。实例本身没有图标资源时不至于是个空白方块，
 * 至少能一眼看出是原版还是带加载器的整合包。
 */
const ICON_KEYWORDS: Array<[RegExp, string]> = [
  [/neoforge/i, "neoforge"],
  [/fabric/i, "fabric"],
  [/liteloader/i, "liteloader"],
  [/forge/i, "forge"],
  [/snapshot|^\d{2}w\d{2}/i, "snapshot_version"],
  [/^[abc]?\d*\.\d+(\.\d+)?$/i, "vanilla"],
];

function guessIconKey(versionId: string): string {
  for (const [pattern, key] of ICON_KEYWORDS) {
    if (pattern.test(versionId)) return key;
  }

  return "vanilla";
}

/** 自定义图标走 /localfile 中转，内置图标直接取 public 下的静态文件 */
function iconSources(versionId: string, iconPath: string): string[] {
  const sources: string[] = [];

  if (iconPath && !iconPath.startsWith("gameicon:")) {
    sources.push(`/localfile?path=${encodeURIComponent(iconPath)}`);
  }

  const key = iconPath.startsWith("gameicon:")
    ? iconPath.slice("gameicon:".length)
    : guessIconKey(versionId);

  if (BUILTIN_ICON_KEYS.has(key)) sources.push(`/instance-icons/${key}.png`);

  return sources;
}

interface LaunchVersionIconProps {
  /** 当前选中的实例 Id；为空时直接显示兜底图标 */
  versionId: string;
}

const LaunchVersionIcon: React.FC<LaunchVersionIconProps> = ({ versionId }) => {
  const [iconPath, setIconPath] = useState("");
  const [brokenCount, setBrokenCount] = useState(0);

  useEffect(() => {
    if (!versionId) {
      setIconPath("");

      return;
    }
    let alive = true;

    GetInstanceVisual(versionId, "")
      .then((visual) => {
        if (alive) setIconPath(visual?.IconPath ?? "");
      })
      .catch(() => {
        if (alive) setIconPath("");
      });

    return () => {
      alive = false;
    };
  }, [versionId]);

  // 换实例时重置加载失败计数，否则新实例会沿用上一个的失败状态
  useEffect(() => {
    setBrokenCount(0);
  }, [versionId, iconPath]);

  const sources = versionId ? iconSources(versionId, iconPath) : [];
  const src = sources[brokenCount];

  if (!src) {
    return <Cube20Regular className="h-7 w-7 text-gray-400" />;
  }

  return (
    <img
      alt=""
      className="h-8 w-8 object-contain [image-rendering:pixelated]"
      src={src}
      onError={() => setBrokenCount((n) => n + 1)}
    />
  );
};

export default LaunchVersionIcon;
