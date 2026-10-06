/*
 * backend-gate 单测：验证三件事——window.go 被收走、宿主视图照常工作、
 * 原始出口（WailsInvoke / chrome.webview.postMessage）对 'C' 消息上闸。
 */
import { beforeEach, describe, expect, it, vi } from "vitest";

import {
  __resetBackendGateForTest,
  hostGo,
  installBackendGate,
} from "./backend-gate";

type Invoke = (message: unknown) => void;
type W = Window & typeof globalThis & Record<string, unknown>;

/** 搭一个最小 wails 环境：window.go 绑定树 + WailsInvoke 记录器 */
function fakeWailsEnv() {
  const w = window as W;
  const calls: unknown[] = [];
  const binding = vi.fn((...args: unknown[]) => {
    (w.WailsInvoke as Invoke)(
      `C${JSON.stringify({ name: "bindings.SystemAPI.Read", args })}`,
    );

    return Promise.resolve("ok");
  });

  w.go = { bindings: { SystemAPI: { Read: binding } } };
  w.WailsInvoke = (message: unknown) => {
    calls.push(message);
  };

  return { w, calls, binding };
}

describe("backend-gate", () => {
  beforeEach(() => {
    __resetBackendGateForTest();
    const w = window as W;

    delete w.go;
    delete w.WailsInvoke;
    delete w.ObfuscatedCall;
    delete w.chrome;
  });

  it("window.go 被换成只抛错的 getter，宿主视图仍可调用绑定", () => {
    const { calls } = fakeWailsEnv();

    installBackendGate();

    expect(() => (window as W).go).toThrow(/后端闸门/);

    const view = hostGo() as {
      bindings: { SystemAPI: { Read: (p: string) => Promise<string> } };
    };

    return view.bindings.SystemAPI.Read("/etc/passwd").then((result) => {
      expect(result).toBe("ok");
      expect(calls).toHaveLength(1);
      expect(String(calls[0])).toContain("bindings.SystemAPI.Read");
    });
  });

  it("非宿主帧直呼 WailsInvoke 的 C 消息被拦截，运行时消息放行", () => {
    const { w } = fakeWailsEnv();

    installBackendGate();

    const invoke = w.WailsInvoke as Invoke;

    expect(() =>
      invoke('C{"name":"bindings.PluginAPI.InstallPluginArchive"}'),
    ).toThrow(/拦截/);
    // 拖动 / 退出 / 事件等运行时自身消息不受影响
    expect(() => invoke("drag")).not.toThrow();
    expect(() => invoke("Q")).not.toThrow();
    expect(() => invoke("EXsome-event")).not.toThrow();
  });

  it("chrome.webview.postMessage 同样上闸", () => {
    const w = fakeWailsEnv().w;
    const posted: unknown[] = [];

    w.chrome = {
      webview: { postMessage: (m: unknown) => posted.push(m) },
    } as unknown as W["chrome"];

    installBackendGate();

    const post = (w.chrome as { webview: { postMessage: Invoke } }).webview
      .postMessage;

    expect(() => post('C{"name":"x"}')).toThrow(/拦截/);
    expect(() => post("drag")).not.toThrow();
    expect(posted).toEqual(["drag"]);
  });

  it("ObfuscatedCall 被禁用", () => {
    const w = fakeWailsEnv().w;

    w.ObfuscatedCall = () => Promise.resolve("leak");
    installBackendGate();

    expect(() => (w.ObfuscatedCall as () => unknown)()).toThrow(/禁用/);
  });
});
