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
import type { auth, launch, config } from "../../wailsjs/go/models";

import React, {
  useCallback,
  useMemo,
  useRef,
  useState,
  useEffect,
} from "react";
import {
  Button,
  Dropdown,
  DropdownItem,
  DropdownMenu,
  DropdownTrigger,
  Spinner,
} from "@heroui/react";
// 图标统一用 Fluent UI System Icons（20px 系）
import {
  Add20Regular,
  ChevronDown20Regular,
  Delete20Regular,
  Play20Regular,
  Stop20Regular,
  ArrowClockwise20Regular as RefreshIcon,
  Person20Regular,
  PersonAdd20Regular,
  Warning20Regular,
} from "@fluentui/react-icons";

import {
  GetInstalledVersionIds,
  EnsureDefaultMinecraftDirectory,
  SelectInstance,
} from "../../wailsjs/go/bindings/InstanceAPI";
import {
  GetGameDirectory,
  GetValue,
  SetValue,
} from "../../wailsjs/go/bindings/ConfigAPI";
import { OpenPath } from "../../wailsjs/go/bindings/SystemAPI";
import {
  GetAccounts,
  GetAccountStableKey,
  GetSelectedAccount,
  SelectAccountByStableKey,
  GetAvatarUrl,
} from "../../wailsjs/go/bindings/AccountAPI";
import {
  GetLaunchSnapshot,
  Launch,
  StopGame,
  GetPlaytimeStats,
} from "../../wailsjs/go/bindings/LauncherAPI";
import { EventsOn, EventsOff } from "../../wailsjs/runtime/runtime";
import {
  DEFAULT_WIDGET_IDS,
  useWidgets,
  widgetById,
  WidgetHost,
  type SelectedAccountSummary,
  type WidgetRenderContext,
} from "../plugin";
import WidgetLibrary from "../components/home/WidgetLibrary";
import LaunchVersionIcon from "../components/home/LaunchVersionIcon";
import ErrorBoundary from "../components/ErrorBoundary";
import {
  useWidgetDrag,
  type DragPayload,
  type DropTarget,
} from "../components/home/useWidgetDrag";
import { asArray } from "../lib/guards";
import { navigateToPage } from "../lib/navigation";
import {
  HOME_WIDGET_COLUMNS_KEY,
  HOME_WIDGET_LAYOUT_KEY,
  WIDGET_COLUMNS_EVENT,
  columnInsertionIndex,
  errorMessage,
  migrateWidgetIds,
  parseWidgetColumns,
  parseWidgetLayout,
  splitWidgetColumns,
} from "../lib/home";
import { notifyCrashIfNeeded } from "../lib/crashNotice";
import { t } from "../i18n";

import { useSimpleMode } from "./simple-mode";

/** 多列网格下每列的目标宽度（下标 = 列数；1 列沿用原有 384px） */
const WIDGET_COLUMN_WIDTHS = [0, 384, 680, 900];
/** 各列数下的宽度上限（vw，防止窄窗口挤压中间空白区） */
const WIDGET_COLUMN_MAX_VW = [0, 32, 56, 76];

interface AccountRow {
  account: auth.LaunchAccount;
  stableKey: string;
  avatar: string;
}

function typeLabel(account: auth.LaunchAccount): string {
  switch (account.Type) {
    case "microsoft":
      return t("正版");
    case "offline":
      return t("离线");
    case "authlib":
      return t("皮肤站");
    default:
      return t("第三方");
  }
}

// 删除区覆盖层：拖动页面组件时盖在启动页与组件库上，拖到哪边松手都是移除。
// 两个面板在同一位置滑动互换，拖动期间只有展开的那一边可见。
const DeleteZoneOverlay: React.FC<{ over: boolean }> = ({ over }) => (
  <div
    className={`
      pointer-events-none absolute inset-0 z-30 flex flex-col items-center
      justify-center gap-3 rounded-3xl transition-colors
      ${over ? "bg-danger/20 ring-2 ring-danger" : "bg-black/5 ring-1 ring-danger/30 dark:bg-white/5"}
    `}
  >
    <span
      className={`
        flex size-14 items-center justify-center rounded-2xl
        ${over ? "bg-danger text-white" : "bg-danger/10 text-danger"}
      `}
    >
      <Delete20Regular className="h-6 w-6" />
    </span>
    <span
      className={`
        px-6 text-center text-xs font-medium
        ${over ? "text-danger" : "text-gray-400"}
      `}
    >
      {over ? t("松开即移除该组件") : t("拖到这里移除组件")}
    </span>
  </div>
);

const HomePage: React.FC = () => {
  // S 模式禁用小组件功能：隐藏组件列 / 组件库 / 添加入口，主页变成居中的纯启动页
  const { simpleMode } = useSimpleMode();

  const [versions, setVersions] = useState<string[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [loadError, setLoadError] = useState<string>("");
  const [minecraftDirectory, setMinecraftDirectory] = useState<string>("");
  const [selectedVersion, setSelectedVersion] = useState<string>("");

  const [accounts, setAccounts] = useState<AccountRow[]>([]);
  const [selectedAccountKey, setSelectedAccountKey] = useState("");
  const [avatarMap, setAvatarMap] = useState<Record<string, string>>({});

  // 启动管线状态（launch:changed 快照驱动）
  const [snap, setSnap] = useState<launch.GameLaunchSnapshot | null>(null);
  const [launchError, setLaunchError] = useState("");
  const launchPhase = snap?.Phase ?? 0; // 0 idle / 1 preparing / 2 running / 3 failed / 4 exited
  const isPreparing = launchPhase === 1;
  const isRunning = launchPhase === 2;
  /** 启动准备中或游戏运行中：小组件禁止再次发起启动 */
  const isBusy = isPreparing || isRunning;

  // 游玩统计（游戏退出结算后会随启动阶段变化重新拉取）
  const [playtimeRecords, setPlaytimeRecords] = useState<
    config.PlaytimeRecord[]
  >([]);

  const loadVersions = async () => {
    setIsLoading(true);
    setLoadError("");
    try {
      let directory = await GetGameDirectory();

      if (!directory) {
        directory = await EnsureDefaultMinecraftDirectory();
      }
      if (!directory) {
        setVersions([]);
        setLoadError(t("无法确定Minecraft目录"));

        return;
      }
      setMinecraftDirectory(directory);
      const list = await GetInstalledVersionIds(directory);
      const versionList = list ?? [];

      setVersions(versionList);
      setSelectedVersion((prev) =>
        prev && versionList.includes(prev) ? prev : (versionList[0] ?? ""),
      );
    } catch (err) {
      console.error(err);
      setVersions([]);
      setLoadError(err instanceof Error ? err.message : String(err));
    } finally {
      setIsLoading(false);
    }
  };

  const loadAccounts = async () => {
    try {
      const list = asArray<auth.LaunchAccount>(await GetAccounts());
      const withKeys = await Promise.all(
        list.map(async (account) => ({
          account,
          stableKey: await GetAccountStableKey(account as never),
          avatar: "",
        })),
      );

      setAccounts(withKeys);
      // 启动使用后端存储的选中账号；没有则回落列表首项
      const selected = (await GetSelectedAccount().catch(
        () => null,
      )) as auth.LaunchAccount | null;
      let key = "";

      if (selected) key = await GetAccountStableKey(selected as never);
      if (!key && withKeys.length > 0) key = withKeys[0].stableKey;
      setSelectedAccountKey(key);
      // 异步加载皮肤头像（失败保留字母占位）
      withKeys.forEach(({ stableKey }) => {
        GetAvatarUrl(stableKey)
          .then((uri) => {
            if (uri)
              setAvatarMap((m) =>
                m[stableKey] === uri ? m : { ...m, [stableKey]: uri },
              );
          })
          .catch(() => {
            /* 字母占位 */
          });
      });
    } catch (err) {
      console.error(err);
    }
  };

  useEffect(() => {
    loadVersions();
    loadAccounts();
  }, []);

  // 头像加载完成后并入账户行
  useEffect(() => {
    setAccounts((prev) => {
      let changed = false;
      const next = prev.map((row) => {
        const uri = avatarMap[row.stableKey];

        if (uri && row.avatar !== uri) {
          changed = true;

          return { ...row, avatar: uri };
        }

        return row;
      });

      return changed ? next : prev;
    });
  }, [avatarMap]);

  // 版本切换 → 同步到后端实例存储（启动管线以选中的实例为准）
  useEffect(() => {
    if (!selectedVersion) return;
    SelectInstance(selectedVersion).catch((err) =>
      console.error(t("切换版本失败"), err),
    );
  }, [selectedVersion]);

  // 启动快照：初始拉取一次 + 订阅 launch:changed
  useEffect(() => {
    GetLaunchSnapshot()
      .then(setSnap)
      .catch(() => {
        /* 未注入 ctx 时忽略 */
      });
    EventsOn("launch:changed", (s: launch.GameLaunchSnapshot) => {
      setSnap(s);
      // 异常退出（非零退出码）时弹一次 NekoPrompt；正常退出/手动停止不弹
      notifyCrashIfNeeded(s);
    });

    return () => EventsOff("launch:changed");
  }, []);

  // 游玩统计：初始拉取一次，游戏运行/退出后重拉（退出时后端已结算本次时长）
  useEffect(() => {
    GetPlaytimeStats()
      .then((records) => setPlaytimeRecords(records ?? []))
      .catch(() => {
        /* 统计加载失败不阻塞主页 */
      });
  }, [launchPhase]);

  const pickAccount = async (stableKey: string) => {
    setSelectedAccountKey(stableKey);
    // 同步到后端：启动游戏时使用该账号
    try {
      await SelectAccountByStableKey(stableKey);
    } catch {
      /* 启动时会再次校验 */
    }
  };

  // 统一启动入口：主页按钮、最近存档、快速进服都走这里，
  // 返回错误文本（null = 已发起启动），便于小组件就地展示失败原因。
  const runLaunch = useCallback(
    async (
      serverHost = "",
      serverPort: number | null = null,
    ): Promise<string | null> => {
      setLaunchError("");
      try {
        // 阻塞至启动流程结束（进程创建成功，或失败返回原因）；期间状态由快照事件驱动
        const result = await Launch(serverHost, serverPort);

        if (!result?.Success) {
          const message = result?.Message || t("启动失败");

          setLaunchError(message);

          return message;
        }

        return null;
      } catch (ex) {
        const message = errorMessage(ex);

        setLaunchError(message);

        return message;
      }
    },
    [],
  );

  const handleLaunch = async () => {
    if (!selectedVersion) return;
    // 后端未记录选中账号时回落列表首项，按钮就不会出现「能点但必然失败」的状态
    if (!selectedAccountKey && accounts.length > 0) {
      await pickAccount(accounts[0].stableKey);
    }
    await runLaunch();
  };

  // 「最近存档」直接启动：先切到该存档所属实例，再走统一启动入口
  const launchWorld = useCallback(
    async (versionId: string): Promise<string | null> => {
      if (!versionId) return t("该存档没有关联的游戏实例");
      try {
        await SelectInstance(versionId);
      } catch (ex) {
        return errorMessage(ex);
      }
      setSelectedVersion(versionId);

      return runLaunch();
    },
    [runLaunch],
  );

  const handleStop = async () => {
    try {
      await StopGame();
    } catch {
      /* 进程已退出时忽略 */
    }
  };

  const openDirectory = async () => {
    if (!minecraftDirectory) return;
    try {
      await OpenPath(minecraftDirectory);
    } catch {
      /* 打开失败不打断启动页 */
    }
  };

  // ---- 小组件布局：顺序持久化在 launcher.yaml 的 homeWidgetLayout ----

  const [widgetIds, setWidgetIds] = useState<string[]>(DEFAULT_WIDGET_IDS);
  const [layoutLoaded, setLayoutLoaded] = useState(false);
  /** 小组件列数（外观设置可调 1~3，存 homeWidgetColumns） */
  const [widgetColumns, setWidgetColumns] = useState(1);
  const isGridWidgets = widgetColumns > 1;
  /** 组件库是否展开（展开时右侧启动页滑出） */
  const [isLibraryOpen, setIsLibraryOpen] = useState(false);
  const widgetIdsRef = useRef(widgetIds);

  widgetIdsRef.current = widgetIds;

  useEffect(() => {
    GetValue(HOME_WIDGET_LAYOUT_KEY)
      .then((raw) => {
        const saved = parseWidgetLayout(raw ?? "");

        // 未注册的 id（插件被禁用）原样保留：只在渲染时跳过，插件重新启用后
        // 组件回到原位，而不是被这里悄悄从布局里删掉。已下线的内置组件则
        // 迁移到替代组件（migrateWidgetIds），避免留下永远空着的占位。
        if (saved) setWidgetIds(migrateWidgetIds(saved));
      })
      .catch(() => {
        /* 读配置失败按默认布局显示 */
      })
      .finally(() => setLayoutLoaded(true));
  }, []);

  // 列数：初始读配置 + 订阅外观设置的实时变更广播
  useEffect(() => {
    GetValue(HOME_WIDGET_COLUMNS_KEY)
      .then((raw) => {
        const saved = parseWidgetColumns(raw ?? "");

        if (saved) setWidgetColumns(saved);
      })
      .catch(() => {
        /* 读配置失败按 1 列显示 */
      });
    const onColumnsChanged = (event: Event) => {
      const value = Math.round((event as CustomEvent<number>).detail);

      if (value >= 1 && value <= 3) setWidgetColumns(value);
    };

    window.addEventListener(WIDGET_COLUMNS_EVENT, onColumnsChanged);

    return () =>
      window.removeEventListener(WIDGET_COLUMNS_EVENT, onColumnsChanged);
  }, []);

  // 启动页重新滑入时刷新版本列表：在下载页 / 实例页装了新版本后回到主页即可见
  useEffect(() => {
    if (!isLibraryOpen) void loadVersions();
  }, [isLibraryOpen]);

  const persistLayout = useCallback((next: string[]) => {
    setWidgetIds(next);
    void SetValue(HOME_WIDGET_LAYOUT_KEY, JSON.stringify(next)).catch(
      () => undefined,
    );
  }, []);

  // 拖动落点 → 新增 / 重排 / 删除
  const handleWidgetDrop = useCallback(
    (payload: DragPayload, target: DropTarget | null) => {
      if (!target) return; // 原地放回：不改布局
      const prev = widgetIdsRef.current;

      if (target.kind === "delete") {
        if (payload.kind !== "list") return;
        persistLayout(prev.filter((id) => id !== payload.widgetId));

        return;
      }

      // 目标落点是「纵排 + 列内位置」：换算成平铺下标再插入
      if (payload.kind === "library") {
        if (prev.includes(payload.widgetId)) return;
        const slices = splitWidgetColumns(prev, widgetColumns);
        const at = columnInsertionIndex(slices, target.column, target.index);
        const next = [...prev];

        next.splice(at, 0, payload.widgetId);
        persistLayout(next);
        setIsLibraryOpen(false); // 拖出组件后组件库收回，启动页回来

        return;
      }

      const from = prev.indexOf(payload.widgetId);

      if (from < 0) return;
      // target.index 数的是"屏幕上仍在的卡片"（被拖的那张还挂着，只是半透明），
      // 而 at 是插入到"已移除源"的数组里：同列且目标在源下方时两者基准差 1，
      // 不修正会让卡片落到指示线下面一格。
      let targetIndex = target.index;
      const fullSlices = splitWidgetColumns(prev, widgetColumns);

      for (let column = 0; column < fullSlices.length; column += 1) {
        const local = fullSlices[column].indexOf(payload.widgetId);

        if (local < 0) continue;
        if (column === target.column && local < targetIndex) targetIndex -= 1;

        break;
      }

      const without = prev.filter((_, index) => index !== from);
      // 移除源之后重新切列，目标列内位置越界时收敛到列尾
      const slices = splitWidgetColumns(without, widgetColumns);
      const at = columnInsertionIndex(slices, target.column, targetIndex);

      if (at === from) return; // 位置没变
      without.splice(at, 0, payload.widgetId);
      persistLayout(without);
    },
    [persistLayout, widgetColumns],
  );

  // 组件库条目被点击（未拖出）：追加到列表末尾
  const addWidget = useCallback(
    (widgetId: string) => {
      const prev = widgetIdsRef.current;

      if (prev.includes(widgetId)) return;
      persistLayout([...prev, widgetId]);
      setIsLibraryOpen(false);
    },
    [persistLayout],
  );

  const widgetListRef = useRef<HTMLDivElement>(null);
  const launchPanelRef = useRef<HTMLDivElement>(null);
  const libraryPanelRef = useRef<HTMLElement>(null);
  const {
    session: dragSession,
    pressingId,
    ghostRef,
    listPressHandlers,
    libraryHandlers,
  } = useWidgetDrag({
    listRef: widgetListRef,
    deleteRef: launchPanelRef,
    libraryRef: libraryPanelRef,
    onDrop: handleWidgetDrop,
    onLibraryPick: addWidget,
  });

  // 平铺布局 → 多个独立纵排（每列各自渲染、独立滚动）
  const widgetColumnsData = useMemo(
    () => splitWidgetColumns(widgetIds, widgetColumns),
    [widgetIds, widgetColumns],
  );

  const isListDrag = dragSession?.payload.kind === "list";
  const draggedDefinition = dragSession
    ? widgetById(dragSession.payload.widgetId)
    : undefined;
  // 订阅注册表：插件加载完成后，它注册的小组件会出现在组件库里
  const widgets = useWidgets();
  const availableWidgets = useMemo(
    () => widgets.filter((widget) => !widgetIds.includes(widget.id)),
    [widgets, widgetIds],
  );
  // 已放置且确实可渲染的组件定义（不含插件被禁用后留下的空位）
  const placedWidgets = useMemo(
    () =>
      widgetIds
        .map((id) => widgetById(id))
        .filter((widget): widget is NonNullable<typeof widget> =>
          Boolean(widget),
        ),
    [widgetIds],
  );
  const placedCount = placedWidgets.length;

  // 交给各小组件的数据与回调（布局无关，组件本身不感知拖动）
  const selectedAccountRow = accounts.find(
    (row) => row.stableKey === selectedAccountKey,
  );
  // 账号摘要统一在这里构造（含头像），皮肤展示等组件直接消费
  const accountSummaries: SelectedAccountSummary[] = accounts.map((row) => ({
    key: row.stableKey,
    name: row.account.DisplayName ?? "",
    type: row.account.Type ?? "",
    avatar: row.avatar,
  }));
  const widgetContext: WidgetRenderContext = {
    playtimeRecords,
    isGameRunning: isRunning,
    isBusy,
    launchPhase,
    launchRevision: snap?.Revision ?? 0,
    selectedVersion,
    onSelectVersion: setSelectedVersion,
    onLaunchVersion: launchWorld,
    onJoin: runLaunch,
    selectedAccount: selectedAccountRow
      ? {
          key: selectedAccountRow.stableKey,
          name: selectedAccountRow.account.DisplayName ?? "",
          type: selectedAccountRow.account.Type ?? "",
          avatar: selectedAccountRow.avatar,
        }
      : null,
    accounts: accountSummaries,
    onSelectAccount: pickAccount,
  };

  // 背景图与模糊由全局 BackgroundLayer（layouts/index.tsx）提供，本页只画半透明前景
  return (
    <div className="relative h-full w-full">
      {/* 前景：左侧小组件列 + 中间空白区 + 右侧启动页/组件库。
          S 模式：小组件功能整体禁用，只渲染居中的启动面板 */}
      <div className="relative flex h-full w-full">
        {/* 小组件列：顺序来自 homeWidgetLayout，列数来自 homeWidgetColumns；
            多列时每列相互独立、各自滚动 */}
        {!simpleMode ? (
          <div
            ref={widgetListRef}
            className="pointer-events-none my-4 ml-1 flex h-[calc(100%-2rem)] min-w-0 shrink gap-3"
            style={{
              width: WIDGET_COLUMN_WIDTHS[widgetColumns] ?? 384,
              maxWidth: `${WIDGET_COLUMN_MAX_VW[widgetColumns] ?? 32}vw`,
            }}
          >
            {widgetColumnsData.map((columnIds, columnIndex) => {
              // 该列第一个组件在平铺数组里的下标（data-widget-index 用）
              const columnOffset = widgetColumnsData
                .slice(0, columnIndex)
                .reduce((sum, column) => sum + column.length, 0);

              return (
                <div
                  key={columnIndex}
                  className="
                  pointer-events-none flex min-h-0 min-w-0 flex-1 flex-col
                  gap-3 overflow-y-auto px-3
                "
                  data-widget-column={columnIndex}
                >
                  {/* 单列时内容不足把卡片压到底部（多列各列从顶部排布） */}
                  {!isGridWidgets ? <div className="min-h-0 flex-1" /> : null}

                  {columnIndex === 0 && layoutLoaded && placedCount === 0 ? (
                    <div className="nya-enter rounded-3xl border border-dashed border-gray-300/80 px-5 py-8 text-center text-xs leading-relaxed text-gray-400 dark:border-gray-700">
                      {t("组件都被移除了")}
                    </div>
                  ) : null}

                  {columnIds.map((widgetId, localIndex) => {
                    const definition = widgetById(widgetId);

                    if (!definition) return null;
                    const isDragged =
                      dragSession?.payload.kind === "list" &&
                      dragSession.payload.widgetId === widgetId;
                    const flatIndex = columnOffset + localIndex;

                    return (
                      <React.Fragment key={widgetId}>
                        {/* 插入位置指示线：插到这个组件之前 */}
                        {dragSession?.targetColumn === columnIndex &&
                        dragSession.targetIndex === localIndex ? (
                          <div className="nya-drop-line flex-none" />
                        ) : null}
                        <div
                          data-widget-index={flatIndex}
                          {...listPressHandlers(widgetId, flatIndex)}
                          className={`
                          relative flex-none select-none
                          ${dragSession ? "pointer-events-none" : ""}
                          ${isDragged ? "opacity-40" : ""}
                          ${pressingId === widgetId ? "scale-[0.98] transition-transform" : ""}
                        `}
                          style={{ touchAction: "none" }}
                          title={
                            dragSession ? undefined : t("长按 1 秒可拖动排序")
                          }
                        >
                          {/* 单个小组件出错只丢它自己，不带走整页（插件组件尤其需要这层） */}
                          <ErrorBoundary title={definition.title}>
                            <WidgetHost
                              context={widgetContext}
                              definition={definition}
                            />
                          </ErrorBoundary>
                          {/* 长按计时进度：1 秒填满即进入拖动 */}
                          {pressingId === widgetId ? (
                            <div className="pointer-events-none absolute inset-x-5 top-1 h-0.5 overflow-hidden rounded-full bg-black/10 dark:bg-white/10">
                              <div className="nya-hold-bar h-full rounded-full bg-primary" />
                            </div>
                          ) : null}
                        </div>
                      </React.Fragment>
                    );
                  })}

                  {/* 拖到该列末尾 */}
                  {dragSession?.targetColumn === columnIndex &&
                  dragSession.targetIndex === columnIds.length ? (
                    <div className="nya-drop-line flex-none" />
                  ) : null}
                </div>
              );
            })}
          </div>
        ) : null}

        {/* 中间空白区：右下角圆形 + 按钮（拖动中隐藏）；S 模式无小组件功能，不渲染 */}
        {!simpleMode ? (
          <div className="relative min-w-0 flex-1">
            {!isLibraryOpen && !dragSession ? (
              <button
                aria-label={t("打开组件盒")}
                className="
                absolute right-6 bottom-8 flex size-12 cursor-pointer items-center
                justify-center rounded-full bg-primary
                text-primary-foreground shadow-lg shadow-primary/40 transition-transform
                hover:scale-105 active:scale-95
              "
                title={t("打开组件盒")}
                type="button"
                onClick={() => setIsLibraryOpen(true)}
              >
                <Add20Regular />
              </button>
            ) : null}
          </div>
        ) : null}

        {/* 右侧区域：启动页 ↔ 组件库，横向滑动互换。
            S 模式没有组件库，启动面板改为主区域居中 */}
        <div
          className={`relative my-4 h-[calc(100%-2rem)] w-[35%] max-w-[480px] min-w-[280px] ${
            simpleMode ? "mx-auto" : "mr-4"
          }`}
        >
          <aside
            ref={launchPanelRef}
            className={`
              absolute inset-0 flex flex-col overflow-hidden rounded-3xl
              border nya-border backdrop-blur-md shadow-lg
              transition-transform duration-300 ease-out nya-panel
              ${isLibraryOpen ? "translate-x-[115%]" : "translate-x-0"}
            `}
          >
            {/* 主视觉：实例图标 + 版本名 + 账号，居中填充整块高度（避免上下留白）。
                min-h-32 保证高度不足时先压缩此处，而不是把底部状态条挤掉。 */}
            <div className="relative flex min-h-32 flex-1 flex-col items-center justify-center gap-3.5 p-4">
              <div className="flex size-24 min-h-14 min-w-14 flex-none items-center justify-center rounded-[28px] border nya-border bg-gradient-to-br from-white/10 to-white/[0.03] shadow-lg">
                <LaunchVersionIcon versionId={selectedVersion} />
              </div>

              {isLoading ? (
                <div className="flex h-7 items-center gap-2 text-[13px] text-gray-400">
                  <Spinner size="sm" />
                  {t("读取中…")}
                </div>
              ) : (
                <Dropdown placement="bottom-start">
                  <DropdownTrigger>
                    <button
                      className="
                        flex max-w-full cursor-pointer flex-col items-center
                        rounded-xl px-3 py-1 transition-colors hover:bg-default-100/60
                      "
                      title={t("选择版本")}
                      type="button"
                    >
                      <span className="max-w-[240px] truncate text-[20px] leading-tight font-semibold text-gray-900 dark:text-gray-100">
                        {selectedVersion || t("未找到已安装版本")}
                      </span>
                      <span className="flex items-center gap-1 text-[11px] text-gray-400">
                        {t("切换版本")}
                        <ChevronDown20Regular className="h-3 w-3" />
                      </span>
                    </button>
                  </DropdownTrigger>
                  <DropdownMenu
                    aria-label={t("游戏版本")}
                    className="max-h-72 overflow-y-auto"
                    items={versions.map((v) => ({ value: v }))}
                    selectedKeys={
                      selectedVersion ? new Set([selectedVersion]) : new Set()
                    }
                    selectionMode="single"
                    variant="flat"
                    onAction={(key) => setSelectedVersion(String(key))}
                  >
                    {(item) => (
                      <DropdownItem key={item.value} textValue={item.value}>
                        <span className="text-[13px]">{item.value}</span>
                      </DropdownItem>
                    )}
                  </DropdownMenu>
                </Dropdown>
              )}

              <Dropdown placement="bottom">
                <DropdownTrigger>
                  <button
                    aria-label={t("选择账号")}
                    className="
                      flex max-w-full cursor-pointer items-center gap-1.5 rounded-full
                      border nya-border bg-default-100/60 py-1 pr-3 pl-1
                      transition-colors hover:bg-default-200/70
                    "
                    type="button"
                  >
                    <span className="flex size-7 flex-none items-center justify-center overflow-hidden rounded-full bg-default-200 dark:bg-gray-800">
                      {selectedAccountRow?.avatar ? (
                        <img
                          alt=""
                          className="h-full w-full object-cover [image-rendering:pixelated]"
                          src={selectedAccountRow.avatar}
                        />
                      ) : selectedAccountRow ? (
                        <span className="text-[11px] font-bold text-gray-500 dark:text-gray-300">
                          {(selectedAccountRow.account.DisplayName || "?")
                            .trim()[0]
                            ?.toUpperCase() || "?"}
                        </span>
                      ) : (
                        <Person20Regular className="h-4 w-4 text-gray-400" />
                      )}
                    </span>
                    <span className="max-w-[150px] truncate text-[12px] font-medium text-gray-900 dark:text-gray-100">
                      {selectedAccountRow
                        ? selectedAccountRow.account.DisplayName
                        : accounts.length > 0
                          ? t("选择账号")
                          : t("添加账号")}
                    </span>
                    {selectedAccountRow ? (
                      <span className="flex-none text-[10px] text-gray-400">
                        {typeLabel(selectedAccountRow.account)}
                      </span>
                    ) : null}
                  </button>
                </DropdownTrigger>
                <DropdownMenu
                  aria-label={t("账号列表")}
                  items={[
                    ...accounts.map((row) => ({
                      kind: "account" as const,
                      row,
                    })),
                    { kind: "add" as const },
                  ]}
                  selectedKeys={
                    selectedAccountKey
                      ? new Set([selectedAccountKey])
                      : new Set()
                  }
                  selectionMode="single"
                  variant="flat"
                  onAction={(key) => {
                    const id = String(key);

                    if (id === "__add_account__") {
                      navigateToPage("account");

                      return;
                    }

                    void pickAccount(id);
                  }}
                >
                  {(item) => {
                    if (item.kind === "add") {
                      return (
                        <DropdownItem
                          key="__add_account__"
                          showDivider
                          color="primary"
                          startContent={
                            <PersonAdd20Regular className="h-4 w-4" />
                          }
                        >
                          {t("添加账号…")}
                        </DropdownItem>
                      );
                    }

                    const { row } = item;

                    return (
                      <DropdownItem
                        key={row.stableKey}
                        description={typeLabel(row.account)}
                        startContent={
                          <span className="flex size-7 items-center justify-center overflow-hidden rounded-lg bg-default-200 dark:bg-gray-800">
                            {row.avatar ? (
                              <img
                                alt=""
                                className="h-full w-full object-cover [image-rendering:pixelated]"
                                src={row.avatar}
                              />
                            ) : (
                              <span className="text-[10px] font-bold text-gray-500 dark:text-gray-300">
                                {(row.account.DisplayName || "?")
                                  .trim()[0]
                                  ?.toUpperCase() || "?"}
                              </span>
                            )}
                          </span>
                        }
                        textValue={row.account.DisplayName ?? ""}
                      >
                        <span className="text-[12px]">
                          {row.account.DisplayName}
                        </span>
                      </DropdownItem>
                    );
                  }}
                </DropdownMenu>
              </Dropdown>
            </div>

            {/* 出错或一个版本都没有时，给出路径并可直接打开（排障入口） */}
            {loadError ||
            (!isLoading && versions.length === 0 && minecraftDirectory) ? (
              <button
                className={`
                  flex flex-none cursor-pointer items-start gap-1.5 px-4 pb-1
                  text-left text-[10px] leading-tight
                  ${loadError ? "text-danger" : "text-gray-400 hover:text-primary"}
                `}
                title={minecraftDirectory || undefined}
                type="button"
                onClick={() => void openDirectory()}
              >
                {loadError ? (
                  <span className="mt-px flex-none">
                    <Warning20Regular className="h-3.5 w-3.5" />
                  </span>
                ) : null}
                <span className="break-all">
                  {loadError || minecraftDirectory}
                </span>
              </button>
            ) : null}

            {/* 底部：状态提示 + 启动/停止按钮（按钮内联失败原因） */}
            <div className="flex-none border-t nya-border px-3 pt-2.5 pb-3">
              {isRunning ? (
                <div className="mb-2 flex items-center gap-1.5 rounded-lg bg-success/10 px-2.5 py-1.5">
                  <span className="size-1.5 flex-none animate-pulse rounded-full bg-success" />
                  <span className="min-w-0 flex-1 truncate text-[11px] leading-5 font-medium text-success-600 dark:text-success-400">
                    {snap?.Message || t("游戏运行中")}
                  </span>
                </div>
              ) : isPreparing ? (
                <div className="mb-2 flex items-center gap-2 rounded-lg bg-primary/10 px-2.5 py-1.5">
                  <Spinner className="flex-none text-primary" size="sm" />
                  <span className="min-w-0 flex-1 truncate text-[11px] leading-5 font-medium text-primary">
                    {snap?.Message || snap?.Title || t("正在启动…")}
                  </span>
                </div>
              ) : null}

              <div className="flex items-center gap-2">
                {isRunning ? (
                  <Button
                    fullWidth
                    className="h-12 text-[14px] font-semibold"
                    color="danger"
                    radius="lg"
                    size="lg"
                    startContent={<Stop20Regular className="h-4 w-4" />}
                    variant="flat"
                    onPress={handleStop}
                  >
                    {t("停止游戏")}
                  </Button>
                ) : (
                  <>
                    <div className="flex min-w-0 flex-1 flex-col gap-1">
                      <Button
                        fullWidth
                        className="h-12 text-[14px] font-semibold shadow-lg shadow-primary/30"
                        color="primary"
                        isDisabled={isPreparing || !selectedVersion}
                        isLoading={isPreparing}
                        radius="lg"
                        size="lg"
                        startContent={
                          !isPreparing ? (
                            <Play20Regular className="h-5 w-5" />
                          ) : undefined
                        }
                        onPress={handleLaunch}
                      >
                        {isPreparing ? t("正在启动…") : t("启动游戏")}
                      </Button>
                      {/* 失败原因就地显示在按钮下方，不再另起一块提示条 */}
                      {!isPreparing && (launchError || launchPhase === 3) ? (
                        <span className="truncate text-[10px] leading-4 text-danger">
                          {launchError || snap?.Message}
                        </span>
                      ) : null}
                    </div>
                    <Button
                      isIconOnly
                      aria-label={t("刷新版本列表")}
                      className="size-12 min-w-12 flex-none border nya-border text-gray-400"
                      isLoading={isLoading}
                      radius="lg"
                      title={t("刷新版本列表")}
                      variant="bordered"
                      onPress={loadVersions}
                    >
                      {isLoading ? undefined : (
                        <RefreshIcon className="h-4 w-4" />
                      )}
                    </Button>
                  </>
                )}
              </div>
            </div>

            {/* 拖动页面组件时：启动页整体作为删除区 */}
            {isListDrag ? (
              <DeleteZoneOverlay over={dragSession?.overDeleteZone === true} />
            ) : null}
          </aside>

          {/* 组件库：与启动页同一位置滑动互换；S 模式禁用小组件功能，不渲染 */}
          {!simpleMode ? (
            <section
              ref={libraryPanelRef}
              className={`
              absolute inset-0 transition-transform duration-300 ease-out
              ${isLibraryOpen ? "translate-x-0" : "translate-x-[115%]"}
            `}
            >
              <WidgetLibrary
                available={availableWidgets}
                draggingId={
                  dragSession?.payload.kind === "library"
                    ? dragSession.payload.widgetId
                    : null
                }
                libraryHandlers={libraryHandlers}
                placed={placedWidgets}
                onClose={() => setIsLibraryOpen(false)}
              />
              {/* 拖动页面组件时：拖回组件库同样移除 */}
              {isListDrag ? (
                <DeleteZoneOverlay
                  over={dragSession?.overDeleteZone === true}
                />
              ) : null}
            </section>
          ) : null}
        </div>
      </div>

      {dragSession && draggedDefinition ? (
        <div
          ref={ghostRef}
          className="nya-drag-ghost pointer-events-none fixed top-0 left-0 z-50"
        >
          <div
            className={`
              flex items-center gap-2.5 rounded-2xl border px-3 py-2 shadow-xl
              backdrop-blur-md
              ${
                dragSession.overDeleteZone
                  ? "border-danger/60 bg-danger/15"
                  : "border nya-border nya-panel-strong"
              }
            `}
          >
            <span
              className={`
                flex size-8 flex-none items-center justify-center rounded-xl
                bg-primary/20 text-primary shadow-sm
              `}
            >
              {draggedDefinition.icon}
            </span>
            <span className="flex flex-col">
              <span className="text-xs font-semibold whitespace-nowrap">
                {draggedDefinition.title}
              </span>
              <span
                className={`
                  text-[10px] whitespace-nowrap
                  ${dragSession.overDeleteZone ? "text-danger" : "text-gray-400"}
                `}
              >
                {dragSession.overDeleteZone
                  ? t("松开以移除")
                  : dragSession.payload.kind === "library"
                    ? t("拖到左侧放置")
                    : t("拖到位置或右侧移除")}
              </span>
            </span>
          </div>
        </div>
      ) : null}
    </div>
  );
};

export default HomePage;
