/*
 * 小组件实例 id（重复放置支持）的行为测试：
 * base id 剥离 "#n" 后缀、实例分配不冲突且复用空位。
 */
import { describe, expect, it } from "vitest";

import { allocateWidgetInstanceId, widgetBaseId } from "./home";

describe("widgetBaseId", () => {
  it("无后缀的原样返回（与历史布局兼容）", () => {
    expect(widgetBaseId("rss")).toBe("rss");
  });

  it("剥掉重复放置产生的 #n 后缀", () => {
    expect(widgetBaseId("rss#2")).toBe("rss");
    expect(widgetBaseId("myPlugin:card#10")).toBe("myPlugin:card");
  });

  it("id 本身以数字结尾不受影响（# 后必须是纯数字）", () => {
    expect(widgetBaseId("quickjoin2")).toBe("quickjoin2");
    expect(widgetBaseId("a#x")).toBe("a#x");
  });
});

describe("allocateWidgetInstanceId", () => {
  it("首次放置用原 id", () => {
    expect(allocateWidgetInstanceId(["log", "playtime"], "rss")).toBe("rss");
  });

  it("已有同名组件时分配最小空闲 #n", () => {
    expect(allocateWidgetInstanceId(["rss"], "rss")).toBe("rss#2");
    expect(allocateWidgetInstanceId(["rss", "rss#2"], "rss")).toBe("rss#3");
  });

  it("删掉中间实例后空位可复用", () => {
    // 删掉了 rss#2（用户删的是第二份）后再次放置应回到 rss#2
    expect(allocateWidgetInstanceId(["rss", "rss#3"], "rss")).toBe("rss#2");
  });
});
