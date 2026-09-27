/*
 * lib/crashNotice.ts 的崩溃判定与诊断文本测试（P3-6）。
 *
 * 这一段的价值全在"别误报、别漏报"：
 *   - 漏报 → 游戏崩了用户看到窗口一闪，什么提示都没有；
 *   - 误报 → 用户自己点"停止"也被弹一个"游戏异常退出"，很快就不信这个提示了。
 * 所以判定同时看后端标题与退出码，两条路径都要有用例。
 */
import type { launch } from "../../wailsjs/go/models";

import { describe, expect, it, vi } from "vitest";

// 崩溃提示会 import 弹窗与 LogViewer（含 HeroUI），测试只关心判定与文本：
// 把这些副作用模块挡掉，用例就只跑纯逻辑。
vi.mock("../components/LogViewer", () => ({ openLogViewer: vi.fn() }));
vi.mock("../components/overlay/dialog", () => ({
  showDialogBox: vi.fn(),
  notify: { success: vi.fn(), error: vi.fn(), info: vi.fn(), warning: vi.fn() },
}));
vi.mock("../../wailsjs/go/bindings/LauncherAPI", () => ({
  DiagnoseCrash: vi.fn().mockResolvedValue(null),
}));
vi.mock("../../wailsjs/go/bindings/SystemAPI", () => ({
  OpenPath: vi.fn().mockResolvedValue(undefined),
}));

import {
  detectCrash,
  formatDiagnosis,
  LAUNCH_PHASE_EXITED,
  notifyCrashIfNeeded,
} from "./crashNotice";

function snapshot(partial: Partial<launch.GameLaunchSnapshot>) {
  return {
    Phase: LAUNCH_PHASE_EXITED,
    ...partial,
  } as launch.GameLaunchSnapshot;
}

describe("detectCrash", () => {
  it("非退出阶段一律不算崩溃", () => {
    expect(detectCrash(snapshot({ Phase: 3, Title: "游戏异常退出" }))).toEqual({
      crashed: false,
      exitCode: null,
    });
  });

  it("非零退出码判为崩溃，并解析出退出码", () => {
    expect(detectCrash(snapshot({ Message: "退出代码：1" }))).toEqual({
      crashed: true,
      exitCode: 1,
    });
    expect(detectCrash(snapshot({ Message: "退出代码: -1073741819" }))).toEqual(
      {
        crashed: true,
        exitCode: -1073741819,
      },
    );
  });

  it("退出码 0 是正常退出，不算崩溃", () => {
    expect(detectCrash(snapshot({ Message: "退出代码：0" }))).toEqual({
      crashed: false,
      exitCode: 0,
    });
  });

  it("解析不到退出码时看后端标题：只有「异常退出」才算崩溃", () => {
    expect(detectCrash(snapshot({ Title: "游戏异常退出" })).crashed).toBe(true);
    expect(detectCrash(snapshot({ Title: "游戏已停止" })).crashed).toBe(false);
    expect(detectCrash(snapshot({})).crashed).toBe(false);
  });

  it("标题与消息都缺失时不误报", () => {
    expect(detectCrash({} as launch.GameLaunchSnapshot).crashed).toBe(false);
  });
});

describe("notifyCrashIfNeeded", () => {
  it("正常退出不弹窗", () => {
    expect(
      notifyCrashIfNeeded(snapshot({ Message: "退出代码：0", Revision: 1 })),
    ).toBe(false);
  });
});

describe("formatDiagnosis", () => {
  it("包含退出码、描述、异常、报告路径与可能原因", () => {
    const text = formatDiagnosis(
      {
        Description: "模组冲突",
        Exception: "java.lang.NoClassDefFoundError",
        ReportPath: "C:\\game\\crash-reports\\crash-1.txt",
        Suspected: ["缺少前置", "内存不足"],
        Suggestions: ["安装 Fabric API", ""],
      } as unknown as launch.CrashDiagnosis,
      1,
    );

    expect(text).toContain("NekoLauncher 崩溃诊断");
    expect(text).toContain("退出代码：1");
    expect(text).toContain("崩溃描述：模组冲突");
    expect(text).toContain("异常：java.lang.NoClassDefFoundError");
    expect(text).toContain("崩溃报告：C:\\game\\crash-reports\\crash-1.txt");
    expect(text).toContain("- 缺少前置：安装 Fabric API");
    expect(text).toContain("- 内存不足");
    expect(text).toContain("完整日志");
  });

  it("退出码未知时写明「未知」，诊断为空时也不报错", () => {
    const text = formatDiagnosis(null, null);

    expect(text).toContain("退出代码：未知");
    expect(text).not.toContain("可能原因");
  });
});
