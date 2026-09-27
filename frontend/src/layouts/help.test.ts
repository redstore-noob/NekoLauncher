/*
 * 帮助页内容的完整性测试（P3-6 前端测试起步的一部分）。
 *
 * 这页的价值全在"条目能被找到、点了能去对地方"：
 *   - section 写错一个字符，条目就会从界面上彻底消失（分组按 section 过滤）；
 *   - 跳转写错页面 id，按钮点了没反应（navigateToPage 对未知 id 只会回落默认页）；
 *   - 条目 id 重复，React key 冲突导致渲染错乱。
 * 这些都是"加一条 FAQ 时手滑"就会犯的错，而人工检查很容易漏。
 */
import { beforeAll, describe, expect, it } from "vitest";

import { pageById, registerBuiltins } from "../plugin";

import { HELP_ENTRIES, HELP_SECTIONS } from "./help";

describe("帮助页内容", () => {
  // 注册内置页面后才能校验跳转目标。代价是这里会把全部页面组件拉进来
  // （three.js / HeroUI 都在里面），这个文件因此比其它测试慢十几秒——
  // 换来的是"跳转写错页面 id"能在 CI 被抓到，值得。
  beforeAll(() => {
    registerBuiltins();
  });

  it("每条都落在已声明的分组里（否则界面上看不到）", () => {
    for (const entry of HELP_ENTRIES) {
      expect(
        HELP_SECTIONS,
        `条目「${entry.title}」的分组「${entry.section}」不在 HELP_SECTIONS 里`,
      ).toContain(entry.section);
    }
  });

  it("条目 id 不重复（重复会让 React key 冲突）", () => {
    const ids = HELP_ENTRIES.map((entry) => entry.id);

    expect(new Set(ids).size).toBe(ids.length);
  });

  it("标题与正文都不为空", () => {
    for (const entry of HELP_ENTRIES) {
      expect(entry.title.trim().length).toBeGreaterThan(0);
      expect(entry.body.length).toBeGreaterThan(0);
      for (const line of entry.body) {
        expect(line.trim().length).toBeGreaterThan(0);
      }
    }
  });

  it("跳转目标必须是真实注册过的页面 id", () => {
    for (const entry of HELP_ENTRIES) {
      if (!entry.action) continue;
      expect(
        pageById(entry.action.pageId),
        `条目「${entry.title}」跳转的页面「${entry.action.pageId}」不存在`,
      ).toBeTruthy();
    }
  });
});
