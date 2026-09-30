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
import type { mcserver } from "../../../wailsjs/go/models";

import React, { useCallback, useEffect, useRef, useState } from "react";
import { Button, Select, SelectItem, Spinner } from "@heroui/react";
import {
  ArrowClockwise20Regular as RefreshIcon,
  Open20Regular,
  Play20Filled,
  Server20Regular,
  Stop20Filled,
} from "@fluentui/react-icons";

import {
  ListServers,
  PollServer,
  StartServer,
  StopServer,
} from "../../../wailsjs/go/bindings/ServerHostAPI";
import { GetValue, SetValue } from "../../../wailsjs/go/bindings/ConfigAPI";
import { navigateToPage } from "../../lib/navigation";
import { popoverMotionProps } from "../../lib/motion";
import { t } from "../../i18n";

import HomeCard from "./HomeCard";

/** 选中的服务器 id（launcher.yaml 键） */
const SELECTED_KEY = "homeHostedServerId";

/** 运行指标轮询间隔 */
const POLL_INTERVAL = 2000;

const STATUS_LABEL: Record<string, string> = {
  stopped: "已停止",
  starting: "启动中",
  running: "运行中",
  stopping: "停止中",
};

const STATUS_CLASS: Record<string, string> = {
  stopped: "bg-gray-500/10 text-gray-500",
  starting: "bg-amber-500/10 text-amber-600",
  running: "bg-emerald-500/10 text-emerald-600",
  stopping: "bg-amber-500/10 text-amber-600",
};

/**
 * 托管服务器状态卡片：下拉选择本机托管的服务器，展示运行状态、
 * 在线人数与 CPU/内存占用，可直接启动/软停止（详细管理进服务器页）。
 */
const HostedServerCard: React.FC = () => {
  const [servers, setServers] = useState<mcserver.ServerInfo[]>([]);
  const [selectedId, setSelectedId] = useState("");
  const [busy, setBusy] = useState(false);
  /** 运行指标（仅选中服务器运行时有值） */
  const [runtime, setRuntime] = useState<mcserver.Snapshot | null>(null);
  // alive 防止卡片卸载后轮询继续回填状态
  const alive = useRef(true);
  /** 当前选中的服务器 id（轮询回调里核对用，避免旧请求覆盖新选中项） */
  const selectedIdRef = useRef("");

  useEffect(() => {
    alive.current = true;

    return () => {
      alive.current = false;
    };
  }, []);

  // 必须在下面的轮询 effect 之前声明：effect 按声明顺序执行，
  // 这样切换选中项时 ref 会先更新，旧请求回来才认得出来
  useEffect(() => {
    selectedIdRef.current = selectedId;
  }, [selectedId]);

  const refreshList = useCallback(async () => {
    try {
      const list = await ListServers();

      if (!alive.current) return;
      setServers(list);
      // 选中的服务器被删除时回落到第一台
      if (list.length > 0 && !list.some((server) => server.ID === selectedId)) {
        setSelectedId(list[0].ID);
      }
    } catch {
      /* 后端暂不可用：保留下次重试 */
    }
  }, [selectedId]);

  // 首次加载：读列表 + 恢复上次选中的服务器
  useEffect(() => {
    void refreshList();
    void GetValue(SELECTED_KEY).then((saved) => {
      if (alive.current && saved) setSelectedId(saved);
    });
  }, [refreshList]);

  const select = (id: string) => {
    setSelectedId(id);
    setRuntime(null);
    void SetValue(SELECTED_KEY, id).catch(() => undefined);
  };

  const selected = servers.find((server) => server.ID === selectedId) ?? null;

  // 状态轮询：选中服务器存在时定时拉快照（运行中才有指标，停止时清空）
  useEffect(() => {
    if (!selectedId) return;
    // 请求是异步的：切到别的服务器后，旧请求回来会把上一台的 CPU/内存
    // 写到新服务器的名字下面，所以回写前再核对一次当前选中的 id
    const requestId = selectedId;
    const poll = async () => {
      try {
        const snapshot = await PollServer(requestId, 0);

        if (!alive.current || requestId !== selectedIdRef.current) return;
        if (snapshot.Status === "stopped") setRuntime(null);
        else setRuntime(snapshot);
        // 状态跃迁（启动完成/退出）同步进列表
        void refreshList();
      } catch {
        if (alive.current && requestId === selectedIdRef.current) {
          setRuntime(null);
        }
      }
    };

    void poll();
    const timer = window.setInterval(() => void poll(), POLL_INTERVAL);

    return () => window.clearInterval(timer);
  }, [selectedId, refreshList]);

  const doStart = async () => {
    if (!selected) return;
    setBusy(true);
    try {
      await StartServer(selected.ID);
      await refreshList();
    } finally {
      if (alive.current) setBusy(false);
    }
  };

  const doStop = async () => {
    if (!selected) return;
    setBusy(true);
    try {
      await StopServer(selected.ID, false);
      await refreshList();
    } finally {
      if (alive.current) setBusy(false);
    }
  };

  const status = selected?.Status ?? "stopped";
  const isRunning = status === "running" || status === "starting";
  const value =
    servers.length === 0
      ? t("暂无托管服务器")
      : selected
        ? t(STATUS_LABEL[status] ?? status)
        : t("待选择");

  return (
    <HomeCard
      action={
        <Button
          isIconOnly
          className="h-7 w-7 min-w-7 text-gray-400"
          radius="full"
          size="sm"
          title={t("刷新服务器列表")}
          variant="light"
          onClick={() => void refreshList()}
        >
          <RefreshIcon />
        </Button>
      }
      icon={<Server20Regular />}
      label={t("托管服务器")}
      value={value}
    >
      {servers.length === 0 ? (
        <div className="nya-enter flex flex-col items-center gap-1.5 rounded-2xl bg-black/5 px-3 py-5 text-center dark:bg-white/5">
          <span className="flex size-9 items-center justify-center rounded-xl bg-primary/10 text-primary">
            <Server20Regular className="h-5 w-5" />
          </span>
          <span className="text-xs leading-relaxed text-gray-400">
            {t("暂无托管服务器")}
          </span>
          <Button
            className="h-7 text-xs"
            size="sm"
            variant="flat"
            onPress={() => navigateToPage("multiplayer", "servers")}
          >
            {t("前往服务器页")}
          </Button>
        </div>
      ) : (
        <>
          {/* 服务器选择下拉 */}
          <Select
            aria-label={t("选择服务器")}
            classNames={{
              trigger:
                "min-h-9 rounded-xl bg-default-100/80 data-[hover=true]:bg-default-200",
            }}
            items={servers.map((server) => ({
              key: server.ID,
              label: `${server.Name}（${server.MCVersion}）`,
            }))}
            popoverProps={{ motionProps: popoverMotionProps }}
            selectedKeys={selected ? [selected.ID] : []}
            size="sm"
            variant="flat"
            onSelectionChange={(keys) => {
              const key = String(Array.from(keys)[0] ?? "");

              if (key) select(key);
            }}
          >
            {(item) => <SelectItem key={item.key}>{item.label}</SelectItem>}
          </Select>

          {selected ? (
            <div className="nya-enter flex flex-col gap-2.5 rounded-2xl bg-black/5 px-3 py-2.5 dark:bg-white/5">
              {/* 状态徽标 + 在线人数 */}
              <div className="flex min-w-0 items-center gap-1.5">
                <span
                  className={`flex-none rounded-full px-2 py-0.5 text-[10px] font-medium ${STATUS_CLASS[status] ?? STATUS_CLASS.stopped}`}
                >
                  {status === "starting" || status === "stopping" ? (
                    <Spinner className="mr-1" size="sm" />
                  ) : null}
                  {t(STATUS_LABEL[status] ?? status)}
                </span>
                {status === "running" ? (
                  <span className="flex-none text-[11px] text-gray-400 tabular-nums">
                    {t("在线玩家")} {selected.Players}/{selected.MaxPlayers}
                  </span>
                ) : null}
                <span className="min-w-0 flex-1 truncate text-right text-[10px] text-gray-400 tabular-nums">
                  {t("端口")} {selected.Port}
                </span>
              </div>

              {/* 运行指标（仅运行中有） */}
              {runtime && status !== "stopped" ? (
                <div className="flex items-center gap-1.5 text-[10px] text-gray-400 tabular-nums">
                  <span>
                    {t("CPU")} {runtime.CPUPercent.toFixed(1)}%
                  </span>
                  <span>·</span>
                  <span>
                    {t("内存")} {runtime.MemoryMB.toFixed(0)} MB
                  </span>
                </div>
              ) : null}

              {/* 启停 + 跳转管理 */}
              <div className="flex items-center gap-1.5">
                {isRunning ? (
                  <Button
                    className="h-7 flex-1 text-xs"
                    color="danger"
                    isDisabled={busy}
                    size="sm"
                    variant="flat"
                    onPress={() => void doStop()}
                  >
                    <Stop20Filled className="h-3.5 w-3.5" />
                    {t("停止")}
                  </Button>
                ) : (
                  <Button
                    className="h-7 flex-1 text-xs"
                    color="primary"
                    isDisabled={busy}
                    size="sm"
                    variant="flat"
                    onPress={() => void doStart()}
                  >
                    <Play20Filled className="h-3.5 w-3.5" />
                    {t("启动")}
                  </Button>
                )}
                <Button
                  className="h-7 text-xs"
                  isDisabled={status === "stopping"}
                  size="sm"
                  variant="light"
                  onPress={() => navigateToPage("multiplayer", "servers")}
                >
                  <Open20Regular className="h-3.5 w-3.5" />
                  {t("管理")}
                </Button>
              </div>
            </div>
          ) : null}
        </>
      )}
    </HomeCard>
  );
};

export default HostedServerCard;
