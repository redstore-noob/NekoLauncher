/*
 * 插件高危动作闸门的策略单测：动作表完整性、确认语义（一次 / 本次运行 / 拒绝）、
 * 并发确认合并、拒绝记录与遗忘、公示节流、NekoPrompt 接线。
 *
 * 这里全部用注入时钟与注入确认框，不依赖真实时间与浮层实现——闸门要在这三种情况下
 * 都表现正确：插件循环调用（不能刷屏）、插件一口气并发调用（只该问一次）、
 * 用户点了拒绝（动作必须失败而不是静默通过）。
 */
import type { PluginManifest } from "./types";
import type {
  SensitiveAction,
  SensitiveActionSpec,
  SensitiveAnswer,
} from "./sensitive";

import { describe, expect, it, vi } from "vitest";

// 默认确认框与公示出口都走宿主浮层门面：换成桩，断言"弹了什么、给了哪些按钮"
const dialogSpy = vi.hoisted(() => ({
  showDialogBox: vi.fn(),
  info: vi.fn(),
  success: vi.fn(),
  warning: vi.fn(),
  error: vi.fn(),
  alert: vi.fn(),
  confirm: vi.fn(),
  promptDialog: vi.fn(),
  hideAlertNow: vi.fn(),
}));

vi.mock("../components/overlay/dialog", () => ({
  notify: {
    info: dialogSpy.info,
    success: dialogSpy.success,
    warning: dialogSpy.warning,
    error: dialogSpy.error,
  },
  alert: dialogSpy.alert,
  confirm: dialogSpy.confirm,
  promptDialog: dialogSpy.promptDialog,
  showDialogBox: dialogSpy.showDialogBox,
  hideAlertNow: dialogSpy.hideAlertNow,
}));

import {
  buildPrompt,
  createSensitiveGate,
  SENSITIVE_ACTIONS,
  sensitiveDetail,
  sensitiveFileName,
} from "./sensitive";

const plugin = (id: string, name = ""): PluginManifest => ({
  id,
  name,
  version: "1.0.0",
  apiVersion: "1",
});

/** 造一个带可控时钟与可控确认框的闸门 */
function gate(options: { answer?: SensitiveAnswer; burstMax?: number } = {}) {
  let clock = 1_000_000;
  const shown: Array<{ message: string; level: string }> = [];
  const asked: string[] = [];
  const instance = createSensitiveGate({
    now: () => clock,
    show: (message, level) => shown.push({ message, level }),
    ask: async (_manifest, _spec, message) => {
      asked.push(message);

      return options.answer ?? "session";
    },
    burstMax: options.burstMax,
  });

  return {
    shown,
    asked,
    instance,
    advance: (ms: number) => {
      clock += ms;
    },
  };
}

describe("SENSITIVE_ACTIONS", () => {
  it("每条动作都有级别、闸门、短名与带插件名占位的文案", () => {
    for (const key of Object.keys(SENSITIVE_ACTIONS) as SensitiveAction[]) {
      const spec: SensitiveActionSpec = SENSITIVE_ACTIONS[key];

      expect(spec.gate === "confirm" || spec.gate === "announce", key).toBe(
        true,
      );
      expect(spec.level === "info" || spec.level === "warning", key).toBe(true);
      expect(spec.label.length, key).toBeGreaterThan(0);
      expect(spec.template, key).toContain("{0}");
      // confirm 级必须说清"允许之后它能干什么"
      if (spec.gate === "confirm") expect(spec.prompt, key).toBeTruthy();
      else expect(spec.prompt, key).toBeUndefined();
    }
  });

  it("需要用户确认的正好是会拉起进程 / 写磁盘 / 改写启动参数的那几个", () => {
    const confirmGated = Object.entries(SENSITIVE_ACTIONS)
      .filter(([, spec]) => spec.gate === "confirm")
      .map(([key]) => key)
      .sort();

    expect(confirmGated).toEqual([
      "install-content",
      "launch",
      "modify-instance",
      "modify-settings",
    ]);
  });
});

describe("确认闸门", () => {
  it("confirm 级动作先问后做：问过一次（本次运行）之后不再问", async () => {
    const { instance, asked, shown } = gate({ answer: "session" });

    await instance.request(
      plugin("dl", "下载看门狗"),
      "install-content",
      "（a.jar）",
    );
    await instance.request(
      plugin("dl", "下载看门狗"),
      "install-content",
      "（b.jar）",
    );

    expect(asked).toHaveLength(1);
    // 每次都公示（4 秒窗口内合并成一条）
    expect(shown).toHaveLength(1);
    expect(shown[0].message).toBe("插件「下载看门狗」正在下载 / 安装（a.jar）");
  });

  it("同意一次只作用于本次调用", async () => {
    const { instance, asked } = gate({ answer: "once" });

    await instance.request(plugin("dl"), "install-content");
    await instance.request(plugin("dl"), "install-content");

    expect(asked).toHaveLength(2);
  });

  it("拒绝：抛错、并且本次运行内不再打扰用户（后续直接失败）", async () => {
    const { instance, asked, shown } = gate({ answer: "deny" });

    await expect(
      instance.request(plugin("dl", "下载看门狗"), "install-content"),
    ).rejects.toThrow("插件「下载看门狗」的「下载 / 安装内容」操作被用户拒绝");

    await expect(
      instance.request(plugin("dl"), "install-content"),
    ).rejects.toThrow("被用户拒绝");
    expect(asked).toHaveLength(1);
    // 被拒绝的动作不该公示成"正在做"
    expect(shown).toEqual([]);
  });

  it("forget 之后重新询问（插件重新加载即可解封）", async () => {
    const { instance, asked } = gate({ answer: "deny" });

    await expect(instance.request(plugin("dl"), "launch")).rejects.toThrow();
    instance.forget("dl");
    await expect(instance.request(plugin("dl"), "launch")).rejects.toThrow();
    expect(asked).toHaveLength(2);

    // 只忘掉指定插件
    await expect(instance.request(plugin("other"), "launch")).rejects.toThrow();
    expect(asked).toHaveLength(3);
  });

  it("并发调用共用一个确认框（插件一口气发起 5 次只问一次）", async () => {
    const { instance, asked, shown } = gate({ answer: "session" });

    await Promise.all(
      Array.from({ length: 5 }, (_, index) =>
        instance.request(plugin("dl"), "install-content", `（f${index}）`),
      ),
    );

    expect(asked).toHaveLength(1);
    expect(shown).toHaveLength(1);
  });

  it("announce 级动作不打扰用户，只公示", async () => {
    const { instance, asked, shown } = gate();

    await instance.request(plugin("dl"), "switch-instance", "（1.21.1）");

    expect(asked).toEqual([]);
    expect(shown).toEqual([
      { message: "插件「dl」把当前实例切到（1.21.1）", level: "info" },
    ]);
  });

  it("未知动作不确认也不公示（表是唯一入口）", async () => {
    const { instance, asked, shown } = gate();

    // @ts-expect-error 故意传一个不在表里的动作名
    await instance.request(plugin("dl"), "delete-everything");

    expect(asked).toEqual([]);
    expect(shown).toEqual([]);
  });
});

describe("公示节流", () => {
  it("同一插件同一动作的公示在窗口内合并，窗口过后再报", () => {
    const { instance, shown, advance } = gate();

    expect(instance.announce(plugin("dl"), "install-content", "（a）")).toBe(
      true,
    );
    expect(instance.announce(plugin("dl"), "install-content", "（b）")).toBe(
      false,
    );
    advance(4000);
    expect(instance.announce(plugin("dl"), "install-content", "（c）")).toBe(
      true,
    );
    expect(shown.map((entry) => entry.message)).toEqual([
      "插件「dl」正在下载 / 安装（a）",
      "插件「dl」正在下载 / 安装（c）",
    ]);
  });

  it("突发上限：多插件一起动作时只放行前几条", () => {
    const { instance, shown, advance } = gate({ burstMax: 2 });

    expect(instance.announce(plugin("a"), "install-content")).toBe(true);
    expect(instance.announce(plugin("b"), "launch")).toBe(true);
    expect(instance.announce(plugin("c"), "modify-settings")).toBe(false);
    advance(3000);
    expect(instance.announce(plugin("c"), "modify-settings")).toBe(true);
    expect(shown).toHaveLength(3);
  });
});

describe("确认框正文", () => {
  it("buildPrompt 带上动作说明、本次对象与决定后果", () => {
    const text = buildPrompt(SENSITIVE_ACTIONS.launch, "（fabric-1.21）");

    expect(text).toContain("启动游戏");
    expect(text).toContain("本次操作：（fabric-1.21）");
    expect(text).toContain("本次运行内不再询问");
  });
});

describe("默认接 NekoPrompt", () => {
  it("confirm 级动作弹三按钮确认框，标题写明插件名、默认落在拒绝上", async () => {
    dialogSpy.showDialogBox.mockResolvedValue("session");
    dialogSpy.warning.mockClear();
    const instance = createSensitiveGate();
    const manifest = plugin("dl", "下载看门狗");

    await instance.request(manifest, "modify-settings");

    expect(dialogSpy.showDialogBox).toHaveBeenCalledTimes(1);
    const request = dialogSpy.showDialogBox.mock.calls[0][0] as {
      title: string;
      message: string;
      severity: string;
      buttons: Array<{ id: string; label: string; default?: boolean }>;
    };

    expect(request.title).toBe("插件「下载看门狗」请求确认");
    expect(request.severity).toBe("warning");
    expect(request.message).toContain("包装命令");
    expect(request.buttons.map((button) => button.id)).toEqual([
      "once",
      "session",
      "deny",
    ]);
    expect(request.buttons.find((button) => button.default)?.id).toBe("deny");
    expect(dialogSpy.warning).toHaveBeenCalledWith(
      "插件「下载看门狗」保存了启动器设置",
      5000,
    );
  });

  it("用户在确认框里点拒绝：动作以错误结束且不公示", async () => {
    dialogSpy.showDialogBox.mockResolvedValue("deny");
    dialogSpy.warning.mockClear();
    const instance = createSensitiveGate();

    await expect(
      instance.request(plugin("dl", "下载看门狗"), "modify-settings"),
    ).rejects.toThrow("被用户拒绝");
    expect(dialogSpy.warning).not.toHaveBeenCalled();
  });

  it("确认框被新对话框顶掉（resolve null）时按拒绝处理", async () => {
    dialogSpy.showDialogBox.mockResolvedValue(null);
    const instance = createSensitiveGate();

    await expect(
      instance.request(plugin("dl"), "install-content"),
    ).rejects.toThrow("被用户拒绝");
  });
});

describe("细节格式", () => {
  it("sensitiveDetail 非空包中文括号，空串整段消失", () => {
    expect(sensitiveDetail("1.21.1")).toBe("（1.21.1）");
    expect(sensitiveDetail("  ")).toBe("");
    expect(sensitiveDetail(undefined)).toBe("");
  });

  it("sensitiveFileName 两种路径分隔符都认", () => {
    expect(sensitiveFileName("C:\\mods\\sodium.jar")).toBe("sodium.jar");
    expect(sensitiveFileName("/home/u/.minecraft/mods/a.jar")).toBe("a.jar");
    expect(sensitiveFileName("no-path.jar")).toBe("no-path.jar");
    expect(sensitiveFileName("")).toBe("");
  });
});
