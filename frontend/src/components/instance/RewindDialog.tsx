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
 * Rewind：单个目标（存档或实例）的快照时间线弹窗。
 * - 每次「新建快照」把目标目录当前状态记入时间线（后端文件级去重，未变文件不重复占盘）；
 * - 任意时间点可回滚，回滚前后端会自动追加一个「安全快照」以便反悔；
 * - 删除快照会回收不再被引用的数据块；
 * - 快照按保存日期分组，列表左侧的轨道（节点 + 连线）标出每个快照的保存时刻；
 * - 快照可标记颜色（语义色键，随明暗主题自动适配）：时间线节点与调色按钮显示该颜色；
 * - 存档与实例走同一套界面，仅后端绑定与提示文案不同（实例快照不含 libraries/assets
 *   等可重新下载目录，回滚时原样保留）；
 * - 打开与操作后的刷新对已有列表是静默的：旧列表原地换新，避免「列表 → 加载圈 →
 *   列表」的闪烁（窗口透明，全屏重绘会被明显感知）。
 */
import type { content } from "../../../wailsjs/go/models";

import React, { useCallback, useEffect, useRef, useState } from "react";
import {
  Button,
  Chip,
  Input,
  Modal,
  ModalContent,
  Popover,
  PopoverContent,
  PopoverTrigger,
  Select,
  SelectItem,
  Spinner,
  Switch,
  Tooltip,
} from "@heroui/react";
import {
  Add20Regular as AddIcon,
  ArrowClockwise20Regular as RefreshIcon,
  ArrowUndo20Regular as RollbackIcon,
  Delete20Regular as DeleteIcon,
  History20Regular as RewindIcon,
  PaintBrush20Regular as ColorIcon,
} from "@fluentui/react-icons";

import {
  CreateInstanceSnapshot,
  CreateSaveSnapshot,
  DeleteInstanceSnapshot,
  DeleteSaveSnapshot,
  ListInstanceSnapshots,
  ListSaveSnapshots,
  RollbackInstanceSnapshot,
  RollbackSaveSnapshot,
  SetInstanceSnapshotColor,
  SetSaveSnapshotColor,
} from "../../../wailsjs/go/bindings/ContentAPI";
import { ModalShell, modalBehaviorProps } from "../modal-shell";
import { confirm, notify } from "../overlay/dialog";
import { popoverMotionProps } from "../../lib/motion";
import { t } from "../../i18n";

/** Rewind 目标类型：save = 单个存档，instance = 实例游戏目录 */
export type RewindKind = "save" | "instance";

/** 两类目标的后端绑定（签名一致，按 kind 分发） */
const REWIND_APIS = {
  save: {
    list: ListSaveSnapshots,
    create: CreateSaveSnapshot,
    setColor: SetSaveSnapshotColor,
    rollback: RollbackSaveSnapshot,
    remove: DeleteSaveSnapshot,
  },
  instance: {
    list: ListInstanceSnapshots,
    create: CreateInstanceSnapshot,
    setColor: SetInstanceSnapshotColor,
    rollback: RollbackInstanceSnapshot,
    remove: DeleteInstanceSnapshot,
  },
} as const;

interface RewindDialogProps {
  isOpen: boolean;
  kind: RewindKind;
  targetPath: string | null;
  targetName: string;
  onClose: () => void;
  /** 回滚成功后通知外部刷新（目标内容已改变） */
  onRestored?: () => void;
  /**
   * 可选的存档清单：提供后（实例模式）弹窗左侧出现存档选择栏，
   * 可直接切换查看每个存档的快照时间线，不必从存档行单独打开。
   */
  saves?: content.GameContentEntry[];
}

/** 人类可读的字节数。 */
function formatBytes(bytes: number): string {
  if (!bytes || bytes <= 0) return "0 B";
  const units = ["B", "KB", "MB", "GB", "TB"];
  let value = bytes;
  let index = 0;

  while (value >= 1024 && index < units.length - 1) {
    value /= 1024;
    index += 1;
  }
  const digits = index === 0 || value >= 10 ? 0 : 1;

  return `${value.toFixed(digits)} ${units[index]}`;
}

function formatDateTime(value: unknown): string {
  if (!value) return "—";
  const date = new Date(value as string);

  return isNaN(date.getTime()) ? String(value) : date.toLocaleString();
}

/** 时间线左栏的保存时刻；完整日期悬停可见，跨天信息由轨道上的日期标签承担。 */
function formatClock(value: unknown): string {
  const date = new Date(value as string);

  return isNaN(date.getTime())
    ? "—"
    : date.toLocaleTimeString(undefined, { hour12: false });
}

function sameDay(a: Date, b: Date): boolean {
  return (
    a.getFullYear() === b.getFullYear() &&
    a.getMonth() === b.getMonth() &&
    a.getDate() === b.getDate()
  );
}

/** 日期标签：今天 / 昨天 / 今年用「M 月 D 日」，跨年补上年份。 */
function formatDayLabel(value: unknown, now: Date): string {
  const date = new Date(value as string);

  if (isNaN(date.getTime())) return "—";
  if (sameDay(date, now)) return t("今天");

  const yesterday = new Date(now);

  yesterday.setDate(yesterday.getDate() - 1);
  if (sameDay(date, yesterday)) return t("昨天");

  const month = date.getMonth() + 1;
  const day = date.getDate();

  return date.getFullYear() === now.getFullYear()
    ? t("{0} 月 {1} 日", { "0": month, "1": day })
    : t("{0} 年 {1} 月 {2} 日", {
        "0": date.getFullYear(),
        "1": month,
        "2": day,
      });
}

interface SnapshotGroup {
  key: string;
  label: string;
  items: content.SaveSnapshot[];
}

/** 按本地日历日把（后端已按时间倒序的）快照分组，供时间线按天分段展示。 */
function groupSnapshotsByDay(
  snapshots: content.SaveSnapshot[],
  now: Date,
): SnapshotGroup[] {
  const groups: SnapshotGroup[] = [];

  for (const snapshot of snapshots) {
    const date = new Date(snapshot.CreatedAt as string);
    const key = isNaN(date.getTime())
      ? "unknown"
      : `${date.getFullYear()}-${date.getMonth()}-${date.getDate()}`;
    const current = groups[groups.length - 1];

    if (current?.key === key) current.items.push(snapshot);
    else
      groups.push({
        key,
        label: formatDayLabel(snapshot.CreatedAt, now),
        items: [snapshot],
      });
  }

  return groups;
}

/** 时间线行三段栅格：保存时刻 | 轨道（节点 + 连线）| 快照内容。 */
const TIMELINE_GRID = "grid grid-cols-[52px_16px_minmax(0,1fr)]";
const TIMELINE_LINE =
  "absolute left-1/2 w-px -translate-x-1/2 bg-gray-200 dark:bg-white/10";

/** 快照标记颜色可选值（语义色键，空串 = 默认色；与后端 saveSnapshotColors 对应）。
 *  类名必须写全（Tailwind 静态扫描），不可拼接。 */
const SNAPSHOT_COLOR_OPTIONS = [
  "primary",
  "secondary",
  "success",
  "warning",
  "danger",
] as const;

const SNAPSHOT_DOT_CLASSES: Record<string, string> = {
  "": "border-2 border-dashed border-gray-300 bg-transparent dark:border-gray-600",
  primary: "bg-primary",
  secondary: "bg-secondary",
  success: "bg-success",
  warning: "bg-warning",
  danger: "bg-danger",
};

const SNAPSHOT_NODE_CLASSES: Record<string, string> = {
  "": "bg-primary ring-primary/10",
  primary: "bg-primary ring-primary/10",
  secondary: "bg-secondary ring-secondary/10",
  success: "bg-success ring-success/10",
  warning: "bg-warning ring-warning/10",
  danger: "bg-danger ring-danger/10",
};

/** 色板里的单个色块。 */
const ColorSwatch: React.FC<{
  value: string;
  current: string;
  onSelect: (color: string) => void;
}> = ({ value, current, onSelect }) => (
  <button
    aria-label={value === "" ? t("默认") : value}
    className={`size-6 cursor-pointer rounded-full transition-transform hover:scale-110 ${
      SNAPSHOT_DOT_CLASSES[value]
    } ${
      current === value
        ? "ring-2 ring-primary ring-offset-2 ring-offset-content1"
        : ""
    }`}
    title={value === "" ? t("默认") : undefined}
    type="button"
    onClick={() => onSelect(value)}
  />
);

/** 快照标记颜色选择器：圆点触发钮 + 色板弹层（选中即关）。 */
const ColorPicker: React.FC<{
  color: string;
  showDefault: boolean;
  isDisabled?: boolean;
  onSelect: (color: string) => void;
}> = ({ color, showDefault, isDisabled, onSelect }) => {
  const [open, setOpen] = useState(false);

  return (
    <Popover isOpen={open} placement="bottom" onOpenChange={setOpen}>
      <PopoverTrigger>
        <Button
          isIconOnly
          aria-label={t("标记颜色")}
          isDisabled={isDisabled}
          size="sm"
          title={t("标记颜色")}
          variant="flat"
        >
          {color === "" ? (
            <ColorIcon />
          ) : (
            <span
              className={`size-3.5 rounded-full ${SNAPSHOT_DOT_CLASSES[color]}`}
            />
          )}
        </Button>
      </PopoverTrigger>
      <PopoverContent>
        <div className="flex items-center gap-1.5 p-2">
          {showDefault ? (
            <ColorSwatch
              current={color}
              value=""
              onSelect={(value) => {
                onSelect(value);
                setOpen(false);
              }}
            />
          ) : null}
          {SNAPSHOT_COLOR_OPTIONS.map((option) => (
            <ColorSwatch
              key={option}
              current={color}
              value={option}
              onSelect={(value) => {
                onSelect(value);
                setOpen(false);
              }}
            />
          ))}
        </div>
      </PopoverContent>
    </Popover>
  );
};

/** 快照定时自动创建的可选间隔（分钟）。 */
const AUTO_INTERVALS = [1, 5, 15, 30];
/** 定时快照的间隔偏好（跨会话记住）。 */
const AUTO_PREF_KEY = "nekolauncher-rewind-auto";

function loadAutoMinutes(): number {
  try {
    const raw = Number(localStorage.getItem(AUTO_PREF_KEY));

    return AUTO_INTERVALS.includes(raw) ? raw : 5;
  } catch {
    return 5;
  }
}

const RewindDialog: React.FC<RewindDialogProps> = ({
  isOpen,
  kind,
  targetPath,
  targetName,
  onClose,
  onRestored,
  saves,
}) => {
  // 存档选择栏的当前选中：null = 整个实例（仅实例模式且传入存档清单时可选存档）
  const [selectedSavePath, setSelectedSavePath] = useState<string | null>(null);
  // 定时快照：开关 + 间隔（弹窗开着期间按间隔对当前目标自动创建）
  const [autoOn, setAutoOn] = useState(false);
  const [autoMinutes, setAutoMinutes] = useState<number>(loadAutoMinutes);
  const autoBusyRef = useRef(false);
  const showSavePicker = kind === "instance" && !!saves?.length;
  const activeKind: RewindKind =
    kind === "instance" && selectedSavePath ? "save" : kind;
  const activeSave = saves?.find((s) => s.SourcePath === selectedSavePath);
  const activePath = selectedSavePath ?? targetPath;
  const activeName = activeSave?.Name ?? targetName;
  const apis = REWIND_APIS[activeKind];
  const [snapshots, setSnapshots] = useState<content.SaveSnapshot[]>([]);
  /** snapshots 所属的目标路径：换目标或未加载时不展示旧数据，防止闪错列表 */
  const [snapshotOwner, setSnapshotOwner] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  const [busy, setBusy] = useState("");
  const [label, setLabel] = useState("");
  /** 新快照的标记颜色（空串 = 默认色） */
  const [color, setColor] = useState("");
  /** snapshotOwner 的非响应式镜像：打开时据此决定静默刷新，避免进 effect 依赖造成二次拉取 */
  const ownerRef = useRef<string | null>(null);
  /** 上一次 isOpen 的镜像：打开瞬间把视角重置为「第一个存档」（无存档则整个实例） */
  const wasOpenRef = useRef(false);

  const load = useCallback(
    async (path: string, silent = false) => {
      if (!silent) setLoading(true);
      try {
        setSnapshots(await apis.list(path));
        setSnapshotOwner(path);
        ownerRef.current = path;
      } catch (ex) {
        notify.error(
          t("读取快照失败：{0}", { "0": (ex as Error)?.message ?? ex }),
        );
      } finally {
        setLoading(false);
      }
    },
    [apis],
  );

  useEffect(() => {
    // 每次打开：有存档清单时直接定位到第一个存档，否则看整个实例
    if (isOpen && !wasOpenRef.current) {
      setSelectedSavePath(saves?.[0]?.SourcePath ?? null);
    }
    wasOpenRef.current = isOpen;
    if (!isOpen) return;
    if (!activePath) {
      setSnapshots([]);
      setSnapshotOwner(null);
      ownerRef.current = null;

      return;
    }
    // 同一目标已有列表时静默刷新；首次打开（无数据）才显示加载圈
    void load(activePath, ownerRef.current === activePath);
    // eslint-disable-next-line react-hooks/exhaustive-deps -- saves 只在打开瞬间读一次（默认定位第一个存档），不作为重启条件
  }, [isOpen, activePath, load]);

  const create = async () => {
    if (!activePath) return;
    setBusy("create");
    try {
      const created = await apis.create(activePath, label.trim(), color);

      setLabel("");
      setColor("");
      notify.success(
        t("快照已创建：{0}", { "0": formatDateTime(created.CreatedAt) }),
      );
      await load(activePath, true);
    } catch (ex) {
      notify.error(
        t("创建快照失败：{0}", { "0": (ex as Error)?.message ?? ex }),
      );
    } finally {
      setBusy("");
    }
  };

  /** 存档选择栏里的快捷创建：不切换视角，直接给指定存档拍一份快照。 */
  const quickCreate = async (save: content.GameContentEntry) => {
    if (busy || autoBusyRef.current) return;
    setBusy(`quick:${save.SourcePath}`);
    try {
      const created = await REWIND_APIS.save.create(save.SourcePath, "", "");

      notify.success(
        t("已为存档「{0}」创建快照：{1}", {
          "0": save.Name,
          "1": formatDateTime(created.CreatedAt),
        }),
      );
      // 正看着这个存档的时间线就原地刷新，否则无需动列表
      if (activePath === save.SourcePath) await load(activePath, true);
    } catch (ex) {
      notify.error(
        t("创建快照失败：{0}", { "0": (ex as Error)?.message ?? ex }),
      );
    } finally {
      setBusy("");
    }
  };

  // ---------- 定时快照 ----------
  // 弹窗打开期间按所选间隔对当前激活目标自动创建（备注"定时快照"、默认色）。
  // 上一份还没拍完时跳过本轮，避免堆积。

  useEffect(() => {
    try {
      localStorage.setItem(AUTO_PREF_KEY, String(autoMinutes));
    } catch {
      /* 偏好写不进去就仅本次生效 */
    }
  }, [autoMinutes]);

  useEffect(() => {
    if (!isOpen || !autoOn || !activePath) return;
    const timer = window.setInterval(() => {
      if (autoBusyRef.current || busy !== "") return;
      autoBusyRef.current = true;
      void apis
        .create(activePath, t("定时快照"), "")
        .then((created) => {
          notify.success(
            t("定时快照已创建：{0}", {
              "0": formatDateTime(created.CreatedAt),
            }),
          );

          return load(activePath, true);
        })
        .catch(() => {
          /* 自动快照失败不打扰用户，下一轮再试 */
        })
        .finally(() => {
          autoBusyRef.current = false;
        });
    }, autoMinutes * 60_000);

    return () => window.clearInterval(timer);
    // eslint-disable-next-line react-hooks/exhaustive-deps -- busy 用最新值判断即可，不作为重启条件
  }, [isOpen, autoOn, autoMinutes, activePath, apis, load]);

  /** 更新已有快照的标记颜色（静默刷新，列表原地换色）。 */
  const updateColor = async (snapshot: content.SaveSnapshot, next: string) => {
    if (!activePath || snapshot.Color === next) return;
    setBusy(snapshot.Id);
    try {
      await apis.setColor(activePath, snapshot.Id, next);
      await load(activePath, true);
    } catch (ex) {
      notify.error(
        t("设置颜色失败：{0}", { "0": (ex as Error)?.message ?? ex }),
      );
    } finally {
      setBusy("");
    }
  };

  const rollback = async (snapshot: content.SaveSnapshot) => {
    if (!activePath) return;
    const ok = await confirm(
      t("回滚到此快照"),
      t(
        "将用该快照覆盖当前内容。回滚前会自动创建一个安全快照，可随时再回滚回来。",
      ),
      { confirmLabel: t("回滚"), severity: "warning" },
    );

    if (!ok) return;
    setBusy(snapshot.Id);
    try {
      await apis.rollback(activePath, snapshot.Id);
      notify.success(t("已回滚到所选快照。"));
      onRestored?.();
      await load(activePath, true);
    } catch (ex) {
      notify.error(t("回滚失败：{0}", { "0": (ex as Error)?.message ?? ex }));
    } finally {
      setBusy("");
    }
  };

  const remove = async (snapshot: content.SaveSnapshot) => {
    if (!activePath) return;
    const ok = await confirm(
      t("删除快照"),
      t("删除后该时间点的快照将不可用，已无引用的数据块会被回收。"),
      { confirmLabel: t("删除"), severity: "warning" },
    );

    if (!ok) return;
    setBusy(snapshot.Id);
    try {
      await apis.remove(activePath, snapshot.Id);
      notify.success(t("快照已删除。"));
      await load(activePath, true);
    } catch (ex) {
      notify.error(t("删除失败：{0}", { "0": (ex as Error)?.message ?? ex }));
    } finally {
      setBusy("");
    }
  };

  // 只有归属当前目标的数据才可展示；静默刷新期间旧列表保持可见
  const owned = snapshotOwner === activePath ? snapshots : [];
  const groups = groupSnapshotsByDay(owned, new Date());
  const lastId = owned[owned.length - 1]?.Id ?? "";

  return (
    <Modal
      isOpen={isOpen}
      size={showSavePicker ? "2xl" : "lg"}
      onClose={onClose}
      {...modalBehaviorProps}
    >
      <ModalContent>
        <ModalShell
          closeGuard={() => busy === ""}
          icon={<RewindIcon />}
          subtitle={
            activeKind === "save"
              ? t("存档快照时间线 · {0}", { "0": activeName })
              : t("实例快照时间线 · {0}", { "0": activeName })
          }
          title="Rewind"
          onClose={onClose}
        >
          <div
            className={showSavePicker ? "flex gap-4" : "flex flex-col gap-3"}
          >
            {/* 存档选择栏：实例模式下可切换查看每个存档的时间线 */}
            {showSavePicker ? (
              <div className="nya-scroll w-48 flex-none overflow-y-auto rounded-xl border nya-border bg-default-100/40 p-1.5">
                <div className="px-2 pb-1 pt-1 text-[10px] font-medium text-gray-400">
                  {t("快照目标")}
                </div>
                <button
                  className={`block w-full truncate rounded-lg px-2.5 py-1.5 text-left text-[13px] transition-colors ${
                    selectedSavePath === null
                      ? "bg-primary/15 font-semibold text-primary"
                      : "text-gray-700 hover:bg-default-200/60 dark:text-gray-200"
                  }`}
                  type="button"
                  onClick={() => setSelectedSavePath(null)}
                >
                  {t("整个实例")}
                </button>
                {(saves ?? []).map((save) => {
                  const quickBusy = busy === `quick:${save.SourcePath}`;

                  return (
                    <div
                      key={save.SourcePath}
                      className={`group flex items-center rounded-lg transition-colors ${
                        selectedSavePath === save.SourcePath
                          ? "bg-primary/15"
                          : "hover:bg-default-200/60"
                      }`}
                    >
                      <button
                        className={`min-w-0 flex-1 truncate px-2.5 py-1.5 text-left text-[13px] ${
                          selectedSavePath === save.SourcePath
                            ? "font-semibold text-primary"
                            : "text-gray-700 dark:text-gray-200"
                        }`}
                        title={save.Name}
                        type="button"
                        onClick={() => setSelectedSavePath(save.SourcePath)}
                      >
                        {save.Name}
                      </button>
                      <Tooltip content={t("为该存档快捷创建快照")} delay={200}>
                        <Button
                          isIconOnly
                          aria-label={t("为该存档快捷创建快照")}
                          className="mr-0.5 h-6 min-w-6 w-6 flex-none opacity-0 group-hover:opacity-100"
                          isDisabled={!!busy || quickBusy}
                          isLoading={quickBusy}
                          size="sm"
                          variant="light"
                          onPress={() => void quickCreate(save)}
                        >
                          {!quickBusy ? (
                            <AddIcon className="h-3.5 w-3.5" />
                          ) : null}
                        </Button>
                      </Tooltip>
                    </div>
                  );
                })}
              </div>
            ) : null}

            <div
              className={`flex min-w-0 flex-1 flex-col ${showSavePicker ? "" : "gap-3"}`}
            >
              <div className="flex items-center gap-2">
                <Input
                  className="min-w-0 flex-1"
                  placeholder={t("快照备注（可选）")}
                  size="sm"
                  value={label}
                  variant="bordered"
                  onValueChange={setLabel}
                />
                <ColorPicker
                  color={color}
                  showDefault={false}
                  onSelect={setColor}
                />
                <Button
                  color="primary"
                  isLoading={busy === "create"}
                  size="sm"
                  startContent={<AddIcon />}
                  onPress={() => void create()}
                >
                  {t("新建快照")}
                </Button>
                <Button
                  isIconOnly
                  aria-label={t("刷新")}
                  isDisabled={loading}
                  isLoading={loading}
                  size="sm"
                  variant="flat"
                  onPress={() => {
                    if (activePath) void load(activePath);
                  }}
                >
                  <RefreshIcon />
                </Button>
              </div>

              <div className="text-[11px] text-gray-400">
                {t(
                  "每次快照只保存与上一份不同的文件，回滚前会自动创建安全快照。",
                )}
              </div>

              {/* 定时快照：弹窗打开期间按间隔对当前目标自动创建 */}
              <div className="flex items-center gap-2.5 rounded-xl border nya-border bg-default-100/40 px-3 py-2">
                <Switch
                  aria-label={t("定时创建快照")}
                  isSelected={autoOn}
                  size="sm"
                  onValueChange={setAutoOn}
                />
                <div className="min-w-0 flex-1">
                  <div className="text-[12px] font-medium text-gray-700 dark:text-gray-200">
                    {t("定时创建快照")}
                  </div>
                  <div className="text-[10px] text-gray-400">
                    {autoOn
                      ? t(
                          "每 {0} 分钟自动为当前目标创建一份快照（备注「定时快照」）。",
                          {
                            "0": autoMinutes,
                          },
                        )
                      : t("开启后按固定间隔自动创建快照，适合边玩边备份。")}
                  </div>
                </div>
                <Select
                  disallowEmptySelection
                  aria-label={t("快照间隔")}
                  className="w-28 flex-none"
                  items={AUTO_INTERVALS.map((minutes) => ({
                    key: String(minutes),
                    label: t("{0} 分钟", { "0": minutes }),
                  }))}
                  popoverProps={{ motionProps: popoverMotionProps }}
                  selectedKeys={[String(autoMinutes)]}
                  size="sm"
                  onSelectionChange={(keys) => {
                    const value = Number(Array.from(keys)[0] ?? 5);

                    if (AUTO_INTERVALS.includes(value)) setAutoMinutes(value);
                  }}
                >
                  {(item) => (
                    <SelectItem key={item.key}>{item.label}</SelectItem>
                  )}
                </Select>
              </div>

              <div className="text-[11px] text-gray-400">
                {t(
                  "回滚前自动创建的安全快照只保留最近若干个（按体积预算自动清理最旧的无标记快照）；带备注或标记颜色的快照不会被自动清理。",
                )}
              </div>

              {activeKind === "instance" ? (
                <div className="text-[11px] text-gray-400">
                  {t(
                    "实例快照不包含 libraries、assets 等可重新下载的目录；回滚时这些目录会原样保留，不被改动。",
                  )}
                </div>
              ) : null}

              {/* 三态共用固定高度：弹窗尺寸从打开那一刻起稳定，不随加载/换页跳动 */}
              {loading && owned.length === 0 ? (
                <div className="flex h-[420px] items-center justify-center gap-2 text-sm text-gray-400">
                  <Spinner size="sm" /> {t("正在读取快照…")}
                </div>
              ) : !loading && owned.length === 0 ? (
                <div className="flex h-[420px] items-center justify-center text-sm text-gray-400">
                  {t("暂无快照")}
                </div>
              ) : (
                <ol className="max-h-[420px] overflow-y-auto pb-4 pt-1 pr-1">
                  {groups.map((group, groupIndex) => (
                    <React.Fragment key={group.key}>
                      {groupIndex > 0 ? (
                        <li className={`${TIMELINE_GRID} h-9`}>
                          <div />
                          <div className="relative">
                            <span className={`${TIMELINE_LINE} top-0 h-1/2`} />
                            <span
                              className={`${TIMELINE_LINE} bottom-0 h-1/2`}
                            />
                            <span className="absolute left-1/2 top-1/2 z-10 -translate-x-1/2 -translate-y-1/2 whitespace-nowrap rounded-full bg-primary px-2.5 py-0.5 text-[10px] font-semibold text-primary-foreground shadow-sm shadow-primary/30">
                              {group.label}
                            </span>
                          </div>
                          <div />
                        </li>
                      ) : null}
                      {group.items.map((snapshot) => {
                        const isSafety = snapshot.Reason === "before-rollback";
                        const isLast = snapshot.Id === lastId;

                        return (
                          <li key={snapshot.Id} className={TIMELINE_GRID}>
                            <div className="pt-2 text-right">
                              <span
                                className="text-[11px] font-semibold leading-5 tabular-nums text-gray-600 dark:text-gray-400"
                                title={formatDateTime(snapshot.CreatedAt)}
                              >
                                {formatClock(snapshot.CreatedAt)}
                              </span>
                            </div>
                            <div className="relative">
                              <span
                                className={`${TIMELINE_LINE} ${
                                  isLast ? "top-0 h-[19px]" : "inset-y-0"
                                }`}
                              />
                              <span
                                className={`absolute left-1/2 top-[14px] size-[9px] -translate-x-1/2 rounded-full ring-4 ${
                                  SNAPSHOT_NODE_CLASSES[
                                    snapshot.Color ||
                                      (isSafety ? "warning" : "")
                                  ]
                                } ${busy === snapshot.Id ? "animate-pulse" : ""}`}
                              />
                            </div>
                            <div className="flex min-w-0 items-center gap-3 rounded-lg py-2 pl-2.5 pr-3 transition-colors hover:bg-primary/10">
                              <div className="flex min-w-0 flex-1 flex-col gap-0.5">
                                {isSafety || snapshot.Label ? (
                                  <div className="flex flex-wrap items-center gap-2">
                                    {isSafety ? (
                                      <Chip
                                        color="warning"
                                        size="sm"
                                        variant="flat"
                                      >
                                        {t("安全快照")}
                                      </Chip>
                                    ) : null}
                                    {snapshot.Label ? (
                                      <span className="min-w-0 truncate text-[13px] font-semibold text-gray-800 dark:text-gray-200">
                                        {snapshot.Label}
                                      </span>
                                    ) : null}
                                  </div>
                                ) : null}
                                <div className="truncate text-[10px] text-gray-400">
                                  {t("{0} 个文件 · 共 {1}", {
                                    "0": snapshot.FileCount,
                                    "1": formatBytes(snapshot.TotalSize),
                                  })}
                                  {" · "}
                                  {t("新增 {0} 个文件 / {1}", {
                                    "0": snapshot.AddedFiles,
                                    "1": formatBytes(snapshot.AddedSize),
                                  })}
                                </div>
                              </div>
                              <div className="flex flex-shrink-0 items-center gap-1.5">
                                <ColorPicker
                                  showDefault
                                  color={snapshot.Color}
                                  isDisabled={busy === snapshot.Id}
                                  onSelect={(next) =>
                                    void updateColor(snapshot, next)
                                  }
                                />
                                <Button
                                  isLoading={busy === snapshot.Id}
                                  size="sm"
                                  startContent={<RollbackIcon />}
                                  variant="flat"
                                  onPress={() => void rollback(snapshot)}
                                >
                                  {t("回滚")}
                                </Button>
                                <Button
                                  isIconOnly
                                  aria-label={t("删除快照")}
                                  color="danger"
                                  isDisabled={busy === snapshot.Id}
                                  size="sm"
                                  variant="flat"
                                  onPress={() => void remove(snapshot)}
                                >
                                  <DeleteIcon />
                                </Button>
                              </div>
                            </div>
                          </li>
                        );
                      })}
                    </React.Fragment>
                  ))}
                </ol>
              )}
            </div>
          </div>
        </ModalShell>
      </ModalContent>
    </Modal>
  );
};

export default RewindDialog;
