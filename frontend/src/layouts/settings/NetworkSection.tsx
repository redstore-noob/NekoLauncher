/*
 * 设置 - 网络分区：代理设置（跟随系统 / 直连 / 自定义）。
 * 自定义模式支持 http:// 与 socks5:// 代理，可选账号密码；
 * 保存后立即应用到全局出站请求（下载、正版登录、更新检查等）。
 */
import type { network } from "../../../wailsjs/go/models";

import React, { useEffect, useState } from "react";
import { Button, Input, Select, SelectItem } from "@heroui/react";

import { popoverMotionProps } from "../../lib/motion";
import {
  GetProxySettings,
  SaveProxySettings,
  TestProxy,
} from "../../../wailsjs/go/bindings/ConfigAPI";
import { useI18n } from "../../i18n";

import Section, { SettingRow } from "./Section";

const PROXY_MODE_KEYS = ["system", "off", "custom"] as const;

function proxyModeLabel(t: (s: string) => string, key: string): string {
  switch (key) {
    case "off":
      return t("直连");
    case "custom":
      return t("自定义");
    default:
      return t("跟随系统");
  }
}

const NetworkSection: React.FC = () => {
  const { t } = useI18n();
  const [mode, setMode] = useState("system");
  const [address, setAddress] = useState("");
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [hint, setHint] = useState("");
  const [hintError, setHintError] = useState(false);
  const [testing, setTesting] = useState(false);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    GetProxySettings()
      .then((settings) => {
        setMode(settings?.Mode || "system");
        setAddress(settings?.Address || "");
        setUsername(settings?.Username || "");
        setPassword(settings?.Password || "");
      })
      .catch(() => {
        /* 保持默认值 */
      });
  }, []);

  const showHint = (text: string, error = false) => {
    setHint(text);
    setHintError(error);
    if (text) setTimeout(() => setHint(""), 4000);
  };

  const buildSettings = (): network.ProxySettings => ({
    Mode: mode,
    Address: address.trim(),
    Username: username.trim(),
    Password: password,
  });

  const save = async () => {
    if (busy) return;
    setBusy(true);
    try {
      await SaveProxySettings(buildSettings());
      showHint(t("已保存，代理设置已生效"));
    } catch (ex) {
      showHint((ex as Error)?.message ?? String(ex), true);
    }
    setBusy(false);
  };

  const test = async () => {
    if (testing) return;
    setTesting(true);
    try {
      const message = await TestProxy(buildSettings());

      showHint(message);
    } catch (ex) {
      showHint((ex as Error)?.message ?? String(ex), true);
    }
    setTesting(false);
  };

  return (
    <Section
      aliases={[t("网络"), t("代理"), t("连接"), "proxy", "network"]}
      title={t("网络")}
    >
      <SettingRow label={t("代理模式")}>
        <Select
          className="w-40 min-w-0 max-w-full [&_*]:min-w-0"
          popoverProps={{ motionProps: popoverMotionProps }}
          selectedKeys={[mode]}
          size="sm"
          onSelectionChange={(keys) =>
            setMode((Array.from(keys)[0] as string) || "system")
          }
        >
          {PROXY_MODE_KEYS.map((key) => (
            <SelectItem key={key}>{proxyModeLabel(t, key)}</SelectItem>
          ))}
        </Select>
      </SettingRow>

      {mode === "custom" ? (
        <>
          <SettingRow
            hint={t("可加 http:// 或 socks5:// 前缀，缺省按 http 处理")}
            label={t("代理地址")}
          >
            <Input
              className="w-56 min-w-0 max-w-full [&_*]:min-w-0"
              placeholder="127.0.0.1:7890"
              radius="lg"
              size="sm"
              value={address}
              onValueChange={setAddress}
            />
          </SettingRow>
          <SettingRow label={t("代理账号（可选）")}>
            <Input
              className="w-56 min-w-0 max-w-full [&_*]:min-w-0"
              radius="lg"
              size="sm"
              value={username}
              onValueChange={setUsername}
            />
          </SettingRow>
          <SettingRow label={t("代理密码（可选）")}>
            <Input
              className="w-56 min-w-0 max-w-full [&_*]:min-w-0"
              radius="lg"
              size="sm"
              type="password"
              value={password}
              onValueChange={setPassword}
            />
          </SettingRow>
        </>
      ) : null}

      <div className="flex flex-wrap items-center gap-2 pt-1">
        <Button
          color="primary"
          isLoading={busy}
          radius="full"
          size="sm"
          onPress={save}
        >
          {t("保存网络设置")}
        </Button>
        <Button
          isDisabled={busy}
          isLoading={testing}
          radius="full"
          size="sm"
          variant="flat"
          onPress={test}
        >
          {t("测试连接")}
        </Button>
        {hint ? (
          <span
            className={`text-xs ${hintError ? "text-danger" : "text-primary"}`}
          >
            {hint}
          </span>
        ) : null}
      </div>
    </Section>
  );
};

export default NetworkSection;
