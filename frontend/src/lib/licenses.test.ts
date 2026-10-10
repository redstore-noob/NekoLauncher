/*
 * 关于页「开源致谢」清单的清单校验。
 *
 * 这份清单是给用户看的合规声明,写错比不写更糟:漏项、条目字段残缺、
 * 同名重复都会让"许可列表"变得不可信。这里用纯数据断言把它们锁住——
 * 新增依赖时忘了登记,或者复制粘贴写漏了字段,CI 会当场拦下。
 */
import { describe, expect, it } from "vitest";

import { LICENSES, PROJECT_SPDX } from "./licenses";

const GROUPS = ["前端", "后端", "工具链", "外部服务与工具"];

describe("开源许可清单", () => {
  it("每一条都有完整的名称/SPDX/链接/分组", () => {
    for (const entry of LICENSES) {
      expect(entry.name.trim(), `${entry.name} 缺名称`).not.toBe("");
      expect(entry.spdx.trim(), `${entry.name} 缺 SPDX`).not.toBe("");
      expect(entry.url, `${entry.name} 的链接必须是 http(s)`).toMatch(
        /^https?:\/\//,
      );
      expect(GROUPS, `${entry.name} 的分组非法:${entry.group}`).toContain(
        entry.group,
      );
    }
  });

  it("同名依赖不重复登记", () => {
    const names = LICENSES.map((entry) => entry.name);
    const duplicated = names.filter(
      (name, index) => names.indexOf(name) !== index,
    );

    expect(duplicated).toEqual([]);
  });

  it("下载镜像 BMCLAPI 单独登记为 MIT 外部服务与工具", () => {
    const bmclapi = LICENSES.find((entry) => entry.name === "BMCLAPI");

    expect(bmclapi, "BMCLAPI 未登记").toBeDefined();
    expect(bmclapi?.spdx).toBe("MIT");
    expect(bmclapi?.url).toContain("bangbang93/openbmclapi");
    expect(bmclapi?.group).toBe("外部服务与工具");
    // 服务类条目必须带说明:它不随启动器分发,只是请求它的接口
    expect(bmclapi?.note?.trim()).not.toBe("");
  });

  it("联机侧两个外部工具都登记了 copyleft 许可", () => {
    const terracotta = LICENSES.find((entry) =>
      entry.name.startsWith("Terracotta"),
    );
    const redstone = LICENSES.find((entry) =>
      entry.name.startsWith("RedstoneOnline"),
    );

    expect(terracotta?.spdx).toBe("AGPL-3.0-only");
    expect(terracotta?.url).toContain("burningtnt/Terracotta");
    expect(terracotta?.group).toBe("外部服务与工具");
    expect(terracotta?.note?.trim()).not.toBe("");

    expect(redstone?.spdx).toBe("GPL-3.0-or-later");
    expect(redstone?.url).toContain("redstoneonline");
    expect(redstone?.group).toBe("外部服务与工具");
    expect(redstone?.note?.trim()).not.toBe("");
  });

  it("四个分组都有条目(前端/后端/工具链/外部服务与工具)", () => {
    for (const group of GROUPS) {
      expect(
        LICENSES.some((entry) => entry.group === group),
        `分组「${group}」没有任何条目`,
      ).toBe(true);
    }
  });

  it("本启动器自身声明为 Apache-2.0", () => {
    expect(PROJECT_SPDX).toBe("Apache-2.0");
  });
});
