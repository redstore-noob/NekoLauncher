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
import type { content } from "../../../wailsjs/go/models";

import React, { useCallback, useEffect, useState } from "react";
import { Button, Chip, Spinner } from "@heroui/react";
import {
  Add20Regular as AddIcon,
  ArrowUndo20Regular as RollbackIcon,
  Delete20Regular as DeleteIcon,
  History20Regular,
} from "@fluentui/react-icons";

import {
  CreateSaveSnapshot,
  DeleteSaveSnapshot,
  ListSaveSnapshots,
  RewindSummary,
  RollbackSaveSnapshot,
} from "../../../wailsjs/go/bindings/ContentAPI";
import {
  GetCurrentInstanceSnapshot,
  GetVersionDetails,
} from "../../../wailsjs/go/bindings/InstanceAPI";
import { errorMessage, formatBytes } from "../../lib/home";
import { asObject } from "../../lib/guards";
import { navigateToPage } from "../../lib/navigation";
import { confirm, notify } from "../overlay/dialog";
import { t } from "../../i18n";

import HomeCard from "./HomeCard";

/** Rewind 汇总的后端载荷（对应 content.RewindSummary） */
interface RewindSummaryPayload {
  SnapshotCount: number;
  BlobBytes: number;
  BudgetBytes: number;
  LastSnapshotAt: unknown;
}

/** 左列的一个条目：某个实例下的一个存档。 */
interface SaveItem {
  instanceId: string;
  name: string;
  sourcePath: string;
}

function formatClock(value: unknown): string {
  const date = new Date(value as string);

  return isNaN(date.getTime())
    ? "—"
    : date.toLocaleString(undefined, {
        month: "numeric",
        day: "numeric",
        hour: "2-digit",
        minute: "2-digit",
        hour12: false,
      });
}

/** 存档清单轮询间隔：实例扫描 + 逐实例读详情是文件 IO，60 秒足够。 */
const SAVES_POLL_INTERVAL_MS = 60000;

/**
 * 时光回溯卡片（双栏）：左列是所有实例的存档聚合列表，右列是选中存档的
 * 全部快照，支持就地新建 / 回滚（自动安全快照）/ 删除；顶部汇总数字与
 * 「管理」入口保留。
 */
const RewindCard: React.FC = () => {
  const [summary, setSummary] = useState<RewindSummaryPayload | null>(null);
  const [saves, setSaves] = useState<SaveItem[]>([]);
  const [selected, setSelected] = useState<SaveItem | null>(null);
  const [snapshots, setSnapshots] = useState<content.SaveSnapshot[]>([]);
  const [loadingSaves, setLoadingSaves] = useState(false);
  const [loadingSnaps, setLoadingSnaps] = useState(false);
  const [busy, setBusy] = useState("");

  const loadSummary = useCallback(async () => {
    try {
      const next = asObject(
        await RewindSummary(),
      ) as RewindSummaryPayload | null;

      if (next) setSummary(next);
    } catch {
      /* 汇总读取失败不打扰主页 */
    }
  }, []);

  const loadSaves = useCallback(async () => {
    setLoadingSaves(true);
    try {
      const snap = await GetCurrentInstanceSnapshot();
      const ids = snap?.VersionIds ?? [];
      const items: SaveItem[] = [];

      for (const id of ids) {
        try {
          const details = await GetVersionDetails(id);

          for (const save of details.Saves ?? []) {
            if (!save.SourcePath) continue;
            items.push({
              instanceId: id,
              name: save.Name,
              sourcePath: save.SourcePath,
            });
          }
        } catch {
          /* 单个实例读失败不阻塞其它实例 */
        }
      }
      setSaves(items);
      setSelected((prev) =>
        prev && items.some((item) => item.sourcePath === prev.sourcePath)
          ? prev
          : (items[0] ?? null),
      );
    } catch {
      /* 实例快照读取失败：保持现状 */
    } finally {
      setLoadingSaves(false);
    }
  }, []);

  useEffect(() => {
    void loadSaves();
    void loadSummary();
    const timer = window.setInterval(() => {
      void loadSaves();
      void loadSummary();
    }, SAVES_POLL_INTERVAL_MS);

    return () => window.clearInterval(timer);
  }, [loadSaves, loadSummary]);

  const loadSnapshots = useCallback(async (path: string, silent = false) => {
    if (!silent) setLoadingSnaps(true);
    try {
      setSnapshots(await ListSaveSnapshots(path));
    } catch (ex) {
      notify.error(t("读取快照失败：{0}", { "0": errorMessage(ex) }));
    } finally {
      setLoadingSnaps(false);
    }
  }, []);

  useEffect(() => {
    if (!selected) {
      setSnapshots([]);

      return;
    }
    void loadSnapshots(selected.sourcePath);
  }, [selected, loadSnapshots]);

  const create = async () => {
    if (!selected || busy) return;
    setBusy("create");
    try {
      await CreateSaveSnapshot(selected.sourcePath, "", "");
      notify.success(t("快照已创建：{0}", { "0": selected.name }));
      await loadSnapshots(selected.sourcePath, true);
      await loadSummary();
    } catch (ex) {
      notify.error(t("创建快照失败：{0}", { "0": errorMessage(ex) }));
    } finally {
      setBusy("");
    }
  };

  const rollback = async (snapshotId: string) => {
    if (!selected || busy) return;
    const ok = await confirm(
      t("回滚到此快照"),
      t(
        "将用该快照覆盖当前内容。回滚前会自动创建一个安全快照，可随时再回滚回来。",
      ),
      { confirmLabel: t("回滚"), severity: "warning" },
    );

    if (!ok) return;
    setBusy(snapshotId);
    try {
      await RollbackSaveSnapshot(selected.sourcePath, snapshotId);
      notify.success(t("已回滚到所选快照。"));
      await loadSnapshots(selected.sourcePath, true);
      await loadSummary();
    } catch (ex) {
      notify.error(t("回滚失败：{0}", { "0": errorMessage(ex) }));
    } finally {
      setBusy("");
    }
  };

  const remove = async (snapshotId: string) => {
    if (!selected || busy) return;
    const ok = await confirm(
      t("删除快照"),
      t("删除后该时间点的快照将不可用，已无引用的数据块会被回收。"),
      { confirmLabel: t("删除"), severity: "warning" },
    );

    if (!ok) return;
    setBusy(snapshotId);
    try {
      await DeleteSaveSnapshot(selected.sourcePath, snapshotId);
      notify.success(t("快照已删除。"));
      await loadSnapshots(selected.sourcePath, true);
      await loadSummary();
    } catch (ex) {
      notify.error(t("删除失败：{0}", { "0": errorMessage(ex) }));
    } finally {
      setBusy("");
    }
  };

  const count = summary?.SnapshotCount ?? 0;

  return (
    <HomeCard
      action={
        <Chip
          as="button"
          className="cursor-pointer"
          color="secondary"
          size="sm"
          variant="flat"
          onClick={() => navigateToPage("instances")}
        >
          {t("管理")}
        </Chip>
      }
      icon={<History20Regular />}
      label={t("时光回溯")}
      tileClass="from-emerald-400 via-teal-500 to-cyan-500 shadow-teal-500/30"
      value={
        loadingSaves && !summary
          ? t("读取中…")
          : t("{0} 个快照", { "0": String(count) })
      }
      valueClass="bg-gradient-to-r from-emerald-500 via-teal-500 to-cyan-500"
    >
      <div className="flex min-h-0 gap-2">
        {/* 左列：所有实例的存档聚合列表 */}
        <div className="nya-scroll flex w-36 flex-none flex-col overflow-y-auto rounded-xl border nya-border bg-default-100/40 p-1.5">
          <div className="px-1.5 pb-1 text-[10px] font-medium text-gray-400">
            {t("存档")}
          </div>
          {loadingSaves && saves.length === 0 ? (
            <div className="flex items-center justify-center py-4">
              <Spinner size="sm" />
            </div>
          ) : saves.length === 0 ? (
            <div className="px-1.5 py-2 text-[11px] leading-relaxed text-gray-400">
              {t("还没有存档：进游戏创建一个世界后，这里就能直接拍快照")}
            </div>
          ) : (
            saves.map((item) => {
              const active = selected?.sourcePath === item.sourcePath;

              return (
                <button
                  key={item.sourcePath}
                  className={`block w-full rounded-lg px-2 py-1.5 text-left transition-colors ${
                    active ? "bg-primary/15" : "hover:bg-default-200/60"
                  }`}
                  title={`${item.name} · ${item.instanceId}`}
                  type="button"
                  onClick={() => setSelected(item)}
                >
                  <span
                    className={`block truncate text-[12px] leading-tight ${
                      active
                        ? "font-semibold text-primary"
                        : "text-gray-700 dark:text-gray-200"
                    }`}
                  >
                    {item.name}
                  </span>
                  <span className="block truncate text-[10px] leading-tight text-gray-400">
                    {item.instanceId}
                  </span>
                </button>
              );
            })
          )}
        </div>

        {/* 右列：选中存档的全部快照 */}
        <div className="nya-scroll flex min-w-0 flex-1 flex-col gap-1 overflow-y-auto">
          <div className="flex items-center gap-2">
            <span className="min-w-0 flex-1 truncate text-[11px] text-gray-400">
              {selected ? selected.name : t("在左侧选择一个存档")}
            </span>
            <Button
              className="min-w-0 h-6 px-2"
              color="primary"
              isDisabled={!selected}
              isLoading={busy === "create"}
              size="sm"
              startContent={<AddIcon />}
              variant="flat"
              onPress={() => void create()}
            >
              {t("新建快照")}
            </Button>
          </div>
          {loadingSnaps && snapshots.length === 0 ? (
            <div className="flex items-center justify-center py-4">
              <Spinner size="sm" />
            </div>
          ) : !selected ? null : snapshots.length === 0 ? (
            <div className="px-1 py-3 text-center text-[11px] leading-relaxed text-gray-400">
              {t("该存档还没有快照")}
            </div>
          ) : (
            snapshots.map((snapshot) => {
              const isSafety = snapshot.Reason === "before-rollback";

              return (
                <div
                  key={snapshot.Id}
                  className="group flex items-center gap-2 rounded-lg px-2 py-1.5 transition-colors hover:bg-primary/10"
                >
                  <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-1.5">
                      <span className="text-[11px] font-semibold tabular-nums text-gray-600 dark:text-gray-300">
                        {formatClock(snapshot.CreatedAt)}
                      </span>
                      {isSafety ? (
                        <span className="rounded bg-warning/15 px-1 text-[9px] text-warning">
                          {t("安全快照")}
                        </span>
                      ) : snapshot.Label ? (
                        <span className="min-w-0 truncate text-[11px] font-medium text-gray-700 dark:text-gray-200">
                          {snapshot.Label}
                        </span>
                      ) : null}
                    </div>
                    <div className="truncate text-[10px] text-gray-400">
                      {t("{0} 个文件 · {1}", {
                        "0": snapshot.FileCount,
                        "1": formatBytes(snapshot.TotalSize),
                      })}
                    </div>
                  </div>
                  <Button
                    className="h-6 min-w-0 px-2"
                    isDisabled={busy === snapshot.Id}
                    isLoading={busy === snapshot.Id}
                    size="sm"
                    startContent={<RollbackIcon className="h-3.5 w-3.5" />}
                    variant="flat"
                    onPress={() => void rollback(snapshot.Id)}
                  >
                    {t("回滚")}
                  </Button>
                  <Button
                    isIconOnly
                    aria-label={t("删除快照")}
                    className="h-6 min-w-6 w-6"
                    color="danger"
                    isDisabled={busy === snapshot.Id}
                    size="sm"
                    variant="light"
                    onPress={() => void remove(snapshot.Id)}
                  >
                    <DeleteIcon className="h-3.5 w-3.5" />
                  </Button>
                </div>
              );
            })
          )}
        </div>
      </div>
    </HomeCard>
  );
};

export default RewindCard;
