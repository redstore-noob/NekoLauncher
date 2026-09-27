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
 * 资源包文件树：按目录分组展示包内文件，支持折叠、搜索、选中与删除。
 * 目录默认展开，点节点收起；搜索时强制展开并只保留命中路径。
 */
import type { PackFile, PackTreeNode } from "./types";

import React, { useMemo, useState } from "react";
import { Button } from "@heroui/react";
import {
  ChevronDown20Regular,
  ChevronRight20Regular,
  Delete20Regular,
  Document20Regular,
  DocumentText20Regular,
  Image20Regular,
} from "@fluentui/react-icons";

import { t } from "../../../i18n";

import { buildFileTree } from "./types";

interface PackFileTreeProps {
  files: PackFile[];
  selectedId: string;
  filter: string;
  onSelect: (id: string) => void;
  onDelete: (id: string) => void;
}

const PackFileTree: React.FC<PackFileTreeProps> = ({
  files,
  selectedId,
  filter,
  onSelect,
  onDelete,
}) => {
  const [collapsed, setCollapsed] = useState<Set<string>>(new Set());

  const tree = useMemo(() => {
    const keyword = filter.trim().toLowerCase();
    const visible = keyword
      ? files.filter((file) => file.path.toLowerCase().includes(keyword))
      : files;

    return buildFileTree(visible);
  }, [files, filter]);

  const searching = filter.trim().length > 0;
  const toggle = (path: string) => {
    setCollapsed((prev) => {
      const next = new Set(prev);

      if (next.has(path)) next.delete(path);
      else next.add(path);

      return next;
    });
  };

  const kindIcon = (file: PackFile) => {
    if (file.kind === "png") return <Image20Regular />;
    if (file.kind === "text") return <DocumentText20Regular />;

    return <Document20Regular />;
  };

  const renderNodes = (nodes: PackTreeNode[], depth: number): React.ReactNode =>
    nodes.map((node) => {
      const indent = { paddingLeft: `${depth * 14 + 6}px` };

      if (!node.isFile) {
        const isCollapsed = !searching && collapsed.has(node.path);

        return (
          <div key={`d:${node.path}`} className="flex flex-col">
            <button
              className="flex w-full cursor-pointer items-center gap-1 rounded-lg py-1.5 pr-2 text-left text-[12px] font-medium text-gray-600 transition-colors hover:bg-default-100 dark:text-gray-300 dark:hover:bg-gray-800"
              style={indent}
              type="button"
              onClick={() => toggle(node.path)}
            >
              {isCollapsed ? (
                <ChevronRight20Regular className="flex-shrink-0 text-gray-400" />
              ) : (
                <ChevronDown20Regular className="flex-shrink-0 text-gray-400" />
              )}
              <span className="truncate">{node.name}</span>
            </button>
            {isCollapsed ? null : renderNodes(node.children, depth + 1)}
          </div>
        );
      }

      const file = node.file as PackFile;
      const active = file.id === selectedId;

      return (
        <div
          key={file.id}
          className={`group flex items-center gap-1.5 rounded-lg pr-1 transition-colors ${
            active
              ? "bg-primary/10 text-primary"
              : "text-gray-700 hover:bg-default-100 dark:text-gray-200 dark:hover:bg-gray-800"
          }`}
          style={indent}
        >
          <button
            className={`flex min-w-0 flex-1 cursor-pointer items-center gap-1.5 py-1.5 text-left ${
              active ? "font-semibold" : ""
            }`}
            type="button"
            onClick={() => onSelect(file.id)}
          >
            <span className="flex-shrink-0 text-gray-400">
              {kindIcon(file)}
            </span>
            <span className="truncate text-[12px]">{node.name}</span>
          </button>
          <Button
            isIconOnly
            aria-label={t("删除 {0}", { "0": file.path })}
            className="text-gray-300 opacity-0 transition-opacity hover:text-danger group-hover:opacity-100"
            size="sm"
            title={t("删除文件")}
            variant="light"
            onPress={() => onDelete(file.id)}
          >
            <Delete20Regular />
          </Button>
        </div>
      );
    });

  if (tree.length === 0) {
    return (
      <div className="px-2 py-6 text-center text-[11px] leading-relaxed text-gray-400">
        {searching ? t("没有匹配的文件") : t("暂无文件，可新建或导入")}
      </div>
    );
  }

  return <div className="flex flex-col gap-0.5">{renderNodes(tree, 0)}</div>;
};

export default PackFileTree;
