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
import type { network } from "../../../wailsjs/go/models";

import React, { useCallback, useEffect, useRef, useState } from "react";
import { Button, Input, Spinner } from "@heroui/react";
import {
  Add20Regular,
  ArrowClockwise20Regular as RefreshIcon,
  Dismiss20Regular,
  Play20Filled,
  Server20Regular,
  Warning20Regular,
} from "@fluentui/react-icons";

import {
  ParseServerAddress,
  PingServer,
} from "../../../wailsjs/go/bindings/ServerAPI";
import { GetValue, SetValue } from "../../../wailsjs/go/bindings/ConfigAPI";
import {
  errorMessage,
  localFileUrl,
  renderMinecraftFormatting,
  stripMinecraftFormatting,
} from "../../lib/home";
import { t } from "../../i18n";

import HomeCard from "./HomeCard";

/** 服务器列表（launcher.yaml 键，JSON 数组 [{host, port}]） */
const SERVERS_KEY = "quickJoinServers";

/** 已保存的服务器（host + port 已解析） */
interface SavedServer {
  host: string;
  port: number;
}

/** 单个服务器的查询状态（entries 以 host:port 为键） */
interface ServerEntryState {
  status: network.MinecraftServerStatus | null;
  latencyMs: number | null;
  error: string;
  isPinging: boolean;
}

const EMPTY_ENTRY: ServerEntryState = {
  status: null,
  latencyMs: null,
  error: "",
  isPinging: false,
};

const serverKey = (server: SavedServer): string =>
  `${server.host}:${server.port}`;

/** 展示地址：默认端口 25565 省略不写 */
const serverAddress = (server: SavedServer): string =>
  server.port === 25565 ? server.host : `${server.host}:${server.port}`;

/** 解析持久化的服务器列表 JSON；格式非法返回 null */
function parseSavedServers(raw: string): SavedServer[] | null {
  if (!raw) return null;
  try {
    const parsed: unknown = JSON.parse(raw);

    if (!Array.isArray(parsed) || parsed.length === 0) return null;
    const servers: SavedServer[] = [];

    for (const item of parsed) {
      const host = (item as { host?: unknown })?.host;
      const port = (item as { port?: unknown })?.port;

      if (typeof host !== "string" || !host.trim()) continue;
      const portNumber = Number(port);

      if (
        !Number.isInteger(portNumber) ||
        portNumber <= 0 ||
        portNumber > 65535
      )
        continue;
      servers.push({ host: host.trim(), port: portNumber });
    }

    return servers.length > 0 ? servers : null;
  } catch {
    return null;
  }
}

const latencyTextClass = (latencyMs: number | null): string =>
  latencyMs === null
    ? ""
    : latencyMs < 80
      ? "text-emerald-500"
      : latencyMs < 200
        ? "text-amber-500"
        : "text-red-500";

interface QuickServerCardProps {
  /** 启动流程进行中（准备/运行）：禁止再次启动 */
  isBusy: boolean;
  /** 直接进服启动；返回错误信息（null = 已发起启动） */
  onJoin: (host: string, port: number) => Promise<string | null>;
}

/**
 * 快速进服卡片（多服务器版）：维护一个可增删的服务器列表，
 * 每台服务器独立 Server List Ping（MOTD / 人数 / 延迟），条目上直接进服。
 */
const QuickServerCard: React.FC<QuickServerCardProps> = ({
  isBusy,
  onJoin,
}) => {
  const [servers, setServers] = useState<SavedServer[]>([]);
  const [entries, setEntries] = useState<Record<string, ServerEntryState>>({});
  const [address, setAddress] = useState("");
  const [addError, setAddError] = useState("");
  const [isJoiningKey, setIsJoiningKey] = useState<string | null>(null);
  // alive 防止卡片卸载后回填状态
  const alive = useRef(true);

  useEffect(() => {
    alive.current = true;

    return () => {
      alive.current = false;
    };
  }, []);

  const setEntry = useCallback(
    (key: string, patch: Partial<ServerEntryState>) => {
      setEntries((prev) => ({
        ...prev,
        [key]: { ...(prev[key] ?? EMPTY_ENTRY), ...patch },
      }));
    },
    [],
  );

  /** 查询单台服务器；结果写入对应条目（内部吞错，错误进条目） */
  const pingOne = useCallback(
    async (server: SavedServer) => {
      const key = serverKey(server);

      setEntry(key, { isPinging: true, error: "" });
      const startedAt = performance.now();

      try {
        const result = (await PingServer(
          server.host,
          server.port,
        )) as network.MinecraftServerStatus;

        if (!alive.current) return;
        setEntry(key, {
          status: result ?? null,
          latencyMs: Math.round(performance.now() - startedAt),
          error: result ? "" : t("服务器未返回状态信息"),
          isPinging: false,
        });
      } catch (err) {
        if (!alive.current) return;
        setEntry(key, {
          status: null,
          latencyMs: null,
          error: errorMessage(err) || t("无法连接服务器"),
          isPinging: false,
        });
      }
    },
    [setEntry],
  );

  const persistServers = useCallback((next: SavedServer[]) => {
    void SetValue(SERVERS_KEY, JSON.stringify(next)).catch(() => undefined);
  }, []);

  // 读取服务器列表，然后全量查询
  useEffect(() => {
    let settled = false;
    const pingAll = (list: SavedServer[]) => {
      list.forEach((server) => void pingOne(server));
    };

    GetValue(SERVERS_KEY)
      .then((raw) => {
        const saved = parseSavedServers(raw ?? "");

        if (!saved) return;
        if (!alive.current || settled) return;
        settled = true;
        setServers(saved);
        pingAll(saved);
      })
      .catch(() => undefined);
  }, [pingOne]);

  /** 添加服务器：后端解析校验 → 去重 → 入列即查询 */
  const handleAdd = async () => {
    const raw = address.trim();

    setAddError("");
    if (!raw) return;
    try {
      const parsed = (await ParseServerAddress(raw)) as network.ServerAddress;
      const server: SavedServer = { host: parsed.Host, port: parsed.Port };

      if (
        servers.some((existing) => serverKey(existing) === serverKey(server))
      ) {
        setAddError(t("这台服务器已经在列表里了喵"));

        return;
      }
      const next = [...servers, server];

      setServers(next);
      persistServers(next);
      setAddress("");
      void pingOne(server);
    } catch (err) {
      setAddError(errorMessage(err) || t("无法解析服务器地址"));
    }
  };

  const handleRemove = (server: SavedServer) => {
    const key = serverKey(server);
    const next = servers.filter((existing) => serverKey(existing) !== key);

    setServers(next);
    persistServers(next);
    setEntries((prev) => {
      const rest = { ...prev };

      delete rest[key];

      return rest;
    });
  };

  const handleJoin = async (server: SavedServer) => {
    const key = serverKey(server);

    setIsJoiningKey(key);
    setEntry(key, { error: "" });
    try {
      const failed = await onJoin(server.host, server.port);

      if (alive.current && failed) setEntry(key, { error: failed });
    } catch (err) {
      if (alive.current)
        setEntry(key, { error: errorMessage(err) || t("启动失败") });
    } finally {
      if (alive.current) setIsJoiningKey(null);
    }
  };

  const isAnyPinging = servers.some(
    (server) => entries[serverKey(server)]?.isPinging ?? false,
  );
  const onlineCount = servers.filter(
    (server) => entries[serverKey(server)]?.status,
  ).length;
  const value =
    servers.length === 0
      ? t("待添加")
      : isAnyPinging && onlineCount === 0
        ? t("查询中…")
        : t("{0}/{1} 在线", { "0": onlineCount, "1": servers.length });

  return (
    <HomeCard
      action={
        <Button
          isIconOnly
          className="h-7 w-7 min-w-7 text-gray-400"
          isDisabled={isAnyPinging || servers.length === 0}
          radius="full"
          size="sm"
          title={t("重新查询全部服务器")}
          variant="light"
          onClick={() => servers.forEach((server) => void pingOne(server))}
        >
          {isAnyPinging ? <Spinner size="sm" /> : <RefreshIcon />}
        </Button>
      }
      icon={<Server20Regular />}
      label={t("快速进服")}
      value={value}
    >
      {/* 添加服务器：回车或 ➕ */}
      <Input
        aria-label={t("服务器地址")}
        classNames={{
          inputWrapper:
            "rounded-lg bg-default-100/80 data-[hover=true]:bg-default-200",
        }}
        endContent={
          <Button
            isIconOnly
            className="h-6 w-6 min-w-6 text-gray-400"
            isDisabled={!address.trim()}
            radius="full"
            size="sm"
            title={t("添加服务器")}
            variant="light"
            onClick={() => void handleAdd()}
          >
            <Add20Regular className="h-4 w-4" />
          </Button>
        }
        placeholder={t("添加服务器，如 mc.example.com")}
        size="sm"
        value={address}
        onKeyDown={(event) => {
          if (event.key === "Enter") void handleAdd();
        }}
        onValueChange={(next) => {
          setAddress(next);
          setAddError("");
        }}
      />

      {servers.length === 0 ? (
        <div className="nya-enter flex flex-col items-center gap-1.5 rounded-lg bg-black/5 px-3 py-5 text-center dark:bg-white/5">
          <span className="flex size-9 items-center justify-center rounded-lg bg-primary/10 text-primary">
            <Server20Regular className="h-5 w-5" />
          </span>
          <span className="text-xs leading-relaxed text-gray-400">
            {t("暂无服务器")}
          </span>
        </div>
      ) : (
        <ul className="nya-scroll max-h-80 space-y-1.5 overflow-y-auto pr-0.5">
          {servers.map((server) => {
            const key = serverKey(server);
            const entry = entries[key] ?? EMPTY_ENTRY;
            const motdText = entry.status
              ? stripMinecraftFormatting(entry.status.Motd).trim()
              : "";
            const isEntryJoining = isJoiningKey === key;

            return (
              <li
                key={key}
                className="nya-enter flex items-start gap-2 rounded-lg bg-black/5 px-2.5 py-2 dark:bg-white/5"
              >
                {entry.status?.IconPath ? (
                  <img
                    alt=""
                    className="size-8 flex-none rounded-lg object-cover shadow-sm [image-rendering:pixelated]"
                    src={localFileUrl(entry.status.IconPath)}
                  />
                ) : (
                  <span
                    className={`flex size-8 flex-none items-center justify-center rounded-lg ${
                      entry.error
                        ? "bg-danger/10 text-danger"
                        : "bg-primary/10 text-primary"
                    }`}
                  >
                    {entry.isPinging ? (
                      <Spinner size="sm" />
                    ) : (
                      <Server20Regular className="h-4 w-4" />
                    )}
                  </span>
                )}

                <div className="flex min-w-0 flex-1 flex-col gap-0.5">
                  {/* 地址 + 人数 + 延迟 */}
                  <div className="flex min-w-0 items-baseline gap-1.5">
                    <span className="truncate text-xs font-semibold text-gray-800 dark:text-gray-100">
                      {serverAddress(server)}
                    </span>
                    {entry.status ? (
                      <>
                        <span className="flex-none rounded-full bg-emerald-500/10 px-1.5 text-[10px] font-medium text-emerald-600 tabular-nums dark:text-emerald-300">
                          {entry.status.OnlinePlayers}/{entry.status.MaxPlayers}
                        </span>
                        {entry.latencyMs !== null ? (
                          <span
                            className={`flex-none text-[10px] tabular-nums ${latencyTextClass(entry.latencyMs)}`}
                          >
                            {entry.latencyMs} ms
                          </span>
                        ) : null}
                      </>
                    ) : entry.isPinging ? (
                      <span className="flex-none text-[10px] text-gray-400">
                        {t("查询中…")}
                      </span>
                    ) : null}
                  </div>

                  {/* MOTD（彩色）：完整换行显示，长文本自动折行不截断 */}
                  <span className="text-[11px] leading-snug whitespace-pre-line break-words text-gray-600 dark:text-gray-300">
                    {entry.error ? (
                      <span className="text-danger">{entry.error}</span>
                    ) : entry.isPinging ? (
                      "…"
                    ) : entry.status ? (
                      motdText ? (
                        renderMinecraftFormatting(entry.status.Motd)
                      ) : (
                        // 与 Modrinth App 相同的空 MOTD 占位文案
                        t("一个 Minecraft 服务器")
                      )
                    ) : (
                      t("未查询")
                    )}
                  </span>
                </div>

                {/* 进服 / 移除 */}
                <div className="flex flex-none flex-col gap-1">
                  <Button
                    isIconOnly
                    className="h-6 w-6 min-w-6 bg-primary/15 text-primary"
                    isDisabled={isBusy}
                    isLoading={isEntryJoining}
                    radius="full"
                    size="sm"
                    title={t("进入 {0}", { "0": serverAddress(server) })}
                    variant="flat"
                    onClick={() => void handleJoin(server)}
                  >
                    {!isEntryJoining ? (
                      <Play20Filled className="h-3.5 w-3.5" />
                    ) : null}
                  </Button>
                  <Button
                    isIconOnly
                    className="h-6 w-6 min-w-6 text-gray-400"
                    radius="full"
                    size="sm"
                    title={t("从列表移除")}
                    variant="light"
                    onClick={() => handleRemove(server)}
                  >
                    <Dismiss20Regular className="h-3.5 w-3.5" />
                  </Button>
                </div>
              </li>
            );
          })}
        </ul>
      )}

      {addError ? (
        <div className="nya-enter flex items-start gap-2 rounded-lg bg-danger/10 px-2.5 py-2 text-[11px] text-danger">
          <span className="mt-px flex-none">
            <Warning20Regular className="h-4 w-4" />
          </span>
          <span className="break-all">{addError}</span>
        </div>
      ) : null}

      {isBusy && servers.length > 0 ? (
        <div className="text-center text-[11px] text-gray-400">
          {t("游戏已在运行，停止后才能再次启动")}
        </div>
      ) : null}
    </HomeCard>
  );
};

export default QuickServerCard;
