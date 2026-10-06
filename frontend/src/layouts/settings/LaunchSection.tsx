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
import React, { useEffect, useState } from "react";
import {
  Button,
  Input,
  Select,
  SelectItem,
  Switch,
  Textarea,
} from "@heroui/react";

import { selectPopoverProps } from "../../lib/motion";
import {
  LoadGlobalLaunchSettings,
  SaveGlobalLaunchSettings,
  GetDefaultVersionIsolation,
  SaveDefaultVersionIsolation,
  GetVerifyFilesBeforeLaunch,
  SaveVerifyFilesBeforeLaunch,
} from "../../../wailsjs/go/bindings/ConfigAPI";
import { asObject } from "../../lib/guards";
import { t } from "../../i18n";

import Section, { SettingRow } from "./Section";
import AdvancedGroup from "./AdvancedGroup";

interface LaunchSettings {
  WindowWidth: number;
  WindowHeight: number;
  JavaExecutable: string;
  AdditionalJvmArguments: string[];
  AdditionalGameArguments: string[];
  ProcessPriority?: string;
  WrapperCommand?: string;
  AdditionalEnvironmentVariables?: string[];
  LaunchFullscreen?: boolean;
}

const PRIORITY_KEYS = ["normal", "low", "belownormal", "abovenormal", "high"];

function priorityLabel(key: string): string {
  switch (key) {
    case "low":
      return t("低");
    case "belownormal":
      return t("低于普通");
    case "abovenormal":
      return t("高于普通");
    case "high":
      return t("高");
    default:
      return t("普通");
  }
}

const LaunchSection: React.FC = () => {
  const [s, setS] = useState<LaunchSettings>({
    WindowWidth: 854,
    WindowHeight: 480,
    JavaExecutable: "",
    AdditionalJvmArguments: [],
    AdditionalGameArguments: [],
    ProcessPriority: "normal",
    WrapperCommand: "",
    AdditionalEnvironmentVariables: [],
    LaunchFullscreen: false,
  });
  const [jvmArgs, setJvmArgs] = useState("");
  const [gameArgs, setGameArgs] = useState("");
  const [envVars, setEnvVars] = useState("");
  const [isolation, setIsolation] = useState<boolean>(true);
  const [verify, setVerify] = useState<boolean>(true);
  const [saveHint, setSaveHint] = useState("");

  useEffect(() => {
    (async () => {
      try {
        const [loaded, iso, ver] = await Promise.all([
          LoadGlobalLaunchSettings(),
          GetDefaultVersionIsolation(),
          GetVerifyFilesBeforeLaunch(),
        ]);

        // 后端读取失败时返回 null，直接写入 state 会让渲染在字段读取上崩掉
        const settings = asObject<LaunchSettings>(loaded);

        if (settings) {
          setS({
            ProcessPriority: "normal",
            WrapperCommand: "",
            AdditionalEnvironmentVariables: [],
            LaunchFullscreen: false,
            ...settings,
          });
          setJvmArgs((settings.AdditionalJvmArguments ?? []).join("\n"));
          setGameArgs((settings.AdditionalGameArguments ?? []).join("\n"));
          setEnvVars(
            (settings.AdditionalEnvironmentVariables ?? []).join("\n"),
          );
        }
        setIsolation(Boolean(iso));
        setVerify(Boolean(ver));
      } catch (ex) {
        console.error(t("读取启动设置失败"), ex);
      }
    })();
  }, []);

  const save = async () => {
    // 与旧版（Vue LauncherSettingsView.saveJvmArgs）一致的合并写法：
    // 先读当前完整设置，只覆盖本页编辑的字段，其余（如 JavaExecutable 的
    // "$auto" 自动选择占位）原样保留 —— 整份覆盖会把 $auto 冲成具体路径。
    const width = Math.max(320, Math.round(s.WindowWidth) || 854);
    const height = Math.max(240, Math.round(s.WindowHeight) || 480);
    const current = await LoadGlobalLaunchSettings();
    const settings = {
      ...current,
      WindowWidth: width,
      WindowHeight: height,
      AdditionalJvmArguments: jvmArgs
        .split("\n")
        .map((x) => x.trim())
        .filter(Boolean),
      AdditionalGameArguments: gameArgs
        .split("\n")
        .map((x) => x.trim())
        .filter(Boolean),
      ProcessPriority: s.ProcessPriority || "normal",
      WrapperCommand: s.WrapperCommand || "",
      AdditionalEnvironmentVariables: envVars
        .split("\n")
        .map((x) => x.trim())
        .filter(Boolean),
      LaunchFullscreen: !!s.LaunchFullscreen,
    };
    const ok = await SaveGlobalLaunchSettings(settings);

    if (ok) {
      setS({ ...s, WindowWidth: width, WindowHeight: height });
    }
    setSaveHint(ok ? t("已保存。") : t("保存失败：参数不合法。"));
  };

  return (
    <Section
      aliases={[
        t("启动"),
        t("窗口"),
        t("宽度"),
        t("高度"),
        "jvm",
        t("参数"),
        t("隔离"),
        t("校验"),
        t("验证"),
        t("优先级"),
        t("环境变量"),
        t("全屏"),
        t("包装"),
        t("进程"),
      ]}
      title={t("游戏启动")}
    >
      <SettingRow label={t("启动前校验文件")}>
        <Switch
          color="primary"
          isSelected={verify}
          onValueChange={async (v) => {
            setVerify(v);
            await SaveVerifyFilesBeforeLaunch(v);
          }}
        />
      </SettingRow>

      <SettingRow label={t("默认版本隔离")}>
        <Switch
          color="primary"
          isSelected={isolation}
          onValueChange={async (v) => {
            setIsolation(v);
            await SaveDefaultVersionIsolation(v);
          }}
        />
      </SettingRow>

      <SettingRow label={t("窗口宽度")}>
        <Input
          className="w-28 min-w-0 max-w-full [&_*]:min-w-0"
          size="sm"
          type="number"
          value={String(s.WindowWidth)}
          onChange={(e) => setS({ ...s, WindowWidth: Number(e.target.value) })}
        />
      </SettingRow>

      <SettingRow label={t("窗口高度")}>
        <Input
          className="w-28 min-w-0 max-w-full [&_*]:min-w-0"
          size="sm"
          type="number"
          value={String(s.WindowHeight)}
          onChange={(e) => setS({ ...s, WindowHeight: Number(e.target.value) })}
        />
      </SettingRow>

      <SettingRow label={t("全屏启动")}>
        <Switch
          color="primary"
          isSelected={!!s.LaunchFullscreen}
          onValueChange={(v) => setS({ ...s, LaunchFullscreen: v })}
        />
      </SettingRow>

      {/* Java 启动参数与进程调优属于高深内容：折叠收纳，保存按钮留在组外 */}
      <AdvancedGroup
        hint={t("JVM / 游戏参数、进程优先级、环境变量")}
        id="launch"
      >
        <div className="pt-1">
          <div className="text-sm text-gray-800 dark:text-gray-200 mb-1">
            {t("附加 JVM 参数")}
          </div>
          <Textarea
            minRows={3}
            placeholder={t("每行一个，如 -XX:+UseG1GC")}
            value={jvmArgs}
            onValueChange={setJvmArgs}
          />
        </div>

        <div className="pt-2">
          <div className="text-sm text-gray-800 dark:text-gray-200 mb-1">
            {t("附加游戏参数")}
          </div>
          <Textarea
            minRows={3}
            placeholder={t("每行一个，如 --fullscreen")}
            value={gameArgs}
            onValueChange={setGameArgs}
          />
        </div>

        <SettingRow label={t("进程优先级")}>
          <Select
            className="w-40 min-w-0 max-w-full [&_*]:min-w-0"
            popoverProps={selectPopoverProps}
            selectedKeys={[s.ProcessPriority || "normal"]}
            size="sm"
            onSelectionChange={(keys) =>
              setS({
                ...s,
                ProcessPriority: (Array.from(keys)[0] as string) || "normal",
              })
            }
          >
            {PRIORITY_KEYS.map((key) => (
              <SelectItem key={key}>{priorityLabel(key)}</SelectItem>
            ))}
          </Select>
        </SettingRow>

        <SettingRow label={t("包装命令")}>
          <Input
            className="w-72 min-w-0 max-w-full [&_*]:min-w-0"
            placeholder="gamemoderun %command%"
            radius="lg"
            size="sm"
            value={s.WrapperCommand ?? ""}
            onValueChange={(v) => setS({ ...s, WrapperCommand: v })}
          />
        </SettingRow>

        <div className="pt-2">
          <div className="text-sm text-gray-800 dark:text-gray-200 mb-1">
            {t("附加环境变量（每行一个，格式 KEY=VALUE）")}
          </div>
          <Textarea
            minRows={2}
            placeholder={"__GL_SHADER_DISK_CACHE_SKIP_CLEANUP=1"}
            value={envVars}
            onValueChange={setEnvVars}
          />
        </div>
      </AdvancedGroup>

      <div className="pt-2 flex items-center gap-3">
        <Button color="primary" size="sm" onPress={save}>
          {t("保存")}
        </Button>
        {saveHint ? (
          <span className="text-xs text-gray-400">{saveHint}</span>
        ) : null}
      </div>
    </Section>
  );
};

export default LaunchSection;
