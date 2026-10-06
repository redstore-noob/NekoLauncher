/*
 * 插件 API 面的权限门、高危动作确认闸门与绑定接线的契约。
 *
 * 逐条验证：
 *   1. 未声明权限时**抛错**（错误信息里带缺哪个权限），绝不静默放行；
 *   2. 声明后能真正落到宿主绑定（绑定被换成桩，断言被调过）；
 *   3. 高危动作先过 NekoPrompt：拒绝时**绑定一次都不能被调**（闸门在动手之前），
 *      "本次运行内允许"只问一次，"允许一次"下次再问；
 *   4. 撤销得掉的动作只公示不打扰；危险读（logs）已收进权限。
 */
import type { PluginApi, PluginManifest } from "./types";

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import {
  DownloadResourceVersion,
  GetContentTasks,
  GetVersions,
  StartDownload,
} from "../../wailsjs/go/bindings/DownloadAPI";
import { ToggleContentEntry } from "../../wailsjs/go/bindings/ContentAPI";
import { SaveGlobalLaunchSettings } from "../../wailsjs/go/bindings/ConfigAPI";
import { GetMemorySnapshot } from "../../wailsjs/go/bindings/MonitorAPI";
import { GetMusicTracks } from "../../wailsjs/go/bindings/MusicAPI";
import {
  Launch,
  LaunchVersion,
  GetLogText,
} from "../../wailsjs/go/bindings/LauncherAPI";

import { createPluginApi } from "./api";
import { NETWORK_PERMISSION, setPluginPermission } from "./grants";

/** 下载类动作都要联网开关：测试里统一用它打开 */
const grantNetwork = (pluginId: string) =>
  setPluginPermission(pluginId, NETWORK_PERMISSION, true);

// 确认框与公示都走宿主浮层门面：换成桩，断言"弹了什么、绑定的按钮是什么"
const dialogSpy = vi.hoisted(() => ({
  showDialogBox: vi.fn(async () => "session"),
  info: vi.fn(),
  success: vi.fn(),
  warning: vi.fn(),
  error: vi.fn(),
  alert: vi.fn(),
  confirm: vi.fn(async () => true),
  promptDialog: vi.fn(async () => null),
  hideAlertNow: vi.fn(),
}));

vi.mock("../components/overlay/dialog", () => ({
  notify: {
    info: dialogSpy.info,
    success: dialogSpy.success,
    warning: dialogSpy.warning,
    error: dialogSpy.error,
  },
  alert: dialogSpy.alert,
  confirm: dialogSpy.confirm,
  promptDialog: dialogSpy.promptDialog,
  showDialogBox: dialogSpy.showDialogBox,
  hideAlertNow: dialogSpy.hideAlertNow,
}));

vi.mock("../../wailsjs/runtime/runtime", () => ({
  EventsOn: vi.fn(() => () => {}),
}));

vi.mock("../../wailsjs/go/bindings/DownloadAPI", () => ({
  GetContentTasks: vi.fn(async () => []),
  GetCurrentDownloadSnapshot: vi.fn(async () => ({ Phase: 0, VersionID: "" })),
  GetVersions: vi.fn(async () => []),
  GetModLoaderVersions: vi.fn(async () => []),
  GetAllDownloadSources: vi.fn(async () => []),
  GetInstalledJavaRuntimes: vi.fn(async () => []),
  StartDownload: vi.fn(async () => true),
  StartModLoaderDownload: vi.fn(async () => true),
  DownloadResourceVersion: vi.fn(async () => ({ SavedPath: "/tmp/x.jar" })),
  InstallModpackToInstance: vi.fn(async () => ({ InstalledFiles: 1 })),
}));

vi.mock("../../wailsjs/go/bindings/ContentAPI", () => ({
  ReadSaves: vi.fn(async () => []),
  ToggleContentEntry: vi.fn(async () => undefined),
}));

vi.mock("../../wailsjs/go/bindings/ConfigAPI", () => ({
  ClearValue: vi.fn(async () => undefined),
  GetValue: vi.fn(async () => ""),
  GetVersionProfile: vi.fn(async () => ({})),
  LoadGlobalLaunchSettings: vi.fn(async () => ({})),
  SaveGlobalLaunchSettings: vi.fn(async () => true),
  SaveVersionProfile: vi.fn(async () => true),
  SetValue: vi.fn(async () => undefined),
}));

vi.mock("../../wailsjs/go/bindings/MonitorAPI", () => ({
  GetMemorySnapshot: vi.fn(async () => ({ TotalMb: 1 })),
  GetSystemUsage: vi.fn(async () => ({ CpuPercent: 1 })),
  GetDiskUsage: vi.fn(async () => ({ TotalGb: 1 })),
}));

vi.mock("../../wailsjs/go/bindings/MusicAPI", () => ({
  GetCurrentTrack: vi.fn(async () => ({ Title: "t" })),
  GetMusicTracks: vi.fn(async () => []),
}));

vi.mock("../../wailsjs/go/bindings/LauncherAPI", () => ({
  GetLogText: vi.fn(async () => ""),
  GetLaunchSnapshot: vi.fn(async () => ({ Phase: 0 })),
  Launch: vi.fn(async () => ({ Success: true })),
  LaunchVersion: vi.fn(async () => ({ Success: true })),
  DiagnoseCrash: vi.fn(async () => null),
}));

const manifest = (
  capabilities: Record<string, boolean>,
  id = "contract",
): PluginManifest => ({
  id,
  name: "契约插件",
  version: "1.0.0",
  apiVersion: "1",
  capabilities,
});

interface GateCase {
  member: string;
  permission: string;
  /** 声明权限后的调用（应落到绑定） */
  call: (api: PluginApi) => Promise<unknown> | unknown;
  /** 断言绑定确实被调过 */
  assert: () => void;
}

const CASES: GateCase[] = [
  {
    member: "getDownloadTasks",
    permission: "downloads",
    call: (api) => api.getDownloadTasks(),
    assert: () => expect(GetContentTasks).toHaveBeenCalled(),
  },
  {
    member: "getVersions",
    permission: "downloads",
    call: (api) => api.getVersions(),
    assert: () => expect(GetVersions).toHaveBeenCalled(),
  },
  {
    member: "startDownload",
    permission: "downloads-write",
    call: (api) => api.startDownload({ id: "1.21.1" } as never),
    assert: () => expect(StartDownload).toHaveBeenCalled(),
  },
  {
    member: "downloadResource",
    permission: "downloads-write",
    call: (api) => api.downloadResource({ projectId: "p" } as never),
    assert: () => expect(DownloadResourceVersion).toHaveBeenCalled(),
  },
  {
    member: "setContentEnabled",
    permission: "instances-write",
    call: (api) => api.setContentEnabled("C:/mods/a.jar", true),
    assert: () =>
      // 后端第二参是"禁用"，插件侧传 enabled=true 必须翻成 false
      expect(ToggleContentEntry).toHaveBeenCalledWith("C:/mods/a.jar", false),
  },
  {
    member: "getMemorySnapshot",
    permission: "system-status",
    call: (api) => api.getMemorySnapshot(),
    assert: () => expect(GetMemorySnapshot).toHaveBeenCalled(),
  },
  {
    member: "getMusicTracks",
    permission: "music",
    call: (api) => api.getMusicTracks(),
    assert: () => expect(GetMusicTracks).toHaveBeenCalled(),
  },
  {
    member: "onLogLine",
    permission: "logs",
    call: (api) => {
      // 订阅完立刻退订：不让轮询定时器漏到用例之外
      api.onLogLine(() => {})?.();

      return Promise.resolve();
    },
    assert: () => expect(GetLogText).toHaveBeenCalled(),
  },
];

describe("插件 API 权限门", () => {
  it.each(CASES)(
    "$member 未声明 $permission 时抛错",
    async ({ permission, call }) => {
      const api = createPluginApi(manifest({}));

      await expect(async () => call(api)).rejects.toThrow(permission);
    },
  );

  it.each(CASES)(
    "$member 声明 $permission 后落到宿主绑定",
    async ({ permission, call, assert }) => {
      const api = createPluginApi(manifest({ [permission]: true }));

      // 用户授权层：声明了还要开关打开才放行（network 默认关闭，清单类调用必须它也开）
      await setPluginPermission("contract", permission, true);
      await setPluginPermission("contract", NETWORK_PERMISSION, true);

      await call(api);
      assert();
    },
  );

  it("读权限不构成写权限：只声明 downloads 时写入动作仍被拒", async () => {
    const api = createPluginApi(manifest({ downloads: true }));

    // downloads 是新增权限、没有存量插件，因此下载域**不做**读→写兼容
    await expect(async () =>
      api.startDownload({ id: "1.21.1" } as never),
    ).rejects.toThrow("downloads-write");
    await expect(async () =>
      api.downloadResource({ projectId: "p" } as never),
    ).rejects.toThrow("downloads-write");
  });

  it("免权限成员不需要任何声明", async () => {
    const api = createPluginApi(manifest({}));

    expect(() => api.navigateToPage("不存在的页面")).toThrow(/页面不存在/);
    expect(typeof api.onGameExit).toBe("function");
    expect(typeof api.registerPageAction).toBe("function");
  });
});

describe("高危动作的确认闸门（端到端）", () => {
  beforeEach(() => {
    dialogSpy.showDialogBox.mockReset();
    dialogSpy.showDialogBox.mockResolvedValue("session");
    dialogSpy.warning.mockClear();
    dialogSpy.info.mockClear();
  });

  it("用户拒绝：动作失败，且绑定一次都没被调（闸门在动手之前）", async () => {
    dialogSpy.showDialogBox.mockResolvedValue("deny");
    const api = createPluginApi(
      manifest({ "downloads-write": true, launch: true }, "gate-deny"),
    );

    vi.mocked(StartDownload).mockClear();
    vi.mocked(LaunchVersion).mockClear();
    await grantNetwork("gate-deny");

    await expect(api.startDownload({ id: "1.21.1" } as never)).rejects.toThrow(
      "被用户拒绝",
    );
    expect(StartDownload).not.toHaveBeenCalled();

    await expect(api.launchVersion("fabric-1.21")).rejects.toThrow(
      "被用户拒绝",
    );
    expect(LaunchVersion).not.toHaveBeenCalled();
  });

  it("改写启动器设置（等同决定下次执行什么）也要先确认", async () => {
    dialogSpy.showDialogBox.mockResolvedValue("deny");
    const api = createPluginApi(
      manifest({ "launcher-config-write": true }, "gate-settings"),
    );

    vi.mocked(SaveGlobalLaunchSettings).mockClear();

    await expect(
      api.saveLauncherSettings({
        WindowWidth: 900,
        WindowHeight: 600,
      } as never),
    ).rejects.toThrow("被用户拒绝");
    expect(SaveGlobalLaunchSettings).not.toHaveBeenCalled();
  });

  it("「本次运行内允许」只问一次，「允许一次」下次再问", async () => {
    const sessionApi = createPluginApi(
      manifest({ "downloads-write": true }, "gate-session"),
    );

    await grantNetwork("gate-session");
    await sessionApi.startDownload({ id: "1.21.1" } as never);
    await sessionApi.startDownload({ id: "1.20.1" } as never);
    expect(dialogSpy.showDialogBox).toHaveBeenCalledTimes(1);

    dialogSpy.showDialogBox.mockClear();
    dialogSpy.showDialogBox.mockResolvedValue("once");
    const onceApi = createPluginApi(manifest({ launch: true }, "gate-once"));

    await onceApi.launchSelected();
    await onceApi.launchSelected();
    expect(dialogSpy.showDialogBox).toHaveBeenCalledTimes(2);
    expect(Launch).toHaveBeenCalledTimes(2);
  });

  it("被拒绝后同一动作不再弹框（插件循环重试也刷不了屏）", async () => {
    dialogSpy.showDialogBox.mockResolvedValue("deny");
    const api = createPluginApi(
      manifest({ "downloads-write": true }, "gate-loop"),
    );

    vi.mocked(DownloadResourceVersion).mockClear();
    await grantNetwork("gate-loop");

    await expect(
      api.downloadResource({ projectId: "p" } as never),
    ).rejects.toThrow("被用户拒绝");
    await expect(
      api.downloadResource({ projectId: "p" } as never),
    ).rejects.toThrow("被用户拒绝");

    expect(dialogSpy.showDialogBox).toHaveBeenCalledTimes(1);
    expect(DownloadResourceVersion).not.toHaveBeenCalled();
  });

  it("撤销得掉的动作不打扰用户，只公示一条 NekoAlert", async () => {
    // 公示器单例的全局突发预算会被同文件的其它用例吃掉：把时钟往前拨 10 分钟，
    // 让本用例从干净窗口开始（同一用例内时钟冻结，去重照常生效）。
    vi.useFakeTimers();
    vi.setSystemTime(new Date(Date.now() + 10 * 60_000));
    try {
      const api = createPluginApi(
        manifest({ "instances-write": true }, "gate-announce"),
      );

      await api.setContentEnabled("C:/mods/sodium.jar", false);

      expect(dialogSpy.showDialogBox).not.toHaveBeenCalled();
      expect(dialogSpy.warning).toHaveBeenCalledWith(
        "插件「契约插件」禁用了（sodium.jar）",
        5000,
      );
    } finally {
      vi.useRealTimers();
    }
  });
});

describe("危险读的权限", () => {
  it("onLogLine 从免费改为需要 logs（日志里有路径 / 账号名 / 服务器地址）", async () => {
    const denied = createPluginApi(manifest({}));

    expect(() => denied.onLogLine(() => {})).toThrow("logs");

    const granted = createPluginApi(manifest({ logs: true }, "logs-on"));

    await setPluginPermission("logs-on", "logs", true);
    const cancel = granted.onLogLine(() => {});

    expect(typeof cancel).toBe("function");
    (cancel as () => void)();
  });
});

describe("用户开关（插件页每个权限一个 Switch）", () => {
  it("关掉开关：返回 null 且宿主绑定一次都不被调用", async () => {
    const api = createPluginApi(manifest({ downloads: true }, "switch-off"));

    await setPluginPermission("switch-off", "downloads", false);
    await setPluginPermission("switch-off", NETWORK_PERMISSION, true);
    vi.mocked(GetVersions).mockClear();

    await expect(api.getVersions()).resolves.toBeNull();
    expect(GetVersions).not.toHaveBeenCalled();

    // 重新打开即恢复
    await setPluginPermission("switch-off", "downloads", true);
    await expect(api.getVersions()).resolves.toEqual([]);
    expect(GetVersions).toHaveBeenCalled();
  });

  it("联网默认关闭：声明了 downloads 也拿不到清单，打开联网后恢复", async () => {
    const api = createPluginApi(
      manifest({ downloads: true, "server-status": true }, "switch-net"),
    );

    await expect(api.getVersions()).resolves.toBeNull();
    await expect(api.getServerStatus("localhost", 25565)).resolves.toBeNull();

    await setPluginPermission("switch-net", NETWORK_PERMISSION, true);
    await expect(api.getVersions()).resolves.toEqual([]);
  });

  it("默认关闭的隐私读：logs / accounts / music", async () => {
    const api = createPluginApi(
      manifest({ logs: true, accounts: true, music: true }, "switch-privacy"),
    );

    expect(api.onLogLine(() => {})).toBeNull();
    await expect(api.getMusicTracks()).resolves.toBeNull();
    await expect(api.getCurrentTrack()).resolves.toBeNull();
    await expect(api.getAccounts()).resolves.toBeNull();

    await setPluginPermission("switch-privacy", "music", true);
    await expect(api.getMusicTracks()).resolves.toEqual([]);
  });

  it("未被用户关闭的权限照常工作（默认开启）", async () => {
    const api = createPluginApi(
      manifest({ "system-status": true }, "switch-default-on"),
    );

    await expect(api.getMemorySnapshot()).resolves.toEqual({ TotalMb: 1 });
  });
});

describe("公示节流（宿主侧，插件关不掉）", () => {
  // 公示器是应用内单例，带着跨用例的去重 / 突发预算。每启用例把假时钟相对真实
  // 时间再往前推 5 分钟（累计），旧记录便统统过期；同一用例内时钟冻结，去重照常生效。
  let clockOffset = 0;

  beforeEach(() => {
    const realNow = Date.now();

    vi.useFakeTimers();
    clockOffset += 5 * 60_000;
    vi.setSystemTime(new Date(realNow + clockOffset));
    dialogSpy.showDialogBox.mockReset();
    dialogSpy.showDialogBox.mockResolvedValue("session");
    dialogSpy.warning.mockClear();
    dialogSpy.info.mockClear();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("读操作不公示（避免把警示条淹掉）", async () => {
    const api = createPluginApi(
      manifest({ downloads: true, "system-status": true, music: true }),
    );

    await api.getDownloadTasks();
    await api.getVersions();
    await api.getMemorySnapshot();
    await api.getMusicTracks();

    expect(dialogSpy.warning).not.toHaveBeenCalled();
    expect(dialogSpy.info).not.toHaveBeenCalled();
  });

  it("同一插件的同一动作在去重窗口内只公示一次（循环写操作不刷屏）", async () => {
    const api = createPluginApi(
      manifest({ "downloads-write": true }, "burst-loop"),
    );

    await grantNetwork("burst-loop");
    for (let index = 0; index < 5; index++) {
      await api.downloadResource({ projectId: `p${index}` } as never);
    }

    expect(dialogSpy.warning).toHaveBeenCalledTimes(1);
  });
});
