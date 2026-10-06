import { describe, expect, it } from "vitest";

import {
  DEFAULT_UI_THEME_ID,
  applyUiTheme,
  getUiTheme,
  getUiThemes,
  registerUiTheme,
  subscribeUiThemes,
  unregisterUiTheme,
} from "./ui-themes";

describe("ui-themes 注册表", () => {
  it("注册后可列出并按 id 取回", () => {
    registerUiTheme({
      id: "test-a",
      name: "测试 A",
      apply: () => undefined,
    });

    expect(getUiTheme("test-a")?.name).toBe("测试 A");
    expect(getUiThemes().some((theme) => theme.id === "test-a")).toBe(true);

    unregisterUiTheme("test-a");
    expect(getUiTheme("test-a")).toBeUndefined();
  });

  it("applyUiTheme 先复位全部主题再点亮选中的", () => {
    const calls: string[] = [];

    registerUiTheme({
      id: "test-a",
      name: "A",
      apply: (on) => calls.push(`a:${on}`),
    });
    registerUiTheme({
      id: "test-b",
      name: "B",
      apply: (on) => calls.push(`b:${on}`),
    });

    calls.length = 0;
    applyUiTheme("test-b");
    expect(calls).toEqual(["a:false", "b:true"]);

    // 默认主题 = 全部关闭
    calls.length = 0;
    applyUiTheme(DEFAULT_UI_THEME_ID);
    expect(calls).toEqual(["a:false", "b:false"]);

    // 未注册的 id 同默认主题（不抛错、全关）
    calls.length = 0;
    applyUiTheme("nonexistent");
    expect(calls).toEqual(["a:false", "b:false"]);

    unregisterUiTheme("test-a");
    unregisterUiTheme("test-b");
  });

  it("注销当前选中的主题时整体回落默认（全关），并通知订阅者", () => {
    const calls: string[] = [];
    let notifications = 0;
    const unsubscribe = subscribeUiThemes(() => {
      notifications += 1;
    });

    registerUiTheme({
      id: "test-a",
      name: "A",
      apply: (on) => calls.push(`a:${on}`),
    });
    expect(notifications).toBe(1);

    applyUiTheme("test-a");
    calls.length = 0;

    // 注销选中的主题 → 回落默认（apply(false)），并再通知一次列表更新
    unregisterUiTheme("test-a");
    expect(calls).toEqual(["a:false"]);
    expect(notifications).toBe(2);
    expect(getUiTheme("test-a")).toBeUndefined();

    unsubscribe();
  });
});
