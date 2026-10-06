/*
 * 启动卡覆盖独占槽的行为约定：最后注册者生效、卸载归属插件自动回落。
 * 主页只认 getLaunchCardOverride()——这套语义坏了，插件主题卡就会
 * 在卸载后留下无人认领的孤儿渲染，这里用单测钉死。
 */
import { describe, expect, it, vi } from "vitest";

import {
  getLaunchCardOverride,
  pageActionsFor,
  registerPage,
  registerPageAction,
  registerWidget,
  setLaunchCardOverride,
  unregisterPlugin,
  widgetById,
} from "./registry";

describe("launch card override slot", () => {
  it("last registration wins and owner unregister falls back to null", () => {
    expect(getLaunchCardOverride()).toBeNull();

    const renderA = () => null;

    setLaunchCardOverride("themeA", renderA);
    expect(getLaunchCardOverride()).toEqual({
      pluginId: "themeA",
      render: renderA,
    });

    // 独占槽：后注册者直接接管
    const renderB = () => null;

    setLaunchCardOverride("themeB", renderB);
    expect(getLaunchCardOverride()?.pluginId).toBe("themeB");

    // 其它插件卸载不影响当前覆盖
    unregisterPlugin("themeA");
    expect(getLaunchCardOverride()?.pluginId).toBe("themeB");

    // 拥有者卸载：覆盖自动摘除，主页回落内置卡片
    unregisterPlugin("themeB");
    expect(getLaunchCardOverride()).toBeNull();
  });

  it("unregisterPlugin still clears widgets by id prefix", () => {
    registerWidget({
      description: "",
      icon: null,
      id: "myPlugin:card",
      render: () => null,
      tileClass: "",
      title: "demo",
    });
    expect(widgetById("myPlugin:card")).toBeDefined();

    unregisterPlugin("myPlugin");
    expect(widgetById("myPlugin:card")).toBeUndefined();
    // 摘组件不影响别的插件挂着的启动卡覆盖（本用例里本就无覆盖）
    expect(getLaunchCardOverride()).toBeNull();
  });
});

describe("page action slot", () => {
  it("按页面分组、按 order 排序，插件卸载时一并摘除", () => {
    registerPage({
      id: "instances",
      label: "实例",
      icon: null,
      order: 40,
      render: () => null,
    });
    registerPage({
      id: "download",
      label: "下载",
      icon: null,
      order: 30,
      render: () => null,
    });
    registerPageAction({
      pageId: "instances",
      id: "pa:b",
      label: "B",
      onPress: () => {},
      order: 2,
    });
    registerPageAction({
      pageId: "instances",
      id: "pa:a",
      label: "A",
      onPress: () => {},
      order: 1,
    });
    registerPageAction({
      pageId: "download",
      id: "pa:c",
      label: "C",
      onPress: () => {},
    });

    expect(pageActionsFor("instances").map((action) => action.id)).toEqual([
      "pa:a",
      "pa:b",
    ]);
    expect(pageActionsFor("download").map((action) => action.id)).toEqual([
      "pa:c",
    ]);
    // 没有注册项的页面拿到的是同一个空数组（快照引用要稳定）
    expect(pageActionsFor("home")).toEqual([]);
    expect(pageActionsFor("home")).toBe(pageActionsFor("home"));

    unregisterPlugin("pa");
    expect(pageActionsFor("instances")).toEqual([]);
    expect(pageActionsFor("download")).toEqual([]);
  });

  it("目标页面未注册只提醒不报错（插件加载顺序无法保证）", () => {
    const warn = vi.spyOn(console, "warn").mockImplementation(() => {});

    registerPageAction({
      pageId: "还没注册的页面",
      id: "pa:d",
      label: "D",
      onPress: () => {},
    });

    expect(warn).toHaveBeenCalledTimes(1);
    expect(pageActionsFor("还没注册的页面")).toHaveLength(1);
    warn.mockRestore();
    unregisterPlugin("pa");
  });

  it("缺少 id / pageId / onPress 直接抛错", () => {
    expect(() =>
      registerPageAction({
        pageId: "instances",
        id: "",
        label: "x",
        onPress: () => {},
      }),
    ).toThrow();
    expect(() =>
      registerPageAction({
        pageId: "",
        id: "pa:e",
        label: "x",
        onPress: () => {},
      }),
    ).toThrow();
    expect(() =>
      registerPageAction({
        pageId: "instances",
        id: "pa:f",
        label: "x",
      } as never),
    ).toThrow();
  });
});
