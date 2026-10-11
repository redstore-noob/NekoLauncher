import type { instance } from "../../wailsjs/go/models";

import { describe, expect, it } from "vitest";

import { isInstanceSelectionChange } from "./instanceSnapshot";

const snapshot: instance.GameInstanceSnapshot = {
  SourcePath: "C:/Minecraft",
  MinecraftDirectory: "C:/Minecraft",
  GameDirectory: "",
  VersionIds: ["vanilla", "fabric"],
  SelectedVersionId: "vanilla",
  UsesVersionDirectoryAsGameDirectory: false,
  IsLoading: false,
  ErrorMessage: "",
};

describe("实例快照事件分类", () => {
  it("切换到隔离实例只同步选中态，不重载整个列表", () => {
    expect(
      isInstanceSelectionChange(snapshot, {
        ...snapshot,
        SelectedVersionId: "fabric",
        GameDirectory: "C:/Minecraft/versions/fabric",
        UsesVersionDirectoryAsGameDirectory: true,
      }),
    ).toBe(true);
  });

  it("重复发布相同快照不触发整页重载", () => {
    expect(isInstanceSelectionChange(snapshot, { ...snapshot })).toBe(true);
  });

  it("首次快照仍需加载列表", () => {
    expect(isInstanceSelectionChange(null, snapshot)).toBe(false);
  });

  it("扫描结束即使版本列表没变也要刷新内容", () => {
    expect(
      isInstanceSelectionChange({ ...snapshot, IsLoading: true }, snapshot),
    ).toBe(false);
  });

  it.each([
    { IsLoading: true },
    { SourcePath: "C:/OtherLauncher/instance" },
    { MinecraftDirectory: "C:/OtherMinecraft" },
    { VersionIds: ["vanilla", "fabric", "forge"] },
    { VersionIds: ["vanilla"] },
    { VersionIds: ["fabric", "vanilla"] },
    { ErrorMessage: "无法读取目录" },
  ])("实际目录、列表或扫描状态变化不能被当成选中事件：%j", (change) => {
    expect(
      isInstanceSelectionChange(snapshot, { ...snapshot, ...change }),
    ).toBe(false);
  });
});
