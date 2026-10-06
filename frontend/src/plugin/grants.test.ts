/*
 * 插件权限"用户授权"层（插件页 Switch）的契约。
 *
 * 它决定"声明了权限的插件到底能不能用这一项"，坏了会两种翻车：默认值写错 →
 * 插件一装上就静默失灵；落盘格式写错 → 用户关掉的权限重启后又开了。
 */
import { beforeEach, describe, expect, it, vi } from "vitest";

import { GetValue, SetValue } from "../../wailsjs/go/bindings/ConfigAPI";

import {
  clearPluginGrants,
  DEFAULT_OFF_PERMISSIONS,
  defaultPermissionGrant,
  hydratePluginGrants,
  isPermissionGranted,
  NETWORK_PERMISSION,
  PLUGIN_GRANTS_CONFIG_KEY,
  setPluginPermission,
} from "./grants";

vi.mock("../../wailsjs/go/bindings/ConfigAPI", () => ({
  GetValue: vi.fn(async () => ""),
  SetValue: vi.fn(async () => undefined),
}));

beforeEach(async () => {
  vi.mocked(GetValue).mockReset();
  // 空对象 = "没有任何显式设置"，用它把模块级授权表清干净（空串会走"无配置"提前返回）
  vi.mocked(GetValue).mockResolvedValue("{}");
  vi.mocked(SetValue).mockClear();
  await hydratePluginGrants();
});

describe("默认值", () => {
  it("联网与三项隐私读默认关闭，其余默认开启", () => {
    for (const permission of [
      NETWORK_PERMISSION,
      "logs",
      "accounts",
      "music",
    ]) {
      expect(defaultPermissionGrant(permission), permission).toBe(false);
      expect(DEFAULT_OFF_PERMISSIONS).toContain(permission);
    }
    for (const permission of [
      "downloads",
      "downloads-write",
      "instances",
      "launch",
      "storage",
      "notifications",
    ]) {
      expect(defaultPermissionGrant(permission), permission).toBe(true);
    }
  });
});

describe("读写开关", () => {
  it("默认值生效；显式关掉后立即失效，重新打开立即恢复", async () => {
    expect(isPermissionGranted("p1", "downloads")).toBe(true);
    expect(isPermissionGranted("p1", NETWORK_PERMISSION)).toBe(false);

    await setPluginPermission("p1", "downloads", false);
    expect(isPermissionGranted("p1", "downloads")).toBe(false);

    await setPluginPermission("p1", NETWORK_PERMISSION, true);
    expect(isPermissionGranted("p1", NETWORK_PERMISSION)).toBe(true);
  });

  it("落盘到 pluginPermissions 键（键名前缀不带插件隔离，授权表按插件分组）", async () => {
    await setPluginPermission("p2", "music", true);

    expect(SetValue).toHaveBeenCalledTimes(1);
    const [key, raw] = vi.mocked(SetValue).mock.calls[0] as [string, string];

    expect(key).toBe(PLUGIN_GRANTS_CONFIG_KEY);
    expect(JSON.parse(raw)).toEqual({ p2: { music: true } });
  });

  it("清掉某插件时只动它自己的条目", async () => {
    await setPluginPermission("p3", "logs", true);
    await setPluginPermission("p4", "logs", true);

    await clearPluginGrants("p3");

    expect(isPermissionGranted("p3", "logs")).toBe(false);
    expect(isPermissionGranted("p4", "logs")).toBe(true);
    const [, raw] = vi.mocked(SetValue).mock.calls.at(-1) as [string, string];

    expect(JSON.parse(raw)).toEqual({ p4: { logs: true } });
  });

  it("落盘失败不回滚内存态（本次运行内开关仍然生效）", async () => {
    const onError = vi.spyOn(console, "error").mockImplementation(() => {});

    vi.mocked(SetValue).mockRejectedValueOnce(new Error("磁盘满"));
    await setPluginPermission("p5", "launch", false);

    expect(isPermissionGranted("p5", "launch")).toBe(false);
    expect(onError).toHaveBeenCalled();
    onError.mockRestore();
  });
});

describe("启动水合", () => {
  it("读到合法配置就按它生效", async () => {
    vi.mocked(GetValue).mockResolvedValue(
      JSON.stringify({ p6: { music: true, downloads: false } }),
    );

    await hydratePluginGrants();

    expect(isPermissionGranted("p6", "music")).toBe(true);
    expect(isPermissionGranted("p6", "downloads")).toBe(false);
    expect(isPermissionGranted("p6", NETWORK_PERMISSION)).toBe(false);
  });

  it("配置损坏 / 形状不对：退回默认值并记警告，不抛错", async () => {
    const warn = vi.spyOn(console, "warn").mockImplementation(() => {});

    vi.mocked(GetValue).mockResolvedValue("{ not json");
    await hydratePluginGrants();
    expect(isPermissionGranted("p7", "downloads")).toBe(true);

    vi.mocked(GetValue).mockResolvedValue("[1,2,3]");
    await hydratePluginGrants();
    expect(warn).toHaveBeenCalled();
    expect(isPermissionGranted("p7", "downloads")).toBe(true);

    // 非布尔值的条目直接忽略（不把它当成 true）
    vi.mocked(GetValue).mockResolvedValue(
      JSON.stringify({ p7: { downloads: "yes", music: true } }),
    );
    await hydratePluginGrants();
    expect(isPermissionGranted("p7", "downloads")).toBe(true);
    expect(isPermissionGranted("p7", "music")).toBe(true);
    warn.mockRestore();
  });

  it("读取失败按默认值处理（默认值本身是安全的）", async () => {
    const onError = vi.spyOn(console, "error").mockImplementation(() => {});

    vi.mocked(GetValue).mockRejectedValue(new Error("配置不可用"));
    await hydratePluginGrants();

    expect(isPermissionGranted("p8", "downloads")).toBe(true);
    expect(isPermissionGranted("p8", NETWORK_PERMISSION)).toBe(false);
    expect(onError).toHaveBeenCalled();
    onError.mockRestore();
  });
});
