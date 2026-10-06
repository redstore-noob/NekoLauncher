/*
 * 侧边栏设置分区：自动隐藏（鼠标贴窗口左缘自动弹出）+ 双盒子拖拽管理页面。
 *
 * 左盒是"显示"（可拖动排序），右盒是"隐藏"；跨盒拖动即隐藏/恢复显示。
 * 拖拽用原生 HTML5 DnD：dragover 时维护 preview（实时预览位置，插到目标
 * 页之前/之后/末尾），drop 时一次性提交 applySidebarLayout 持久化。
 * 主页、外观与设置是导航的兜底入口，可以排序但拖不进"隐藏"盒。
 */
import React, { useState } from "react";
import { Switch } from "@heroui/react";
import {
  ArrowUp20Regular,
  ArrowDown20Regular,
  ArrowLeft20Regular,
  ArrowRight20Regular,
  ReOrderDotsVertical20Regular,
} from "@fluentui/react-icons";

import { SegmentedTabs } from "../../components/segmented-tabs";
import { usePages, type PageDefinition } from "../../plugin";
import { useI18n } from "../../i18n";
import { useSimpleMode } from "../simple-mode";
import {
  PROTECTED_SIDEBAR_PAGES,
  orderPages,
  useSidebarSettings,
  type SidebarPlacement,
  type SidebarStyle,
} from "../sidebar-settings";
import Section, { SettingRow } from "../settings/Section";

type BoxId = "visible" | "hidden";

/** 拖动中的实时预览位置：插到 beforeId 之前，null 表示追加到末尾 */
interface DragPreview {
  id: string;
  to: BoxId;
  beforeId: string | null;
}

const SidebarSection: React.FC = () => {
  const { t } = useI18n();
  // 插件页注册晚于首帧也没关系：usePages 订阅注册表，列表会自动跟上
  const pages = usePages();
  const {
    hiddenPages,
    pageOrder,
    autoHide,
    placement,
    style,
    applySidebarLayout,
    setAutoHide,
    setPlacement,
    setStyle,
  } = useSidebarSettings();
  const { simpleMode } = useSimpleMode();

  // 按"保存顺序 → 注册表 order"排好后拆成两盒的基础列表
  const ordered = orderPages(pages, pageOrder);
  const baseVisible = ordered
    .filter((page) => !hiddenPages.has(page.id))
    .map((page) => page.id);
  const baseHidden = ordered
    .filter((page) => hiddenPages.has(page.id))
    .map((page) => page.id);
  const pageById = new Map<string, PageDefinition>(
    ordered.map((page) => [page.id, page]),
  );

  const isProtected = (id: string) => PROTECTED_SIDEBAR_PAGES.has(id);

  const [dragId, setDragId] = useState<string | null>(null);
  const [preview, setPreview] = useState<DragPreview | null>(null);

  const endDrag = () => {
    setDragId(null);
    setPreview(null);
  };

  const withoutId = (ids: string[], id: string) =>
    ids.filter((item) => item !== id);
  const insertBefore = (ids: string[], id: string, beforeId: string | null) => {
    const rest = withoutId(ids, id);
    const index = beforeId == null ? -1 : rest.indexOf(beforeId);

    return index === -1
      ? [...rest, id]
      : [...rest.slice(0, index), id, ...rest.slice(index)];
  };

  // 实时预览：把拖拽项从两盒都移除，再插到预览目标位置
  let visibleIds = baseVisible;
  let hiddenIds = baseHidden;

  if (dragId && preview) {
    visibleIds =
      preview.to === "visible"
        ? insertBefore(baseVisible, preview.id, preview.beforeId)
        : withoutId(baseVisible, preview.id);
    hiddenIds =
      preview.to === "hidden"
        ? insertBefore(baseHidden, preview.id, preview.beforeId)
        : withoutId(baseHidden, preview.id);
  }

  const handleItemDragStart = (event: React.DragEvent, id: string) => {
    event.dataTransfer.effectAllowed = "move";
    event.dataTransfer.setData("text/plain", id);
    setDragId(id);
  };

  // item 上的 dragover：按鼠标在目标的上/下半决定插到它前面还是后面，
  // 并 stopPropagation 避免触达盒子的"追加到末尾"兜底
  const handleItemDragOver = (
    event: React.DragEvent,
    targetId: string,
    box: BoxId,
    ids: string[],
  ) => {
    if (!dragId) return;
    event.preventDefault();
    event.stopPropagation();
    if (dragId === targetId) return;
    if (box === "hidden" && isProtected(dragId)) {
      event.dataTransfer.dropEffect = "none";

      return;
    }
    event.dataTransfer.dropEffect = "move";
    const rect = event.currentTarget.getBoundingClientRect();
    const beforeTarget = event.clientY < rect.top + rect.height / 2;
    const beforeId = beforeTarget
      ? targetId
      : (ids[ids.indexOf(targetId) + 1] ?? null);

    setPreview((prev) =>
      prev &&
      prev.id === dragId &&
      prev.to === box &&
      prev.beforeId === beforeId
        ? prev
        : { id: dragId, to: box, beforeId },
    );
  };

  // 盒子空白处的 dragover：追加到末尾（item 已 stopPropagation，不会到这里）
  const handleBoxDragOver = (event: React.DragEvent, box: BoxId) => {
    if (!dragId) return;
    event.preventDefault();
    if (box === "hidden" && isProtected(dragId)) {
      event.dataTransfer.dropEffect = "none";

      return;
    }
    event.dataTransfer.dropEffect = "move";
    setPreview((prev) =>
      prev && prev.id === dragId && prev.to === box && prev.beforeId === null
        ? prev
        : { id: dragId, to: box, beforeId: null },
    );
  };

  const handleDrop = (event: React.DragEvent) => {
    event.preventDefault();
    if (dragId && preview) {
      const nextVisible = withoutId(baseVisible, dragId);
      const nextHidden = withoutId(baseHidden, dragId);
      const target = preview.to === "visible" ? nextVisible : nextHidden;
      const index =
        preview.beforeId == null ? -1 : target.indexOf(preview.beforeId);

      if (index === -1) target.push(dragId);
      else target.splice(index, 0, dragId);
      applySidebarLayout(nextVisible, nextHidden);
    }
    endDrag();
  };

  const renderItem = (id: string, box: BoxId, ids: string[]) => {
    const page = pageById.get(id);

    if (!page) return null;

    return (
      <li key={id}>
        <div
          draggable
          aria-label={t("拖动调整「{label}」", { label: t(page.label) })}
          className={`nya-panel-inner nya-border flex cursor-grab select-none items-center gap-1.5 rounded-medium border px-2 py-1.5 text-sm text-gray-800 active:cursor-grabbing dark:text-gray-200 ${
            dragId === id ? "opacity-50 ring-1 ring-primary/40" : ""
          }`}
          onDragEnd={endDrag}
          onDragOver={(event) => handleItemDragOver(event, id, box, ids)}
          onDragStart={(event) => handleItemDragStart(event, id)}
        >
          <ReOrderDotsVertical20Regular className="flex-shrink-0 text-gray-400" />
          <span className="flex-shrink-0 [&>svg]:w-5">{page.icon}</span>
          <span className="truncate">{t(page.label)}</span>
        </div>
      </li>
    );
  };

  const renderBox = (
    title: string,
    box: BoxId,
    ids: string[],
    emptyText: string,
  ) => (
    <div
      className={`nya-panel nya-border min-w-[200px] flex-1 rounded-large border p-2 transition-shadow ${
        dragId && preview?.to === box ? "ring-2 ring-primary/40" : ""
      }`}
      onDragOver={(event) => handleBoxDragOver(event, box)}
      onDrop={handleDrop}
    >
      <div className="mb-2 flex items-center justify-between px-1">
        <span className="text-xs font-medium text-gray-500 dark:text-gray-400">
          {t(title)}
        </span>
        <span className="text-xs text-gray-400">
          {t("{count} 项", { count: ids.length })}
        </span>
      </div>
      <ul className="min-h-[96px] space-y-1">
        {ids.map((id) => renderItem(id, box, ids))}
        {ids.length === 0 && (
          <li className="nya-border flex h-[88px] items-center justify-center rounded-lg border border-dashed text-xs text-gray-400">
            {t(emptyText)}
          </li>
        )}
      </ul>
    </div>
  );

  return (
    <Section
      aliases={[
        t("侧边栏"),
        t("侧栏"),
        t("标签"),
        t("页面"),
        t("排序"),
        t("隐藏"),
        t("自动隐藏"),
        t("停靠"),
        t("位置"),
        t("形态"),
        t("岛式"),
        t("陆式"),
        "sidebar",
      ]}
      title={t("侧边栏")}
    >
      <SettingRow hint={t("鼠标移到窗口对应边缘时弹出")} label={t("自动隐藏")}>
        <Switch
          aria-label={t("自动隐藏侧边栏")}
          color="primary"
          isSelected={autoHide}
          size="sm"
          onValueChange={setAutoHide}
        />
      </SettingRow>

      <SettingRow label={t("停靠位置")}>
        <SegmentedTabs
          items={[
            { key: "left", label: <ArrowLeft20Regular /> },
            { key: "top", label: <ArrowUp20Regular /> },
            { key: "right", label: <ArrowRight20Regular /> },
            { key: "bottom", label: <ArrowDown20Regular /> },
          ]}
          layoutId="nya-sidebar-placement"
          value={placement}
          onChange={(value) => setPlacement(value as SidebarPlacement)}
        />
      </SettingRow>

      {/* 岛式 = 悬浮面板（圆角 + 边距 + 投影）；陆式 = 贴着停靠边与窗口连成
          一体（无圆角投影，只留朝内容区的一条边线） */}
      <SettingRow
        hint={t("岛式悬浮于窗口内，陆式贴边连成一体")}
        label={t("形态")}
      >
        <SegmentedTabs
          items={[
            { key: "island", label: t("岛式") },
            { key: "land", label: t("陆式") },
          ]}
          layoutId="nya-sidebar-style"
          value={style}
          onChange={(value) => setStyle(value as SidebarStyle)}
        />
      </SettingRow>

      {simpleMode ? (
        <div className="nya-border rounded-lg border border-dashed p-3 text-xs text-gray-400">
          {t(
            "NekoLauncher-S 模式已开启：侧边栏固定为五个页面，页面管理暂时不可用。关闭 S 模式后可继续调整。",
          )}
        </div>
      ) : (
        <>
          <div className="pt-1 text-xs text-gray-400">
            {t(
              "拖动标签可调整顺序；拖进「隐藏」盒可在侧边栏隐藏（主页、外观与设置不可隐藏）",
            )}
          </div>
          <div className="flex flex-wrap gap-3">
            {renderBox(
              t("显示"),
              "visible",
              visibleIds,
              t("侧边栏至少会保留主页、外观与设置"),
            )}
            {renderBox(t("隐藏"), "hidden", hiddenIds, t("拖动标签到这里隐藏"))}
          </div>
        </>
      )}
    </Section>
  );
};

export default SidebarSection;
