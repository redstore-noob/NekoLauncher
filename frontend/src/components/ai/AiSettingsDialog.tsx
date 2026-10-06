/*
 * AI 设置弹窗（从 layouts/ai.tsx 拆出）：模型供应商 / API 地址与 Key /
 * 本地引擎检测 / 实例操作权限 / 高级设置。
 *
 * 状态全部由 AiPage 持有，这里只读 + 回调上抛（onSettingsChange /
 * onProviderChange / onReprobe / onResetAlwaysAllowed / onSave），弹窗自身
 * 只保留"高级设置"的展开态。
 */
import type { AiProvider, AiSettings } from "../../lib/ai/types";

import React, { useState } from "react";
import {
  Button,
  Divider,
  Input,
  Modal,
  ModalContent,
  Select,
  SelectItem,
  Switch,
} from "@heroui/react";
import {
  ChevronDown20Regular as ChevronDownIcon,
  ChevronRight20Regular as ChevronRightIcon,
  Settings20Regular as SettingsIcon,
} from "@fluentui/react-icons";

import { selectPopoverProps } from "../../lib/motion";
import { ModalShell, modalBehaviorProps } from "../modal-shell";
import { t } from "../../i18n";
import {
  BUILTIN_PROVIDERS,
  LOCAL_ENGINE_HINTS,
  usesEditableBaseUrl,
} from "../../lib/ai/providers";

/** 本地引擎 /models 探测结果（AiPage 持有，弹窗只读） */
export type LocalProbeState =
  | { status: "idle" }
  | { status: "probing" }
  | { status: "online"; models: string[] }
  | { status: "offline" };

interface Props {
  open: boolean;
  onClose: () => void;
  settings: AiSettings;
  /** 局部更新设置项（与 setState 的函数式更新同签名） */
  onSettingsChange: (updater: (prev: AiSettings) => AiSettings) => void;
  currentProvider: AiProvider;
  /** 切换供应商：补默认模型 / API 格式，本地引擎清空手填地址 */
  onProviderChange: (providerId: string) => void;
  localProbe: LocalProbeState;
  onReprobe: () => void;
  /** 生效的 API 端点（展示"当前配置"用） */
  effectiveBaseUrl: string;
  /** 本次运行期内"总是允许"的工具名列表 */
  alwaysAllowedList: string[];
  onResetAlwaysAllowed: () => void;
  /** 保存并关闭 */
  onSave: () => void;
}

const AiSettingsDialog: React.FC<Props> = ({
  open,
  onClose,
  settings,
  onSettingsChange,
  currentProvider,
  onProviderChange,
  localProbe,
  onReprobe,
  effectiveBaseUrl,
  alwaysAllowedList,
  onResetAlwaysAllowed,
  onSave,
}) => {
  // 高级设置默认收起，普通用户不需要碰
  const [advancedOpen, setAdvancedOpen] = useState(false);

  return (
    <Modal isOpen={open} size="lg" onClose={onClose} {...modalBehaviorProps}>
      <ModalContent>
        <ModalShell
          icon={<SettingsIcon />}
          title={t("AI 设置")}
          onClose={onClose}
        >
          <div className="flex flex-col gap-4">
            <div className="flex flex-col gap-2">
              <label className="text-xs font-medium text-gray-500">
                {t("模型供应商")}
              </label>
              <Select
                disallowEmptySelection
                popoverProps={selectPopoverProps}
                selectedKeys={new Set([settings.providerId])}
                onSelectionChange={(keys) => {
                  const id = String(Array.from(keys)[0] ?? "");

                  if (id) onProviderChange(id);
                }}
              >
                {BUILTIN_PROVIDERS.map((p) => (
                  <SelectItem key={p.id}>{p.name}</SelectItem>
                ))}
              </Select>
            </div>

            {usesEditableBaseUrl(currentProvider) && (
              <div className="flex flex-col gap-2">
                <label className="text-xs font-medium text-gray-500">
                  {t("API 地址")}
                </label>
                <Input
                  placeholder={
                    currentProvider.local
                      ? currentProvider.baseUrl
                      : "https://api.example.com/v1"
                  }
                  value={settings.customBaseUrl}
                  onValueChange={(v) =>
                    onSettingsChange((s) => ({ ...s, customBaseUrl: v }))
                  }
                />
                {currentProvider.local && (
                  <span className="text-[11px] text-gray-400">
                    {t("留空使用默认地址；修改过端口或地址时填写")}
                  </span>
                )}
              </div>
            )}

            <div className="flex flex-col gap-2">
              <label className="text-xs font-medium text-gray-500">
                {t("API Key")}
              </label>
              <Input
                placeholder={
                  currentProvider.local
                    ? t("本地引擎通常无需填写")
                    : t("输入 API Key（必填）")
                }
                type="password"
                value={settings.apiKey}
                onValueChange={(v) =>
                  onSettingsChange((s) => ({ ...s, apiKey: v }))
                }
              />
              {!settings.apiKey && !currentProvider.local && (
                <span className="text-[11px] text-warning">
                  {t("⚠️ API Key 未填写，将无法调用 AI")}
                </span>
              )}
              {!settings.apiKey && currentProvider.local && (
                <span className="text-[11px] text-gray-400">
                  {t(
                    "本地引擎默认不校验 Key；若 llama-server 启动时设置了 --api-key，在这里填上即可",
                  )}
                </span>
              )}
            </div>

            <div className="flex flex-col gap-2">
              <label className="text-xs font-medium text-gray-500">
                {t("模型名称")}
              </label>
              <Input
                placeholder={
                  currentProvider.local
                    ? t("从下方检测到的模型中选择，或手动输入")
                    : currentProvider.defaultModel || "model-name"
                }
                value={settings.model}
                onValueChange={(v) =>
                  onSettingsChange((s) => ({ ...s, model: v }))
                }
              />
            </div>

            {/* 本地引擎检测：在线时列出模型一键选用，离线时给启动指引 */}
            {currentProvider.local && (
              <div className="flex flex-col gap-2 rounded-lg border nya-border p-3">
                <div className="flex items-center justify-between">
                  <label className="text-xs font-medium text-gray-500">
                    {t("本地引擎检测")}
                  </label>
                  <Button
                    isDisabled={localProbe.status === "probing"}
                    size="sm"
                    variant="flat"
                    onPress={onReprobe}
                  >
                    {localProbe.status === "probing"
                      ? t("检测中…")
                      : t("重新检测")}
                  </Button>
                </div>

                {localProbe.status === "probing" && (
                  <span className="text-[11px] text-gray-400">
                    {t("正在连接 {url} …", {
                      url:
                        settings.customBaseUrl.trim() ||
                        currentProvider.baseUrl,
                    })}
                  </span>
                )}

                {localProbe.status === "online" &&
                  localProbe.models.length > 0 && (
                    <>
                      <span className="text-[11px] text-success">
                        ●{" "}
                        {t("已连接，发现 {count} 个模型，点击选用：", {
                          count: localProbe.models.length,
                        })}
                      </span>
                      <div className="flex flex-wrap gap-1.5">
                        {localProbe.models.map((m) => (
                          <button
                            key={m}
                            className={`max-w-full truncate rounded-md border px-2 py-1 font-mono text-[11px] transition-colors ${
                              settings.model === m
                                ? "border-primary/40 bg-primary/20 text-primary"
                                : "border-transparent bg-default-200/60 text-gray-300 hover:bg-default-300/60"
                            }`}
                            title={m}
                            type="button"
                            onClick={() =>
                              onSettingsChange((s) => ({ ...s, model: m }))
                            }
                          >
                            {m}
                          </button>
                        ))}
                      </div>
                    </>
                  )}

                {localProbe.status === "online" &&
                  localProbe.models.length === 0 && (
                    <span className="text-[11px] text-warning">
                      ●{" "}
                      {t(
                        "已连接到引擎，但没有发现模型，请先在引擎侧加载 / 拉取模型",
                      )}
                    </span>
                  )}

                {localProbe.status === "offline" && (
                  <span className="text-[11px] text-danger">
                    ●{" "}
                    {t("未检测到本地引擎，请确认它已启动，然后点「重新检测」")}
                  </span>
                )}

                {localProbe.status !== "idle" && (
                  <div className="rounded-lg bg-default-100/50 px-2.5 py-2 text-[11px] leading-relaxed text-gray-400">
                    {t(LOCAL_ENGINE_HINTS[settings.providerId] ?? "")}
                  </div>
                )}
              </div>
            )}

            {/* 温度设置已隐藏，保留默认值 0.7 */}

            <Divider />

            <div className="flex flex-col gap-3">
              <label className="text-xs font-medium text-gray-500">
                {t("实例操作权限")}
              </label>

              {/* 权限一：查看实例文件夹 */}
              <div
                className={`flex items-start gap-3 rounded-lg border p-3 ${
                  settings.allowFolderRead
                    ? "border-success/40 bg-success/5"
                    : "border-default-300 bg-default-100/40"
                }`}
              >
                <Switch
                  isSelected={settings.allowFolderRead}
                  onValueChange={(v) =>
                    onSettingsChange((s) => ({ ...s, allowFolderRead: v }))
                  }
                />
                <div className="flex-1">
                  <div className="text-sm font-medium">
                    {t("查看实例文件夹")}
                  </div>
                  <div className="mt-0.5 text-[11px] text-gray-400">
                    {settings.allowFolderRead
                      ? t(
                          "AI 可以查看实例的模组/存档等列表、浏览与读取实例内文件（含日志和配置），并打开实例文件夹",
                        )
                      : t("AI 无法查看实例文件夹内容，相关查询会被直接拒绝")}
                  </div>
                </div>
              </div>

              {/* 权限二：修改实例文件夹 */}
              <div
                className={`flex items-start gap-3 rounded-lg border p-3 ${
                  settings.allowFolderWrite
                    ? "border-success/40 bg-success/5"
                    : "border-default-300 bg-default-100/40"
                }`}
              >
                <Switch
                  isSelected={settings.allowFolderWrite}
                  onValueChange={(v) =>
                    onSettingsChange((s) => ({ ...s, allowFolderWrite: v }))
                  }
                />
                <div className="flex-1">
                  <div className="text-sm font-medium">
                    {t("修改实例文件夹")}
                  </div>
                  <div className="mt-0.5 text-[11px] text-gray-400">
                    {settings.allowFolderWrite
                      ? t(
                          "允许 AI 安装模组、启用/禁用模组等写入操作，是否逐次确认见下方开关",
                        )
                      : t(
                          "AI 无法对实例文件夹做任何修改，即使处于可修改模式也会被拒绝",
                        )}
                  </div>
                </div>
              </div>

              {/* 权限三：直接修改（确认模式） */}
              <div
                className={`flex items-start gap-3 rounded-lg border p-3 ${
                  settings.allowModify
                    ? "border-success/40 bg-success/5"
                    : "border-warning/40 bg-warning/5"
                }`}
              >
                <Switch
                  isSelected={settings.allowModify}
                  onValueChange={(v) =>
                    onSettingsChange((s) => ({ ...s, allowModify: v }))
                  }
                />
                <div className="flex-1">
                  <div className="text-sm font-medium">
                    {settings.allowModify
                      ? t("允许直接修改")
                      : t("需确认 / 只读")}
                  </div>
                  <div className="mt-0.5 text-[11px] text-gray-400">
                    {settings.allowModify
                      ? t(
                          "AI 可以不经批准直接执行允许范围内的修改操作（含启动/停止游戏）",
                        )
                      : t("修改与启动/停止游戏每次都需要你手动批准后才会执行")}
                  </div>
                </div>
              </div>

              {/* 总是允许授权：可随时撤销，重启后自动清空 */}
              {alwaysAllowedList.length > 0 && (
                <div className="flex items-start justify-between gap-3 rounded-lg border border-success/30 bg-success/5 p-3">
                  <div className="min-w-0 flex-1">
                    <div className="text-sm font-medium">
                      {t("已总是允许的工具")}
                    </div>
                    <div className="mt-0.5 text-[11px] text-gray-400">
                      {t(
                        "本运行期内这些修改类操作不再需要批准，重启启动器后自动重置",
                      )}
                    </div>
                    <div className="mt-1.5 flex flex-wrap gap-1">
                      {alwaysAllowedList.map((name) => (
                        <span
                          key={name}
                          className="rounded bg-default-200/60 px-1.5 py-0.5 font-mono text-[11px]"
                        >
                          {name}
                        </span>
                      ))}
                    </div>
                  </div>
                  <Button
                    className="flex-shrink-0"
                    color="warning"
                    size="sm"
                    variant="flat"
                    onPress={onResetAlwaysAllowed}
                  >
                    {t("撤销全部")}
                  </Button>
                </div>
              )}
            </div>

            {/* 高级设置：默认收起，普通用户不需要碰 */}
            <Divider />
            <button
              className="flex w-full items-center justify-between rounded-lg px-1 py-1 text-xs font-medium text-gray-500 hover:bg-default-100/60"
              type="button"
              onClick={() => setAdvancedOpen((v) => !v)}
            >
              <span>{t("高级设置")}</span>
              {advancedOpen ? (
                <ChevronDownIcon className="h-3.5 w-3.5" />
              ) : (
                <ChevronRightIcon className="h-3.5 w-3.5" />
              )}
            </button>
            {advancedOpen && (
              <>
                <div className="flex flex-col gap-2">
                  <label className="text-xs font-medium text-gray-500">
                    {t("API 格式")}
                  </label>
                  <Select
                    disallowEmptySelection
                    popoverProps={selectPopoverProps}
                    selectedKeys={new Set([settings.apiFormat])}
                    onSelectionChange={(keys) => {
                      const fmt = String(Array.from(keys)[0] ?? "openai");

                      if (fmt === "openai" || fmt === "anthropic") {
                        onSettingsChange((s) => ({ ...s, apiFormat: fmt }));
                      }
                    }}
                  >
                    <SelectItem key="openai" textValue="OpenAI 兼容格式">
                      <div className="flex flex-col">
                        <span className="text-sm">OpenAI 兼容格式</span>
                        <span className="text-[11px] text-gray-400">
                          /chat/completions · 适用于 OpenAI / DeepSeek /
                          通义千问 等
                        </span>
                      </div>
                    </SelectItem>
                    <SelectItem key="anthropic" textValue="Anthropic 格式">
                      <div className="flex flex-col">
                        <span className="text-sm">Anthropic 格式</span>
                        <span className="text-[11px] text-gray-400">
                          /messages · 适用于 Claude 系列模型
                        </span>
                      </div>
                    </SelectItem>
                  </Select>
                </div>

                <div className="flex flex-col gap-2">
                  <label className="text-xs font-medium text-gray-500">
                    {t("上下文窗口大小")}
                  </label>
                  <Select
                    disallowEmptySelection
                    popoverProps={selectPopoverProps}
                    selectedKeys={new Set([String(settings.contextWindow)])}
                    onSelectionChange={(keys) => {
                      const v = Number(Array.from(keys)[0] ?? 262144);

                      onSettingsChange((s) => ({ ...s, contextWindow: v }));
                    }}
                  >
                    <SelectItem key="4096">4K (4096 tokens)</SelectItem>
                    <SelectItem key="8192">8K (8192 tokens)</SelectItem>
                    <SelectItem key="16384">16K (16384 tokens)</SelectItem>
                    <SelectItem key="32768">32K (32768 tokens)</SelectItem>
                    <SelectItem key="65536">64K (65536 tokens)</SelectItem>
                    <SelectItem key="131072">128K (131072 tokens)</SelectItem>
                    <SelectItem key="200000">200K (200000 tokens)</SelectItem>
                    <SelectItem key="262144">256K (262144 tokens)</SelectItem>
                  </Select>
                  <span className="text-[11px] text-gray-400">
                    {t("根据模型支持的上下文窗口选择，过大会增加 API 调用成本")}
                  </span>
                </div>

                <div className="flex items-center justify-between rounded-lg border nya-border p-3">
                  <div>
                    <div className="text-sm font-medium">
                      {t("显示工具调用详情")}
                    </div>
                    <div className="text-[11px] text-gray-400 mt-0.5">
                      {t("在聊天中展示 AI 调用工具的名称、参数和返回结果")}
                    </div>
                  </div>
                  <Switch
                    isSelected={settings.showToolCalls}
                    onValueChange={(v) =>
                      onSettingsChange((s) => ({ ...s, showToolCalls: v }))
                    }
                  />
                </div>

                <div className="rounded-lg bg-default-100/50 p-3">
                  <div className="text-[11px] font-medium text-gray-500">
                    {t("当前配置")}
                  </div>
                  <div className="mt-1 space-y-0.5 text-[11px] text-gray-400">
                    <div>
                      {t("供应商")}: {currentProvider.name}
                    </div>
                    <div>
                      {t("端点")}: {effectiveBaseUrl || t("未设置")}
                    </div>
                    <div>
                      {t("模型")}: {settings.model || t("未设置")}
                    </div>
                  </div>
                </div>
              </>
            )}

            <div className="flex justify-end gap-2 pt-1">
              <Button variant="flat" onPress={onClose}>
                {t("关闭")}
              </Button>
              <Button color="primary" onPress={onSave}>
                {t("保存设置")}
              </Button>
            </div>
          </div>
        </ModalShell>
      </ModalContent>
    </Modal>
  );
};

export default AiSettingsDialog;
