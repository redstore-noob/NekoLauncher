/*
 * Copyright 2026 烟花
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

/**
 * 更新检测前端逻辑的用例。重点防三类回归：
 *  1. 把「查不到（unknown）」显示成「已是最新」——谎报；
 *  2. 把「检查失败（checkFailed）」显示成「已是最新」——同样谎报；
 *  3. 覆盖式替换的确认文案里丢掉「会备份」这句话——静默覆盖用户文件。
 */

import type { download } from "../../wailsjs/go/models";

import { describe, expect, it } from "vitest";

import {
  badgeToneFor,
  downloadLink,
  findUpdateFor,
  hasModpackIssues,
  indexUpdateFiles,
  isUpdatable,
  modpackSummary,
  normalizePathKey,
  projectPageURL,
  replaceConfirmMessage,
  shouldShowBadge,
  suggestedFileName,
  summarizeResult,
  versionTransition,
} from "./instanceUpdates";

/** 造一个检测条目（只填用例关心的字段，其余留空）。 */
function entry(
  fields: Partial<download.ContentUpdateFile>,
): download.ContentUpdateFile {
  return { Status: "unknown", ...fields } as download.ContentUpdateFile;
}

describe("路径索引", () => {
  it("大小写与分隔符不同也能命中同一条目", () => {
    const index = indexUpdateFiles([
      entry({ FilePath: "C:\\Game\\mods\\Sodium.jar", Status: "updatable" }),
    ]);

    expect(findUpdateFor(index, "c:/game/mods/sodium.jar")?.Status).toBe(
      "updatable",
    );
  });

  it("空路径不建立索引项（避免把 undefined 变成键）", () => {
    const index = indexUpdateFiles([entry({ FilePath: "" }), entry({})]);

    expect(index.size).toBe(0);
    expect(normalizePathKey(undefined)).toBe("");
  });

  it("后端返回 null/未定义时不抛异常", () => {
    expect(indexUpdateFiles(null).size).toBe(0);
    expect(indexUpdateFiles(undefined).size).toBe(0);
    expect(findUpdateFor(indexUpdateFiles(null), "x.jar")).toBeNull();
  });
});

describe("可更新判定", () => {
  it("只有 updatable 才算可更新", () => {
    expect(isUpdatable(entry({ Status: "updatable" }))).toBe(true);
    expect(isUpdatable(entry({ Status: "latest" }))).toBe(false);
    expect(isUpdatable(entry({ Status: "unknown" }))).toBe(false);
    expect(isUpdatable(entry({ Status: "checkFailed" }))).toBe(false);
    expect(isUpdatable(null)).toBe(false);
  });

  it("防回归：查不到（unknown）必须给中性「未知」标记，而不是不显示", () => {
    expect(badgeToneFor(entry({ Status: "unknown" }))).toBe("unknown");
    expect(shouldShowBadge(entry({ Status: "unknown" }))).toBe(true);
  });

  it("防回归：检查失败（checkFailed）必须显示失败标记", () => {
    expect(badgeToneFor(entry({ Status: "checkFailed" }))).toBe("failed");
    expect(shouldShowBadge(entry({ Status: "checkFailed" }))).toBe(true);
  });

  it("已是最新与未检测（无结果）不显示角标", () => {
    expect(badgeToneFor(entry({ Status: "latest" }))).toBe("none");
    expect(badgeToneFor(entry({ Status: "skipped" }))).toBe("none");
    expect(badgeToneFor(null)).toBe("none");
    expect(shouldShowBadge(undefined)).toBe(false);
  });

  it("可更新用专用标记", () => {
    expect(badgeToneFor(entry({ Status: "updatable" }))).toBe("update");
  });
});

describe("版本与链接展示", () => {
  it("有新旧版本时展示迁移箭头", () => {
    expect(
      versionTransition(
        entry({ CurrentVersion: "1.0.0", LatestVersion: "1.1.0" }),
      ),
    ).toBe("1.0.0 → 1.1.0");
  });

  it("只有最新版本时不编造当前版本", () => {
    expect(versionTransition(entry({ LatestVersion: "1.1.0" }))).toBe("1.1.0");
    expect(versionTransition(entry({}))).toBe("");
    expect(versionTransition(null)).toBe("");
  });

  it("项目页地址来自 projectID，没有就不给链接", () => {
    expect(projectPageURL(entry({ ProjectID: "AABBCC" }))).toBe(
      "https://modrinth.com/project/AABBCC",
    );
    expect(projectPageURL(entry({}))).toBe("");
    expect(projectPageURL(null)).toBe("");
  });

  it("下载地址原样返回（没有则空串）", () => {
    expect(downloadLink(entry({ DownloadURL: "https://cdn/x.jar" }))).toBe(
      "https://cdn/x.jar",
    );
    expect(downloadLink(entry({}))).toBe("");
  });
});

describe("整合包比对展示", () => {
  it("缺失/被改动时提示有问题", () => {
    const result = {
      Modpack: {
        Status: "issues",
        MissingCount: 2,
        ModifiedCount: 1,
        StatusText: "清单比对：2 个文件缺失、1 个被改动、3 个一致。",
      },
    } as unknown as download.ContentUpdateCheckResult;

    expect(hasModpackIssues(result)).toBe(true);
    expect(modpackSummary(result)).toContain("缺失");
  });

  it("状态是 issues 但计数为 0 时不算问题（避免误报）", () => {
    const result = {
      Modpack: { Status: "issues", MissingCount: 0, ModifiedCount: 0 },
    } as unknown as download.ContentUpdateCheckResult;

    expect(hasModpackIssues(result)).toBe(false);
  });

  it("CurseForge 清单不可校验时只如实转述，不显示成功", () => {
    const result = {
      Modpack: {
        Status: "unsupported",
        StatusText:
          "这是 CurseForge 格式的整合包清单…需要 CurseForge API key。",
      },
    } as unknown as download.ContentUpdateCheckResult;

    expect(hasModpackIssues(result)).toBe(false);
    expect(modpackSummary(result)).toContain("API key");
  });

  it("摘要按后端计数拼装，「未知」不能被吞掉", () => {
    const result = {
      UpdatableCount: 1,
      LatestCount: 2,
      UnknownCount: 3,
      FailedCount: 0,
      SkippedCount: 1,
    } as unknown as download.ContentUpdateCheckResult;
    const summary = summarizeResult(result);

    expect(summary).toContain("可更新 1 个");
    expect(summary).toContain("已是最新 2 个");
    expect(summary).toContain("未知 3 个");
    expect(summary).toContain("跳过 1 个");
    expect(summary).not.toContain("检查失败");
  });

  it("摘要支持注入翻译函数（非中文环境下数字与文案都要在）", () => {
    const result = {
      UpdatableCount: 2,
      LatestCount: 0,
      UnknownCount: 0,
    } as unknown as download.ContentUpdateCheckResult;
    const summary = summarizeResult(result, (source, params) =>
      source.replace("{0}", String(params?.["0"] ?? "")),
    );

    expect(summary).toContain("可更新 2 个");
  });

  it("没有结果时摘要为空串", () => {
    expect(summarizeResult(null)).toBe("");
    expect(summarizeResult(undefined)).toBe("");
  });
});

describe("落地动作文案", () => {
  it("防回归：覆盖式替换的确认文案必须写明会先备份", () => {
    const message = replaceConfirmMessage(
      entry({ FileName: "sodium.jar", LatestVersion: "1.1.0" }),
    );

    expect(message).toContain("sodium.jar");
    expect(message).toContain("1.1.0");
    expect(message).toContain("备份");
    expect(message).toContain(".bak-");
  });

  it("没有最新版本号时也要提示备份", () => {
    expect(replaceConfirmMessage(entry({ FileName: "x.jar" }))).toContain(
      "备份",
    );
  });

  it("另存文件名带上版本号并保留扩展名", () => {
    expect(
      suggestedFileName(
        entry({ FileName: "sodium.jar", LatestVersion: "1.1.0" }),
      ),
    ).toBe("sodium-1.1.0.jar");
    expect(suggestedFileName(entry({ FileName: "pack.zip" }))).toBe(
      "pack-new.zip",
    );
  });

  it("没有扩展名/没有文件名时也给出可用名字", () => {
    expect(
      suggestedFileName(entry({ FileName: "noext", LatestVersion: "2" })),
    ).toBe("noext-2");
    expect(suggestedFileName(entry({}))).toBe("content-new.jar");
  });
});
