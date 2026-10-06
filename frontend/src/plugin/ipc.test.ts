/*
 * 插件间消息总线的契约：校验（类型名 / JSON 负载 / 大小）、投递隔离
 * （深拷贝、接收方异常不断投递）、资格（停用 / 权限被收回的收不到）
 * 与清理（卸载后退订、登记摘除）。
 *
 * api 层另验：未声明 ipc 抛错、用户关掉开关返回 null、发送频控。
 */
import type { PluginManifest } from "./types";

import { beforeEach, afterEach, describe, expect, it, vi } from "vitest";

import { GetValue } from "../../wailsjs/go/bindings/ConfigAPI";

import { createPluginApi, runPluginCleanups } from "./api";
import { setPluginPermission, hydratePluginGrants } from "./grants";
import { markPluginActive } from "./registry";
import {
  deliverPluginMessage,
  ipcPlugins,
  MAX_PAYLOAD_BYTES,
  registerIpcPlugin,
  resetPluginBus,
  subscribePluginMessage,
  unregisterIpcPlugin,
  validatePluginMessage,
} from "./ipc";

vi.mock("../components/overlay/dialog", () => ({
  notify: {
    info: vi.fn(),
    success: vi.fn(),
    warning: vi.fn(),
    error: vi.fn(),
  },
  confirm: vi.fn(async () => true),
}));

vi.mock("../../wailsjs/runtime/runtime", () => ({
  EventsOn: vi.fn(() => () => {}),
}));

vi.mock("../../wailsjs/go/bindings/ConfigAPI", () => ({
  ClearValue: vi.fn(async () => undefined),
  GetValue: vi.fn(async () => ""),
  SetValue: vi.fn(async () => undefined),
}));

vi.mock("../../wailsjs/go/bindings/DownloadAPI", () => ({
  GetContentTasks: vi.fn(async () => []),
  GetCurrentDownloadSnapshot: vi.fn(async () => null),
}));

const manifest = (id: string): PluginManifest => ({
  id,
  name: `插件${id}`,
  version: "1.0.0",
  apiVersion: "1",
  capabilities: { ipc: true },
});

beforeEach(async () => {
  vi.mocked(GetValue).mockResolvedValue("{}");
  await hydratePluginGrants();
});

afterEach(() => {
  resetPluginBus();
});

describe("发送参数校验", () => {
  it("合法参数原样通过；未传负载归一为 null", () => {
    expect(validatePluginMessage(" ping ", { a: 1 })).toEqual({
      type: "ping",
      payload: { a: 1 },
    });
    expect(validatePluginMessage("ping", undefined).payload).toBeNull();
  });

  it("类型名为空或超长时抛错", () => {
    expect(() => validatePluginMessage("", null)).toThrow();
    expect(() => validatePluginMessage("x".repeat(65), null)).toThrow();
  });

  it("负载必须 JSON 可序列化：函数 / 循环引用抛错", () => {
    expect(() => validatePluginMessage("ping", () => 1)).toThrow();
    const circular: Record<string, unknown> = {};

    circular.self = circular;
    expect(() => validatePluginMessage("ping", circular)).toThrow();
  });

  it("负载超过上限抛错", () => {
    expect(() =>
      validatePluginMessage("ping", "x".repeat(MAX_PAYLOAD_BYTES + 1)),
    ).toThrow();
  });
});

describe("投递", () => {
  const sender = { id: "a", name: "A", version: "1" };
  const everyone = () => true;

  it("点对点只送达目标；广播送达全部监听者", () => {
    const toA = vi.fn();
    const toB = vi.fn();

    subscribePluginMessage("a", toA);
    subscribePluginMessage("b", toB);

    expect(deliverPluginMessage(sender, "b", "hello", { n: 1 }, everyone)).toBe(
      1,
    );
    expect(toA).not.toHaveBeenCalled();
    expect(toB).toHaveBeenCalledTimes(1);

    expect(deliverPluginMessage(sender, null, "all", null, everyone)).toBe(2);
  });

  it("每个接收者拿到独立深拷贝：改对象不回串发送方或其它接收者", () => {
    const payload = { list: [1, 2] };
    const seen: unknown[] = [];

    subscribePluginMessage("a", (message) => {
      (message.payload as { list: number[] }).list.push(99);
      seen.push(message.payload);
    });
    subscribePluginMessage("b", (message) => seen.push(message.payload));

    deliverPluginMessage(sender, null, "clone", payload, everyone);

    expect(seen[0]).toEqual({ list: [1, 2, 99] });
    expect(seen[1]).toEqual({ list: [1, 2] });
    expect(payload).toEqual({ list: [1, 2] });
  });

  it("接收方抛错不断投递：其它订阅者照收，抛错者不计入送达数", () => {
    const spy = vi.fn();

    subscribePluginMessage("bad", () => {
      throw new Error("炸了");
    });
    subscribePluginMessage("good", spy);

    expect(deliverPluginMessage(sender, null, "boom", null, everyone)).toBe(1);
    expect(spy).toHaveBeenCalledTimes(1);
  });

  it("canReceive 拒绝的订阅者收不到（停用 / 权限被收回）", () => {
    const spy = vi.fn();

    subscribePluginMessage("muted", spy);
    deliverPluginMessage(sender, null, "secret", null, () => false);

    expect(spy).not.toHaveBeenCalled();
  });

  it("from 是发送方身份的拷贝：接收方改它不影响下一次投递", () => {
    const ids: string[] = [];

    subscribePluginMessage("b", (message) => {
      message.from.id = "伪造";
      ids.push(message.from.id);
    });

    deliverPluginMessage(sender, "b", "x", null, everyone);
    deliverPluginMessage(sender, "b", "x", null, everyone);

    expect(ids).toEqual(["伪造", "伪造"]);
    expect(ipcPlugins()).not.toContainEqual(
      expect.objectContaining({ id: "伪造" }),
    );
  });
});

describe("登记与清理", () => {
  it("unregisterIpcPlugin 摘掉登记并退订它的全部订阅", () => {
    const spy = vi.fn();

    registerIpcPlugin(manifest("gone"));
    subscribePluginMessage("gone", spy);
    unregisterIpcPlugin("gone");

    deliverPluginMessage(
      { id: "x", name: "X", version: "1" },
      null,
      "x",
      null,
      () => true,
    );

    expect(spy).not.toHaveBeenCalled();
    expect(ipcPlugins().map((entry) => entry.id)).not.toContain("gone");
  });
});

describe("API 层权限门与频控", () => {
  it("未声明 ipc 时调用抛错", async () => {
    const api = createPluginApi({ ...manifest("no-ipc"), capabilities: {} });

    expect(() => api.ipc.broadcast("x")).toThrow("ipc");
    runPluginCleanups("no-ipc");
  });

  it("声明后 send/broadcast/onMessage 可用；用户关掉开关返回 null", async () => {
    markPluginActive("talky", true);
    const api = createPluginApi(manifest("talky"));
    const received: string[] = [];

    api.ipc.onMessage((message) => received.push(message.type));
    expect(api.ipc.send("talky", "self", null)).toBe(1);
    expect(received).toEqual(["self"]);

    await setPluginPermission("talky", "ipc", false);
    expect(api.ipc.broadcast("x")).toBeNull();
    expect(api.ipc.onMessage(() => {})).toBeNull();

    runPluginCleanups("talky");
    markPluginActive("talky", false);
  });

  it("发送频控：5 秒窗口第 101 条被丢弃（返回 0，不投递）", async () => {
    markPluginActive("flood", true);
    const api = createPluginApi(manifest("flood"));
    const spy = vi.fn();

    api.ipc.onMessage(spy);
    for (let index = 0; index < 100; index += 1) {
      api.ipc.broadcast("tick");
    }
    expect(spy).toHaveBeenCalledTimes(100);
    expect(api.ipc.broadcast("tick")).toBe(0);
    expect(spy).toHaveBeenCalledTimes(100);

    runPluginCleanups("flood");
    markPluginActive("flood", false);
  });

  it("runPluginCleanups 后自动退订且不再出现在发现列表", async () => {
    markPluginActive("bye", true);
    const api = createPluginApi(manifest("bye"));
    const spy = vi.fn();

    api.ipc.onMessage(spy);
    runPluginCleanups("bye");

    markPluginActive("other", true);
    const other = createPluginApi(manifest("other"));

    other.ipc.broadcast("late");
    expect(spy).not.toHaveBeenCalled();
    expect((other.ipc.plugins() ?? []).map((entry) => entry.id)).not.toContain(
      "bye",
    );

    runPluginCleanups("other");
    markPluginActive("other", false);
  });

  it("ipc.plugins 只包含已登记的插件", async () => {
    markPluginActive("p1", true);
    createPluginApi(manifest("p1"));

    const api = createPluginApi(manifest("p2"));

    expect((api.ipc.plugins() ?? []).map((entry) => entry.id)).toEqual([
      "p1",
      "p2",
    ]);

    runPluginCleanups("p1");
    runPluginCleanups("p2");
    markPluginActive("p1", false);
    markPluginActive("p2", false);
  });
});
