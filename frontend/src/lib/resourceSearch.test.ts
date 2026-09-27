/*
 * lib/resourceSearch.ts 的纯函数测试（X-3 资源搜索）。
 *
 * 防的是"下载落点错位"：项目类型到内容子目录的映射写错时，下载本身会成功、
 * 界面也会显示"已安装"，但游戏里就是找不到这个资源——这种回归只能靠测试拦住。
 * 映射必须与 Go 侧 models.SubDirectoryForProjectType 完全一致（那边另有用例）。
 */
import { describe, expect, it } from "vitest";

import { sourceLabel, subDirectoryForProjectType } from "./resourceSearch";

describe("subDirectoryForProjectType", () => {
  it("按项目类型给出正确的实例内容子目录", () => {
    expect(subDirectoryForProjectType("mod")).toBe("mods");
    expect(subDirectoryForProjectType("shader")).toBe("shaderpacks");
    expect(subDirectoryForProjectType("resourcepack")).toBe("resourcepacks");
  });

  it("接受别名（shaderpack / resourcepacks），与 Go 侧归一化口径一致", () => {
    expect(subDirectoryForProjectType("shaderpack")).toBe("shaderpacks");
    expect(subDirectoryForProjectType("resourcepacks")).toBe("resourcepacks");
  });

  it("未知或缺失类型回退 mods，而不是落到游戏目录根", () => {
    expect(subDirectoryForProjectType("")).toBe("mods");
    expect(subDirectoryForProjectType("datapack")).toBe("mods");
  });
});

describe("sourceLabel", () => {
  it("两个资源站显示各自的正式名称", () => {
    expect(sourceLabel("modrinth")).toBe("Modrinth");
    expect(sourceLabel("curseforge")).toBe("CurseForge");
  });

  it("未知来源按 Modrinth 显示（后端默认值也是它）", () => {
    expect(sourceLabel("")).toBe("Modrinth");
    expect(sourceLabel("unknown")).toBe("Modrinth");
  });
});
