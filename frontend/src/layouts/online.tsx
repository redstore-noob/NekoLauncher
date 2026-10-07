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
 * 联机页：把两家联机供应商（陶瓦联机 / 红石联机）收敛成同一套交互。
 *
 * 页面不做任何协议处理——建房 / 加入 / 退出都是后端会话（internal/online），
 * 状态由 online:changed 事件推送（后端内部轮询供应商的状态机），前端只负责
 * 渲染与把用户动作转成一次调用。两家的形态差异（虚拟局域网 vs 公网中继）
 * 体现在供应商说明、设置项与提示文案上。
 *
 * 左列 = 当前会话与建房/加入操作，右列 = 供应商设置（含本机后台服务状态）。
 */
import type { ReactNode } from "react";
import type { online } from "../../wailsjs/go/models";

type ProviderInfo = online.ProviderInfo;
type SessionStatus = online.Status;
type OnlineSettings = online.Settings;
type ProviderRuntime = online.Runtime;
type GameServerOption = online.LocalServer;
type RelayNode = online.RelayOption;

import React, { useCallback, useEffect, useMemo, useState } from "react";
import {
  Button,
  Chip,
  Input,
  Select,
  SelectItem,
  Spinner,
  Textarea,
} from "@heroui/react";
import {
  Add20Regular,
  ArrowExit20Regular,
  ArrowSync20Regular,
  ArrowDownload20Regular,
  CheckmarkCircle20Regular,
  ChevronRight20Regular,
  Copy20Regular,
  Dismiss20Regular,
  Flash20Regular,
  FolderOpen20Regular,
  Globe20Regular,
  Info20Regular,
  Link20Regular,
  Open20Regular,
  People20Regular,
  Play20Regular,
  Rocket20Regular,
  Server20Regular,
  Warning20Regular,
} from "@fluentui/react-icons";
import { motion } from "framer-motion";

import { selectPopoverProps } from "../lib/motion";
import SegmentedTabs from "../components/segmented-tabs";
import { confirm, notify } from "../components/overlay/dialog";
import { Launch } from "../../wailsjs/go/bindings/LauncherAPI";
import {
  InstallTerracotta,
  GetRuntime,
  GetSettings,
  GetRelayList,
  GenerateAPIKey,
  ListStatuses,
  Host,
  Join,
  Leave,
  ListLocalServers,
  ListProviders,
  ListRelayOptions,
  OpenPage,
  ProbeRelays,
  SaveRelayList,
  SaveSettings,
  ShutdownProvider,
  UseFastestRelay,
} from "../../wailsjs/go/bindings/OnlineAPI";
import { SelectFile } from "../../wailsjs/go/bindings/SystemAPI";
import { EventsOn } from "../../wailsjs/runtime/runtime";
import { t } from "../i18n";
import { errorMessage } from "../lib/home";
import { isWindowsPlatform } from "../lib/platform";

/** 默认中继提示（与后端 online.DefaultRelayAddress 保持一致）。 */
const DEFAULT_RELAY_HINT = "122.51.108.96";
/** 设置读回前的兜底值（与后端 online 包的默认值保持一致）。 */
const FALLBACK_SETTINGS: OnlineSettings = {
  Provider: "terracotta",
  Player: "",
  TerracottaPath: "",
  RedstoneRelay: DEFAULT_RELAY_HINT,
  RedstoneKey: "",
  HasRedstoneKey: false,
  Target: "127.0.0.1:25565",
  ServerID: "",
  MaxPlayers: 8,
};

/** 会话状态 → 中文文案与配色（Chip 的 color 取值见 HeroUI）。 */
function stateChip(status: SessionStatus | null): ReactNode {
  const state = status?.State ?? "idle";

  if (state === "hosting") {
    return (
      <Chip color="success" size="sm" variant="flat">
        ● {t("房间已就绪")}
      </Chip>
    );
  }
  if (state === "joined") {
    return (
      <Chip color="success" size="sm" variant="flat">
        ● {t("已连接")}
      </Chip>
    );
  }
  if (state === "starting") {
    return (
      <Chip color="warning" size="sm" variant="flat">
        {t("准备中")}
      </Chip>
    );
  }
  if (state === "error") {
    return (
      <Chip color="danger" size="sm" variant="flat">
        {t("出错")}
      </Chip>
    );
  }

  return (
    <Chip size="sm" variant="flat">
      {t("空闲")}
    </Chip>
  );
}

/** 复制到剪贴板（含降级方案，WebView 上 clipboard API 偶尔不可用）。 */
async function copyText(value: string): Promise<boolean> {
  try {
    await navigator.clipboard.writeText(value);

    return true;
  } catch {
    try {
      const area = document.createElement("textarea");

      area.value = value;
      area.style.position = "fixed";
      area.style.opacity = "0";
      document.body.appendChild(area);
      area.select();
      const ok = document.execCommand("copy");

      area.remove();

      return ok;
    } catch {
      return false;
    }
  }
}

/** 窄标签 + 值的一行设置项。 */
const SettingRow: React.FC<{
  label: string;
  hint?: string;
  children: ReactNode;
}> = ({ label, hint, children }) => (
  <div className="flex flex-col gap-1.5">
    <span className="text-[12px] font-medium text-gray-600 dark:text-gray-300">
      {t(label)}
    </span>
    {children}
    {hint ? (
      <span className="text-[10px] leading-tight text-gray-400 dark:text-gray-500">
        {t(hint)}
      </span>
    ) : null}
  </div>
);

const OnlinePage: React.FC = () => {
  const [providers, setProviders] = useState<ProviderInfo[]>([]);
  const [providerId, setProviderId] = useState("terracotta");
  const [statusMap, setStatusMap] = useState<Record<string, SessionStatus>>({});
  // 设置永远有值：读回前用与后端一致的默认值，避免表单/按钮出现"点了没反应"
  const [settings, setSettings] = useState<OnlineSettings>(FALLBACK_SETTINGS);
  const [runtime, setRuntime] = useState<ProviderRuntime | null>(null);
  const [servers, setServers] = useState<GameServerOption[]>([]);
  const [busy, setBusy] = useState(false);
  const [launching, setLaunching] = useState(false);

  // 建房表单
  const [joinInput, setJoinInput] = useState("");
  // 红石联机的转发目标：启动器托管的服务器 / 本机端口（固定转发到 127.0.0.1）
  const [targetMode, setTargetMode] = useState<"server" | "manual">("server");
  const [targetServerId, setTargetServerId] = useState("");
  const [manualPort, setManualPort] = useState("25565");

  // 设置面板
  const [settingsDirty, setSettingsDirty] = useState(false);
  const [saving, setSaving] = useState(false);
  const [installing, setInstalling] = useState(false);

  // 红石联机的中继节点：候选列表（测速结果由"一键选最快"的提示直接给出）
  const [relayNodes, setRelayNodes] = useState<RelayNode[]>([]);
  const [probing, setProbing] = useState(false);
  // 自定义节点列表（每行一条：名称=地址），与后端 online.redstoneRelayList 对应
  const [relayListText, setRelayListText] = useState("");
  const [savingRelayList, setSavingRelayList] = useState(false);
  const [probingNodes, setProbingNodes] = useState(false);
  // 首帧加载失败汇总：之前全部被 .catch(() => undefined) 吞掉，用户以为
  // 会话没在跑 / 在拿默认值编辑设置，现在集中成黄条提示并提供一键重试
  const [initProblems, setInitProblems] = useState<string[]>([]);
  // 高级设置默认折叠：中继节点 / 并发上限都是一次性配置；API Key、节点预检
  // 与自动换线已封装进后端建房流程，界面不再暴露
  const [showAdvanced, setShowAdvanced] = useState(false);
  // 设置抽屉开关：顶栏「联机设置」按钮切换，专注会话时可以整列收起
  const [settingsOpen, setSettingsOpen] = useState(true);

  const provider = useMemo(
    () => providers.find((item) => item.ID === providerId) ?? null,
    [providers, providerId],
  );
  const isRedstone = providerId === "redstone";

  // 后端按供应商各持一个会话：状态按 Provider 归位，两个标签页互不干扰
  const statusForCurrent = statusMap[providerId] ?? null;
  const currentState = statusForCurrent?.State ?? "idle";
  const sessionLive =
    currentState === "hosting" ||
    currentState === "joined" ||
    currentState === "starting";
  const otherProviderId = providerId === "redstone" ? "terracotta" : "redstone";
  const otherStatus = statusMap[otherProviderId] ?? null;
  // 另一家也开着会话：只是告知（两家可以同时开），不再要求用户先退出
  const otherProviderLive =
    !!otherStatus &&
    otherStatus.State !== "idle" &&
    otherStatus.State !== "error";

  const patchSettings = useCallback((patch: Partial<OnlineSettings>) => {
    setSettings((prev) => ({ ...prev, ...patch }));
    setSettingsDirty(true);
  }, []);

  // 三个 refresh 都返回是否成功：首帧加载据此汇总失败提示（其它调用方忽略返回值无害）
  const refreshRuntime = useCallback(async (id: string) => {
    try {
      setRuntime(await GetRuntime(id));

      return true;
    } catch {
      setRuntime(null);

      return false;
    }
  }, []);

  const refreshServers = useCallback(async () => {
    try {
      const list = await ListLocalServers();

      setServers(Array.isArray(list) ? list : []);

      return true;
    } catch {
      setServers([]);

      return false;
    }
  }, []);

  /** 拉取可选节点（内置 + 已保存的自定义节点）。 */
  const refreshRelayNodes = useCallback(async () => {
    try {
      const list = await ListRelayOptions();

      setRelayNodes(Array.isArray(list) ? list : []);

      return true;
    } catch {
      setRelayNodes([]);

      return false;
    }
  }, []);

  /** 一键测速并挑最快的节点写进设置（预检结果由后端返回）。 */
  const pickFastestRelay = useCallback(async () => {
    setProbing(true);
    try {
      const probe = await UseFastestRelay();

      if (probe?.Address) {
        // 后端已经把节点写进设置；这里同步表单（其它字段的未保存修改仍然算脏）
        patchSettings({ RedstoneRelay: probe.Address });
        notify.success(
          t("已选用最快节点 {0}（{1} ms）", {
            "0": probe.Address,
            "1": String(probe.LatencyMs),
          }),
        );
        await refreshRelayNodes();
      }
    } catch (ex) {
      notify.error(t("自动选节点失败：{0}", { "0": errorMessage(ex) }));
    } finally {
      setProbing(false);
    }
  }, [patchSettings, refreshRelayNodes]);

  /** 只测速不改设置：把可达节点数与最快延迟报告给用户。 */
  const probeRelayNodes = useCallback(async () => {
    setProbingNodes(true);
    try {
      const probes = await ProbeRelays();
      const reachable = (Array.isArray(probes) ? probes : []).filter(
        (item) => item?.Reachable,
      );

      if (reachable.length === 0) {
        notify.warning(t("所有节点都连不上，请检查网络或填写自己的节点"));

        return;
      }
      const fastest = reachable.reduce((left, right) =>
        (right.LatencyMs || 0) < (left.LatencyMs || 0) ? right : left,
      );

      notify.success(
        t("共 {0} 个节点可用，最快 {1} ms", {
          "0": String(reachable.length),
          "1": String(fastest.LatencyMs || 0),
        }),
      );
    } catch (ex) {
      notify.error(t("测速失败：{0}", { "0": errorMessage(ex) }));
    } finally {
      setProbingNodes(false);
    }
  }, []);

  /** 保存自定义节点列表（每行一条：名称=地址），保存后刷新候选下拉。 */
  const saveRelayList = useCallback(async () => {
    setSavingRelayList(true);
    try {
      await SaveRelayList(relayListText);
      notify.success(t("节点列表已保存"));
      await refreshRelayNodes();
    } catch (ex) {
      notify.error(t("节点列表保存失败：{0}", { "0": errorMessage(ex) }));
    } finally {
      setSavingRelayList(false);
    }
  }, [relayListText, refreshRelayNodes]);

  /** 生成一个新的 API Key 写进表单（保存后下次建房生效）。 */
  const regenerateAPIKey = useCallback(async () => {
    try {
      const key = await GenerateAPIKey();

      if (key) {
        patchSettings({ RedstoneKey: key });
        notify.success(t("已生成新的 API Key，保存后生效"));
      }
    } catch (ex) {
      notify.error(errorMessage(ex));
    }
  }, [patchSettings]);

  // 首帧：供应商 / 设置 / 状态 / 本机服务器 / 中继节点。
  // 失败不再 .catch(() => undefined) 静默吞掉，而是汇总进黄条 + 一键重试。
  // 运行时状态不在这里拉：切供应商的 effect 挂载时就会按 providerId 加载，
  // 之前两边各调一次 refreshRuntime("terracotta") 是重复请求。
  const loadInitial = useCallback(async () => {
    const problems: string[] = [];

    await Promise.all([
      ListProviders()
        .then((list) => setProviders(Array.isArray(list) ? list : []))
        .catch(() => {
          setProviders([]);
          problems.push(t("供应商列表加载失败"));
        }),
      GetSettings()
        .then((value) => {
          if (!value) return;
          setSettings(value);
          setProviderId(value.Provider || "terracotta");
          // 兼容旧配置（完整的 host:port）：只取端口部分
          const target = value.Target ?? "";
          const port = target.includes(":")
            ? target.split(":").pop() || ""
            : target;

          setManualPort(/^\d{1,5}$/.test(port) ? port : "25565");
          if (value.ServerID) setTargetServerId(value.ServerID);
        })
        .catch(() => problems.push(t("联机设置未加载，表单显示的是默认值"))),
      ListStatuses()
        .then((list) => {
          const map: Record<string, SessionStatus> = {};

          (Array.isArray(list) ? list : []).forEach((item) => {
            if (item?.Provider) map[item.Provider] = item;
          });
          setStatusMap(map);
        })
        .catch(() => problems.push(t("会话状态未加载"))),
      refreshServers().then((ok) => {
        if (!ok) problems.push(t("本机服务器列表加载失败"));
      }),
      refreshRelayNodes().then((ok) => {
        if (!ok) problems.push(t("中继节点列表加载失败"));
      }),
      GetRelayList()
        .then((value) => setRelayListText(value ?? ""))
        .catch(() => problems.push(t("自定义节点列表未加载"))),
    ]);
    setInitProblems(problems);
  }, [refreshServers, refreshRelayNodes]);

  useEffect(() => {
    void loadInitial();
  }, [loadInitial]);

  // 会话状态由后端推送，前端不轮询（按 Provider 归位，两家的状态各自更新）
  useEffect(() => {
    const off = EventsOn("online:changed", (payload: SessionStatus) => {
      if (!payload?.Provider) return;
      setStatusMap((prev) => ({ ...prev, [payload.Provider]: payload }));
    });

    return () => {
      if (typeof off === "function") off();
    };
  }, []);

  // 切供应商时刷新本机运行时状态（陶瓦是否有进程、红石是否有隧道）
  useEffect(() => {
    void refreshRuntime(providerId);
  }, [providerId, refreshRuntime]);

  /** 一键进服：用启动器当前实例直接连到会话地址。 */
  const joinGame = async () => {
    if (!statusForCurrent?.JoinHost) return;
    setLaunching(true);
    try {
      const result = await Launch(
        statusForCurrent.JoinHost,
        statusForCurrent.JoinPort || null,
        "",
      );

      if (!result?.Success) {
        notify.error(result?.Message || t("启动失败"));
      } else {
        notify.success(t("已启动游戏并连接服务器"));
      }
    } catch (ex) {
      notify.error(t("启动失败：{0}", { "0": errorMessage(ex) }));
    } finally {
      setLaunching(false);
    }
  };

  const doHost = async () => {
    if (isRedstone) {
      if (targetMode === "server" && !targetServerId) {
        notify.warning(t("请先选择要转发的本机服务器"));

        return;
      }
      if (targetMode === "manual") {
        const port = Number(manualPort);

        if (!Number.isInteger(port) || port < 1 || port > 65535) {
          notify.warning(t("请先填写要转发的本机端口（1-65535）"));

          return;
        }
      }
    }

    setBusy(true);
    try {
      await Host({
        Provider: providerId,
        Player: settings.Player ?? "",
        Target:
          isRedstone && targetMode === "manual"
            ? `127.0.0.1:${Number(manualPort)}`
            : "",
        ServerID: isRedstone && targetMode === "server" ? targetServerId : "",
        MaxPlayers: settings.MaxPlayers || 0,
      });
    } catch (ex) {
      notify.error(t("创建房间失败：{0}", { "0": errorMessage(ex) }));
    } finally {
      setBusy(false);
      void refreshRuntime(providerId);
    }
  };

  const doJoin = async () => {
    const value = joinInput.trim();

    if (!value) {
      notify.warning(
        isRedstone
          ? t("请先填写房主给你的公网地址")
          : t("请先填写房主给你的房间码"),
      );

      return;
    }

    setBusy(true);
    try {
      await Join(providerId, value, settings.Player ?? "");
    } catch (ex) {
      notify.error(t("加入房间失败：{0}", { "0": errorMessage(ex) }));
    } finally {
      setBusy(false);
      void refreshRuntime(providerId);
    }
  };

  const doLeave = async () => {
    setBusy(true);
    try {
      // 只退出当前标签页这一家：另一家的会话继续跑
      await Leave(providerId);
    } catch (ex) {
      notify.error(t("退出房间失败：{0}", { "0": errorMessage(ex) }));
    } finally {
      setBusy(false);
      void refreshRuntime(providerId);
    }
  };

  const doSaveSettings = async () => {
    setSaving(true);
    try {
      await SaveSettings({ ...settings, Provider: providerId });
      setSettingsDirty(false);
      notify.success(t("联机设置已保存"));
      void refreshRuntime(providerId);
    } catch (ex) {
      notify.error(t("保存失败：{0}", { "0": errorMessage(ex) }));
    } finally {
      setSaving(false);
    }
  };

  const installTerracotta = async () => {
    setInstalling(true);
    try {
      const result = await InstallTerracotta();

      if (result?.ManualHint) {
        notify.warning(result.ManualHint);

        return;
      }
      if (result?.Path) {
        patchSettings({ TerracottaPath: result.Path });
        notify.success(
          t("已安装陶瓦联机 {0}，保存后生效", { "0": result.Version ?? "" }),
        );
        await refreshRuntime(providerId);
      }
    } catch (ex) {
      notify.error(t("安装失败：{0}", { "0": errorMessage(ex) }));
    } finally {
      setInstalling(false);
    }
  };

  const pickTerracottaBinary = async () => {
    try {
      const picked = await SelectFile(
        t("选择陶瓦联机可执行文件"),
        t("陶瓦联机"),
        // Windows 的发行物是 .exe；macOS/Linux 没有扩展名约定，不加过滤器
        isWindowsPlatform() ? "*.exe" : "",
      );

      if (!picked) return;
      patchSettings({ TerracottaPath: picked });
      notify.info(t("已选择 {0}，保存后生效", { "0": picked }));
    } catch {
      /* 用户取消 */
    }
  };

  const shutdownProvider = async () => {
    const ok = await confirm(
      t("关闭后台服务"),
      isRedstone
        ? t("将断开隧道并让中继释放端口，房间里的玩家会立刻掉线。")
        : t(
            "将结束本机的陶瓦联机进程。如果它是你自己打开的窗口，那个窗口也会一起关闭。",
          ),
      { confirmLabel: t("关闭服务"), severity: "warning" },
    );

    if (!ok) return;
    try {
      await ShutdownProvider(providerId);
      notify.success(t("后台服务已关闭"));
    } catch (ex) {
      notify.error(t("关闭失败：{0}", { "0": errorMessage(ex) }));
    } finally {
      void refreshRuntime(providerId);
    }
  };

  const copyValue = async (value: string, label: string) => {
    const ok = await copyText(value);

    if (ok) notify.success(t("{0} 已复制到剪贴板", { "0": t(label) }));
    else notify.error(t("复制失败，请手动选中文本复制"));
  };

  const providerTabs = providers.map((item) => ({
    key: item.ID,
    label: (
      <span className="flex items-center gap-1.5">
        {item.ID === "redstone" ? <Globe20Regular /> : <People20Regular />}
        {t(item.Name)}
      </span>
    ),
  }));

  const switchProvider = (next: string) => {
    setProviderId(next);
    if (settings) patchSettings({ Provider: next });
    setJoinInput("");
  };

  const serverOptions = useMemo(
    () =>
      servers.map((item) => ({
        key: item.ID,
        name: item.Name,
        port: item.Port,
        running: item.Running,
      })),
    [servers],
  );

  const selectedServer = servers.find((item) => item.ID === targetServerId);
  // 选中服务器后给一行"实际会被转发的地址"，避免用户以为填的是公网地址
  const selectedServerHint = selectedServer
    ? t("将转发到 127.0.0.1:{0}（端口取自服务器配置）", {
        "0": String(selectedServer.Port),
      })
    : "";

  // 节点下拉项：内置的加个标记，方便和自定义节点区分
  const relayNodeItems = useMemo(
    () =>
      relayNodes.map((node) => ({
        key: node.Address,
        name: node.Name,
        label: `${node.Name} · ${node.Address}${node.Builtin ? ` · ${t("内置")}` : ""}`,
      })),
    [relayNodes],
  );

  // ---- 会话卡片 ----
  const sessionCard = () => {
    if (!statusForCurrent) return null;

    const isHost = statusForCurrent.State === "hosting";
    const shareValue = statusForCurrent.Room || statusForCurrent.Address;
    const shareLabel = isHost
      ? isRedstone
        ? t("联机地址")
        : t("房间码")
      : t("房间地址");
    // 出错时可能根本没有房间（例如建房第一步就失败），标题别谎称"已加入的房间"
    const failedWithoutRoom =
      statusForCurrent.State === "error" &&
      !shareValue &&
      !statusForCurrent.JoinHost;

    return (
      <div className="flex flex-col gap-4">
        {/* 驾驶舱头部：标题 + 状态 + 主操作 */}
        <div className="flex flex-wrap items-center gap-3">
          <span className="flex size-11 flex-none items-center justify-center rounded-xl bg-primary/15 text-primary">
            {isHost ? <Rocket20Regular /> : <Link20Regular />}
          </span>
          <div className="min-w-0">
            <div className="flex items-center gap-2">
              <span className="text-[15px] font-semibold text-gray-800 dark:text-gray-200">
                {failedWithoutRoom
                  ? t("上次操作失败")
                  : isHost
                    ? t("我创建的房间")
                    : t("已加入的房间")}
              </span>
              {stateChip(statusForCurrent)}
            </div>
            <div className="truncate text-[11px] text-gray-400">
              {t(statusForCurrent.Phase || "")}
              {statusForCurrent.Since
                ? ` · ${t("开始于")} ${new Date(statusForCurrent.Since * 1000).toLocaleTimeString()}`
                : ""}
            </div>
          </div>
          <div className="ml-auto flex items-center gap-1.5">
            {currentState === "starting" ? <Spinner size="sm" /> : null}
            <Button
              color="danger"
              isDisabled={busy}
              size="sm"
              startContent={<ArrowExit20Regular />}
              variant="flat"
              onPress={() => void doLeave()}
            >
              {currentState === "starting" ? t("取消") : t("退出房间")}
            </Button>
          </div>
        </div>

        {/* 房间码主舞台：大字居中 + 渐变底 + 复制 */}
        {shareValue ? (
          <div className="overflow-hidden rounded-xl border border-primary/30 bg-gradient-to-br from-primary/15 via-primary/5 to-transparent px-6 py-7">
            <div className="text-center text-[11px] font-medium tracking-widest text-gray-400 uppercase">
              {shareLabel}
            </div>
            <div className="mt-2 flex flex-wrap items-center justify-center gap-3">
              <span className="font-mono text-2xl font-bold tracking-wide break-all text-center text-primary md:text-3xl">
                {shareValue}
              </span>
              <Button
                isIconOnly
                aria-label={t("复制")}
                size="sm"
                variant="flat"
                onPress={() => void copyValue(shareValue, shareLabel)}
              >
                <Copy20Regular />
              </Button>
            </div>
            {statusForCurrent.LocalAddress ? (
              <div className="mt-2 text-center font-mono text-[11px] text-gray-400">
                {statusForCurrent.LocalAddress}
              </div>
            ) : null}
          </div>
        ) : null}

        {/* 一键进服：整宽大按钮（本地直连地址存在时） */}
        {statusForCurrent.JoinHost ? (
          <Button
            fullWidth
            className="h-11 text-[14px] font-semibold"
            color="primary"
            isLoading={launching}
            startContent={launching ? undefined : <Play20Regular />}
            onPress={() => void joinGame()}
          >
            {t("一键进服")}
          </Button>
        ) : null}

        {statusForCurrent.Tip ? (
          <div className="flex items-start gap-1.5 text-[11px] leading-relaxed text-gray-500 dark:text-gray-400">
            <Info20Regular className="mt-0.5 flex-none" />
            <span>{t(statusForCurrent.Tip)}</span>
          </div>
        ) : null}

        {/* 中继节点说明（预检发现切换节点 / 全部不通时的运行期提示，原样展示） */}
        {statusForCurrent.RelayNote ? (
          <div className="flex items-start gap-1.5 rounded-lg border border-warning/30 bg-warning/5 px-3 py-2 text-[11px] leading-relaxed text-warning-600 dark:text-warning-400">
            <Globe20Regular className="mt-0.5 flex-none" />
            <span>{statusForCurrent.RelayNote}</span>
          </div>
        ) : null}

        {/* 当前连接数（红石中继不暴露成员名单，只报活跃隧道连接） */}
        {isRedstone && isHost ? (
          <div className="text-[11px] text-gray-500 dark:text-gray-400">
            {t("当前连接数")}：{statusForCurrent.Connections ?? 0}
          </div>
        ) : null}

        {/* 成员列表：陶瓦来自房间状态机；红石转发托管服务器时来自服务器的 list */}
        {statusForCurrent.Players?.length ? (
          <div className="flex flex-col gap-1.5">
            <span className="text-[12px] font-medium text-gray-600 dark:text-gray-300">
              {isRedstone ? t("在线玩家") : t("房间成员")} ·{" "}
              {statusForCurrent.Players.length}
            </span>
            <div className="flex flex-wrap gap-1.5">
              {statusForCurrent.Players.map((player, index) => (
                <span
                  key={`${player.Name}-${index}`}
                  className="flex items-center gap-1.5 rounded-full border nya-border px-2.5 py-1 text-[11px] text-gray-600 dark:text-gray-300"
                >
                  <People20Regular />
                  {player.Name}
                  <span
                    className={
                      player.Kind === "HOST"
                        ? "text-primary"
                        : "text-gray-400 dark:text-gray-500"
                    }
                  >
                    {player.Kind === "HOST" ? t("房主") : t("成员")}
                  </span>
                </span>
              ))}
            </div>
          </div>
        ) : null}

        {currentState === "error" && statusForCurrent.Error ? (
          <div className="flex items-start gap-1.5 rounded-lg border border-danger/30 bg-danger/5 px-3 py-2 text-[11px] leading-relaxed text-danger">
            <Warning20Regular className="mt-0.5 flex-none" />
            <span>{statusForCurrent.Error}</span>
          </div>
        ) : null}
      </div>
    );
  };

  return (
    <div className="relative h-full w-full flex flex-col overflow-hidden">
      {/* ============ 顶栏：供应商切换 + 状态徽章 + 全局动作 ============ */}
      <div className="flex-shrink-0 flex flex-col gap-2 px-6 pt-5 pb-3">
        <div className="flex flex-wrap items-center gap-2">
          {providers.length > 0 ? (
            <SegmentedTabs
              className="flex w-fit items-center gap-1 rounded-full border nya-border p-1"
              disabled={currentState === "starting" || busy}
              items={providerTabs}
              layoutId="nya-online-provider"
              value={providerId}
              onChange={switchProvider}
            />
          ) : null}

          {stateChip(statusForCurrent)}
          {provider?.Ready ? (
            <Chip
              color="success"
              size="sm"
              startContent={<CheckmarkCircle20Regular />}
              variant="flat"
            >
              {t("可用")}
            </Chip>
          ) : provider ? (
            <Chip
              color="warning"
              size="sm"
              startContent={<Warning20Regular />}
              variant="flat"
            >
              {t("需要先准备")}
            </Chip>
          ) : null}
          {provider?.NeedsMod ? (
            <Chip size="sm" variant="flat">
              {t("房主需装模组")}
            </Chip>
          ) : null}
          {statusForCurrent?.Players?.length ? (
            <Chip color="primary" size="sm" variant="flat">
              <People20Regular />
              {t("在线玩家 {0}", { "0": statusForCurrent.Players.length })}
            </Chip>
          ) : null}
          {isRedstone &&
          statusForCurrent?.State === "hosting" &&
          statusForCurrent.Connections ? (
            <Chip color="primary" size="sm" variant="flat">
              <People20Regular />
              {t("连接数 {0}", { "0": statusForCurrent.Connections })}
            </Chip>
          ) : null}

          <div className="flex-1" />

          <div className="flex flex-wrap items-center gap-1.5">
            <Button
              size="sm"
              startContent={<ArrowSync20Regular />}
              variant="flat"
              onPress={() => void refreshRuntime(providerId)}
            >
              {t("刷新")}
            </Button>
            {provider ? (
              <Button
                size="sm"
                startContent={<Open20Regular />}
                variant="flat"
                onPress={() => void OpenPage(provider.Homepage)}
              >
                {t("项目主页")}
              </Button>
            ) : null}
            <Button
              color={settingsOpen ? "primary" : "default"}
              size="sm"
              startContent={
                <ChevronRight20Regular
                  className={`transition-transform ${settingsOpen ? "rotate-180" : ""}`}
                />
              }
              variant="flat"
              onPress={() => setSettingsOpen((v) => !v)}
            >
              {t("联机设置")}
            </Button>
          </div>
        </div>

        {/* 首帧加载失败黄条：哪块数据没拿到一目了然，重试只补跑一次加载 */}
        {initProblems.length > 0 ? (
          <div className="flex flex-wrap items-center gap-2 rounded-lg border border-warning/30 bg-warning/5 px-3 py-2 text-[12px] text-warning-600 dark:text-warning-400">
            <Warning20Regular className="flex-none" />
            <span className="min-w-0 flex-1">{initProblems.join("；")}</span>
            <Button
              size="sm"
              variant="light"
              onPress={() => void loadInitial()}
            >
              {t("重试")}
            </Button>
          </div>
        ) : null}
      </div>

      {/* ============ 主体：剧场（会话驾驶舱 / 双入口大卡） + 设置抽屉 ============ */}
      <div className="flex-1 min-h-0 flex gap-4 px-6 pb-5">
        <div className="nya-panel flex min-w-0 flex-1 flex-col overflow-hidden rounded-large border nya-border">
          {/* 供应商说明条 */}
          {provider ? (
            <div className="flex flex-none flex-col gap-1.5 border-b nya-border px-5 pb-3 pt-4">
              <div className="flex flex-wrap items-center gap-2">
                <span className="text-[13px] font-semibold text-gray-800 dark:text-gray-200">
                  {t(provider.Name)}
                </span>
              </div>
              <div className="text-[11px] leading-relaxed text-gray-500 dark:text-gray-400">
                {t(provider.Summary)}
              </div>
              {provider.Hint ? (
                <div className="text-[10px] leading-tight text-gray-400 dark:text-gray-500">
                  {provider.Hint}
                </div>
              ) : null}
            </div>
          ) : null}

          <div className="nya-scroll min-h-0 flex-1 overflow-y-auto p-5">
            {/* 切供应商时整块淡入：两家的表单/会话不同，用 key 区分重播 */}
            <motion.div
              key={providerId}
              animate={{ opacity: 1, y: 0 }}
              className="flex min-h-full flex-col gap-3"
              initial={{ opacity: 0, y: 6 }}
              transition={{ duration: 0.18, ease: "easeOut" }}
            >
              {/* 另一家供应商也开着会话：只是告知——两家可以同时开着，互不打断 */}
              {otherProviderLive && otherStatus ? (
                <div className="flex items-center gap-2 rounded-lg border border-primary/30 bg-primary/5 px-4 py-3 text-[12px] text-primary">
                  <Info20Regular className="flex-none" />
                  <span className="min-w-0 flex-1">
                    {t(
                      "另一家联机（{0}）的会话也在进行中，两家可以同时开着。",
                      {
                        "0": t(
                          otherStatus.Provider === "redstone"
                            ? "红石联机"
                            : "陶瓦联机",
                        ),
                      },
                    )}
                  </span>
                  <Button
                    isDisabled={busy}
                    size="sm"
                    variant="flat"
                    onPress={() => switchProvider(otherProviderId)}
                  >
                    {t("切过去看看")}
                  </Button>
                </div>
              ) : null}

              {sessionLive || currentState === "error"
                ? sessionCard()
                : /* ============ 双入口大卡：无会话时的主舞台 ============ */
                  null}

              {/* 建房 / 加入：本标签页已有会话时收起，避免误触"换一局" */}
              {!sessionLive ? (
                <div className="grid flex-1 grid-cols-1 items-stretch gap-4 lg:grid-cols-2">
                  {/* ---- 创建房间大卡 ---- */}
                  <div className="flex flex-col gap-3 rounded-xl border nya-border bg-black/[0.03] p-5 dark:bg-white/[0.03]">
                    <div className="flex flex-col gap-1.5">
                      <span className="flex size-12 items-center justify-center rounded-2xl bg-primary/15 text-primary">
                        <Add20Regular className="h-6 w-6" />
                      </span>
                      <span className="mt-1.5 text-[15px] font-semibold text-gray-800 dark:text-gray-200">
                        {t("创建房间")}
                      </span>
                      <span className="text-[11px] leading-relaxed text-gray-400">
                        {isRedstone
                          ? t(
                              "把本机服务器或端口通过中继暴露到公网，建房自动启动",
                            )
                          : t("来?咱两练练?")}
                      </span>
                    </div>

                    {isRedstone ? (
                      <>
                        <SegmentedTabs
                          className="flex w-fit items-center gap-1 rounded-full border nya-border p-1"
                          items={[
                            {
                              key: "server",
                              label: (
                                <span className="flex items-center gap-1.5">
                                  <Server20Regular />

                                  {t("启动器服务器")}
                                </span>
                              ),
                            },
                            {
                              key: "manual",
                              label: (
                                <span className="flex items-center gap-1.5">
                                  <Link20Regular />
                                  {t("本机地址")}
                                </span>
                              ),
                            },
                          ]}
                          layoutId="nya-online-target"
                          value={targetMode}
                          onChange={(next) =>
                            setTargetMode(
                              next === "manual" ? "manual" : "server",
                            )
                          }
                        />

                        {targetMode === "server" ? (
                          serverOptions.length > 0 ? (
                            <SettingRow label="转发到哪台服务器">
                              <Select
                                aria-label={t("转发到哪台服务器")}
                                items={serverOptions}
                                placeholder={t("选择一台本机服务器")}
                                popoverProps={selectPopoverProps}
                                selectedKeys={
                                  targetServerId ? [targetServerId] : []
                                }
                                size="sm"
                                variant="bordered"
                                onSelectionChange={(keys) => {
                                  const key = [...keys][0];

                                  if (key !== undefined) {
                                    setTargetServerId(String(key));
                                    patchSettings({ ServerID: String(key) });
                                  }
                                }}
                              >
                                {(item) => (
                                  <SelectItem
                                    key={item.key}
                                    textValue={item.name}
                                  >
                                    {`${item.name} · ${item.port} · ${
                                      item.running ? t("运行中") : t("已停止")
                                    }`}
                                  </SelectItem>
                                )}
                              </Select>
                              {selectedServerHint ? (
                                <span className="text-[10px] text-gray-400 dark:text-gray-500">
                                  {selectedServerHint}
                                </span>
                              ) : null}
                            </SettingRow>
                          ) : (
                            <div className="rounded-lg border border-dashed border-gray-300/80 px-4 py-3 text-[11px] leading-relaxed text-gray-400 dark:border-gray-700">
                              {t("暂无托管服务器，可切到「本机地址」直接转发")}
                            </div>
                          )
                        ) : (
                          <SettingRow
                            hint="游戏里「对局域网开放」后，把聊天栏提示的端口填到这里；固定转发到 127.0.0.1。"
                            label="本机端口"
                          >
                            <Input
                              aria-label={t("本机端口")}
                              className="w-32 min-w-0 max-w-full"
                              max={65535}
                              min={1}
                              placeholder="25565"
                              size="sm"
                              type="number"
                              value={manualPort}
                              variant="bordered"
                              onValueChange={(value) => {
                                const digits = value.replace(/\D/g, "");

                                setManualPort(digits);
                                patchSettings({
                                  Target: `127.0.0.1:${digits || "25565"}`,
                                });
                              }}
                            />
                          </SettingRow>
                        )}
                      </>
                    ) : null}

                    <div className="mt-auto pt-2">
                      <Button
                        fullWidth
                        className="h-11 text-[14px] font-semibold"
                        color="primary"
                        isDisabled={busy || !provider?.Ready}
                        isLoading={busy}
                        startContent={busy ? undefined : <Rocket20Regular />}
                        onPress={() => void doHost()}
                      >
                        {t("创建房间")}
                      </Button>
                    </div>
                  </div>

                  {/* ---- 加入房间大卡 ---- */}
                  <div className="flex flex-col gap-3 rounded-xl border nya-border bg-black/[0.03] p-5 dark:bg-white/[0.03]">
                    <div className="flex flex-col gap-1.5">
                      <span className="flex size-12 items-center justify-center rounded-2xl bg-primary/15 text-primary">
                        <Play20Regular className="h-6 w-6" />
                      </span>
                      <span className="mt-1.5 text-[15px] font-semibold text-gray-800 dark:text-gray-200">
                        {t("加入房间")}
                      </span>
                      <span className="text-[11px] leading-relaxed text-gray-400">
                        {isRedstone
                          ? t("拿到房主的公网地址？粘贴进来直接连接")
                          : t("拿到房主的房间码？粘贴进来直接加入")}
                      </span>
                    </div>

                    <div className="my-auto flex flex-col gap-3 py-4">
                      <Input
                        aria-label={t("房间码")}
                        className="font-mono"
                        classNames={{ input: "text-[15px] tracking-wide" }}
                        placeholder={
                          isRedstone
                            ? "122.51.108.96:12345"
                            : "U/XXXX-XXXX-XXXX-XXXX"
                        }
                        size="lg"
                        value={joinInput}
                        variant="bordered"
                        onKeyDown={(event) => {
                          if (event.key === "Enter") void doJoin();
                        }}
                        onValueChange={setJoinInput}
                      />
                      {isRedstone ? (
                        <span className="text-[10px] text-gray-400 dark:text-gray-500">
                          {t("只填主机名时按 25565 端口处理。")}
                        </span>
                      ) : null}
                    </div>

                    <div className="mt-auto pt-2">
                      <Button
                        fullWidth
                        className="h-11 text-[14px] font-semibold"
                        isDisabled={busy}
                        isLoading={busy}
                        startContent={busy ? undefined : <Play20Regular />}
                        onPress={() => void doJoin()}
                      >
                        {t("加入房间")}
                      </Button>
                    </div>
                  </div>
                </div>
              ) : null}
            </motion.div>
          </div>
        </div>

        {/* ============ 设置抽屉：顶栏「联机设置」开关控制 ============ */}
        {settingsOpen ? (
          <div className="nya-panel flex w-[320px] flex-none flex-col overflow-hidden rounded-large border nya-border">
            <div className="flex flex-none items-center gap-2 border-b nya-border px-4 pb-3 pt-4">
              <span className="text-[13px] font-semibold text-gray-700 dark:text-gray-300">
                {t("联机设置")}
              </span>
              {settingsDirty ? (
                <Chip color="warning" size="sm" variant="flat">
                  {t("未保存")}
                </Chip>
              ) : null}
              <div className="ml-auto flex items-center gap-1">
                <Button
                  color={settingsDirty ? "primary" : "default"}
                  isDisabled={!settingsDirty || saving}
                  isLoading={saving}
                  size="sm"
                  startContent={
                    saving ? undefined : <CheckmarkCircle20Regular />
                  }
                  variant="flat"
                  onPress={() => void doSaveSettings()}
                >
                  {t("保存")}
                </Button>
                <Button
                  isIconOnly
                  size="sm"
                  title={t("收起设置")}
                  variant="light"
                  onPress={() => setSettingsOpen(false)}
                >
                  <ChevronRight20Regular />
                </Button>
              </div>
            </div>

            <div className="nya-scroll flex min-h-0 flex-1 flex-col gap-4 overflow-y-auto px-4 pb-4 pt-3">
              <SettingRow label="玩家昵称">
                <Input
                  placeholder={t("留空使用当前账号名")}
                  size="sm"
                  value={settings.Player}
                  variant="bordered"
                  onValueChange={(value) => patchSettings({ Player: value })}
                />
              </SettingRow>

              {isRedstone ? (
                <div className="flex flex-col gap-3 rounded-lg border nya-border p-3">
                  <span className="flex items-center gap-1.5 text-[12px] font-semibold text-gray-700 dark:text-gray-300">
                    <Globe20Regular />
                    {t("红石联机")}
                  </span>

                  {/* 本机隧道状态：红石联机日常唯一需要关注的行 */}
                  <div className="flex items-center justify-between gap-2">
                    <span className="text-[11px] text-gray-500 dark:text-gray-400">
                      {t("本机隧道")}：
                      <span className="font-mono">
                        {runtime?.Running
                          ? `${t("运行中")} · ${runtime.Port || "-"}`
                          : t("未运行")}
                      </span>
                    </span>
                    <Button
                      color="danger"
                      isDisabled={!runtime?.Running}
                      size="sm"
                      startContent={<Dismiss20Regular />}
                      variant="light"
                      onPress={() => void shutdownProvider()}
                    >
                      {t("断开隧道")}
                    </Button>
                  </div>

                  {/* 高级设置默认折叠：中继管理 / API Key 等都是一次性配置 */}
                  <button
                    className="flex items-center gap-1 self-start text-[12px] font-medium text-gray-500 transition-colors hover:text-primary"
                    type="button"
                    onClick={() => setShowAdvanced((v) => !v)}
                  >
                    <ChevronRight20Regular
                      className={`transition-transform ${showAdvanced ? "rotate-90" : ""}`}
                    />
                    {t("高级设置")}
                  </button>

                  {showAdvanced ? (
                    <div className="flex flex-col gap-3 border-t border-dashed border-gray-200 pt-3 dark:border-gray-700">
                      <SettingRow
                        hint="建房前会自动预检，当前节点连不上时会自动换最快可达的节点；API Key 也已自动管理，一般无需手动调整。"
                        label="中继节点"
                      >
                        <div className="flex flex-col items-end gap-2">
                          <Select
                            aria-label={t("选择中继节点")}
                            className="w-72 min-w-0 max-w-full [&_*]:min-w-0"
                            items={relayNodeItems}
                            placeholder={t("从节点列表选择")}
                            popoverProps={selectPopoverProps}
                            selectedKeys={
                              settings.RedstoneRelay
                                ? [settings.RedstoneRelay]
                                : []
                            }
                            size="sm"
                            onSelectionChange={(keys) => {
                              const key = [...keys][0];

                              if (key !== undefined) {
                                patchSettings({ RedstoneRelay: String(key) });
                              }
                            }}
                          >
                            {(item) => (
                              <SelectItem key={item.key} textValue={item.name}>
                                {item.label}
                              </SelectItem>
                            )}
                          </Select>
                          <Button
                            isLoading={probing}
                            size="sm"
                            startContent={
                              probing ? undefined : <Flash20Regular />
                            }
                            variant="flat"
                            onPress={() => void pickFastestRelay()}
                          >
                            {t("自动选最快节点")}
                          </Button>
                          <Button
                            isLoading={probingNodes}
                            size="sm"
                            startContent={
                              probingNodes ? undefined : <ArrowSync20Regular />
                            }
                            variant="flat"
                            onPress={() => void probeRelayNodes()}
                          >
                            {t("测速")}
                          </Button>
                        </div>
                      </SettingRow>

                      <SettingRow
                        hint="自己的节点，每行一条：名称=地址。"
                        label="自定义节点列表"
                      >
                        <div className="flex flex-col items-end gap-2">
                          <Textarea
                            aria-label={t("自定义节点列表")}
                            className="[&_*]:min-w-0"
                            minRows={2}
                            placeholder="我家节点=1.2.3.4"
                            size="sm"
                            value={relayListText}
                            variant="bordered"
                            onValueChange={setRelayListText}
                          />
                          <Button
                            isLoading={savingRelayList}
                            size="sm"
                            variant="flat"
                            onPress={() => void saveRelayList()}
                          >
                            {t("保存节点列表")}
                          </Button>
                        </div>
                      </SettingRow>

                      <SettingRow
                        hint="留空则自动生成；换新密钥后保存，下次建房生效。"
                        label="联机密钥"
                      >
                        <div className="flex flex-col items-end gap-2">
                          <Input
                            aria-label={t("联机密钥")}
                            className="w-72 min-w-0 max-w-full font-mono"
                            placeholder={
                              settings.HasRedstoneKey
                                ? t("已保存（输入以更换）")
                                : ""
                            }
                            size="sm"
                            value={settings.RedstoneKey ?? ""}
                            variant="bordered"
                            onValueChange={(value) =>
                              patchSettings({ RedstoneKey: value })
                            }
                          />
                          <Button
                            size="sm"
                            startContent={<ArrowSync20Regular />}
                            variant="flat"
                            onPress={() => void regenerateAPIKey()}
                          >
                            {t("重新生成")}
                          </Button>
                        </div>
                      </SettingRow>

                      <SettingRow
                        hint="隧道最多同时转发多少个玩家连接。"
                        label="并发上限"
                      >
                        <Select
                          aria-label={t("并发上限")}
                          className="w-28 min-w-0 max-w-full [&_*]:min-w-0"
                          popoverProps={selectPopoverProps}
                          selectedKeys={[String(settings.MaxPlayers || 8)]}
                          size="sm"
                          onSelectionChange={(keys) => {
                            const key = [...keys][0];

                            if (key !== undefined) {
                              patchSettings({ MaxPlayers: Number(key) || 8 });
                            }
                          }}
                        >
                          {["2", "4", "8", "16", "32"].map((value) => (
                            <SelectItem key={value}>{value}</SelectItem>
                          ))}
                        </Select>
                      </SettingRow>
                    </div>
                  ) : null}
                </div>
              ) : (
                <div className="flex flex-col gap-3 rounded-lg border nya-border p-3">
                  <span className="flex items-center gap-1.5 text-[12px] font-semibold text-gray-700 dark:text-gray-300">
                    <People20Regular />
                    {t("陶瓦联机")}
                  </span>

                  <div className="flex flex-col gap-1 text-[11px] text-gray-500 dark:text-gray-400">
                    <span>
                      {t("后台服务")}：
                      {runtime?.Running
                        ? `${t("运行中")}${runtime.Version ? ` · v${runtime.Version}` : ""}`
                        : t("未运行")}
                    </span>
                    <span className="break-all font-mono text-[10px] text-gray-400 dark:text-gray-500">
                      {runtime?.Binary || t("尚未找到可执行文件")}
                    </span>
                  </div>

                  <Button
                    color="primary"
                    isLoading={installing}
                    size="sm"
                    startContent={
                      installing ? undefined : <ArrowDownload20Regular />
                    }
                    variant="flat"
                    onPress={() => void installTerracotta()}
                  >
                    {t("自动下载并安装")}
                  </Button>

                  <Button
                    size="sm"
                    startContent={<FolderOpen20Regular />}
                    variant="flat"
                    onPress={() => void pickTerracottaBinary()}
                  >
                    {t("选择可执行文件")}
                  </Button>
                  <Button
                    size="sm"
                    startContent={<ArrowSync20Regular />}
                    variant="light"
                    onPress={() => void refreshRuntime(providerId)}
                  >
                    {t("重新检测")}
                  </Button>
                  <Button
                    color="danger"
                    isDisabled={!runtime?.Running}
                    size="sm"
                    startContent={<Dismiss20Regular />}
                    variant="flat"
                    onPress={() => void shutdownProvider()}
                  >
                    {t("关闭后台服务")}
                  </Button>
                </div>
              )}
            </div>
          </div>
        ) : null}
      </div>
    </div>
  );
};

export default OnlinePage;
