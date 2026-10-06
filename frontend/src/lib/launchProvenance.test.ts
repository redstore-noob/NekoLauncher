/*
 * lib/launchProvenance.ts 的测试。
 *
 * 这个模块的价值全在「后端给什么标识，界面就得说出什么话」，所以测三件事：
 *   1. 已知 Key 能翻成带 Detail 插值的完整说明（后端契约的主路径）；
 *   2. **未知 Key / 未知 Kind 优雅降级**（显示原始 key，绝不抛异常）——
 *      后端加新 Key 是常态，界面崩掉是不可接受的；
 *   3. 分组、冲突描述、长参数截断这些纯排版决策。
 *
 * 测试环境语言固定为 zh-CN（原文即译文，见 test-setup.ts），
 * 所以断言里的中文就是源码里的模板原文，稳定可读。
 */
import type { launch } from "../../wailsjs/go/models";

import { describe, expect, it } from "vitest";

import { translate } from "../i18n";

import {
  describeConflict,
  describeKind,
  describeSection,
  describeSource,
  describeSourceWithPlugin,
  entriesForView,
  groupBySection,
  isLongArgument,
  summarizeReport,
  truncateArgument,
} from "./launchProvenance";

/** 造一条来源，只填关心的字段。 */
function source(
  over: Partial<{
    Kind: string;
    Key: string;
    Detail: string;
    PluginID: string;
  }> = {},
) {
  return {
    Kind: "launcher-auto",
    Key: "",
    Detail: "",
    PluginID: "",
    ...over,
  };
}

/**
 * 造一条条目。
 * 返回值断言成 launch.LaunchArgumentEntry：生成的模型类带 convertValues 方法，
 * 测试里不需要，构造出结构等价的普通对象即可。
 */
function entry(
  over: Partial<{
    Index: number;
    Argument: string;
    Section: string;
    Shadowed: boolean;
    Source: ReturnType<typeof source>;
  }> = {},
) {
  return {
    Index: 0,
    Argument: "-Xmx4096M",
    Section: "jvm",
    Shadowed: false,
    Source: source(),
    ...over,
  } as unknown as launch.LaunchArgumentEntry;
}

describe("describeSource：已知 Key", () => {
  it("带 Detail 的模板会做插值", () => {
    expect(
      describeSource(
        source({
          Kind: "instance-settings",
          Key: "memory.max.instance",
          Detail: "4096",
        }),
      ),
    ).toBe("实例独立设置的最大堆（4096 MiB）");
  });

  it("Detail 为空时不留空括号", () => {
    const text = describeSource(
      source({ Kind: "global-settings", Key: "memory.max.global", Detail: "" }),
    );

    expect(text).toBe("全局设置的最大堆");
    expect(text).not.toContain("{0}");
    expect(text).not.toContain("（）");
  });

  it("四种内存来源互不相同（这是用户最常问的那个『为什么』）", () => {
    const keys = [
      "memory.min.auto",
      "memory.max.instance",
      "memory.max.global",
      "memory.max.automatic",
    ];
    const texts = keys.map((Key) =>
      describeSource(source({ Key, Detail: "4096" })),
    );

    expect(new Set(texts).size).toBe(4);
    for (const text of texts) expect(text).toContain("4096");
  });

  it("任务书列出的每个 Key 都有对应文案（后端契约）", () => {
    const keys = [
      "memory.min.auto",
      "memory.max.instance",
      "memory.max.global",
      "memory.max.automatic",
      "jvm.tuning.g1",
      "jvm.user.instance",
      "jvm.user.global",
      "jvm.library-directory",
      "jvm.classpath",
      "version-json.logging",
      "version-json.jvm",
      "version-json.game",
      "version-json.main-class",
      "version-json.classpath-legacy",
      "game.user.instance",
      "game.user.global",
      "plugin.prepend-jvm",
      "plugin.append-jvm",
      "plugin.prepend-game",
      "plugin.append-game",
    ];

    for (const Key of keys) {
      const text = describeSource(source({ Key, Detail: "4096" }));

      // 有文案 = 不再回显原始 key
      expect(text, `Key「${Key}」没有对应文案`).not.toBe(Key);
      expect(text.length).toBeGreaterThan(0);
    }
  });
});

describe("describeSource：优雅降级", () => {
  it("未知 Key 原样显示，不抛异常", () => {
    expect(
      describeSource(source({ Kind: "plugin", Key: "plugin.some-future-key" })),
    ).toBe("plugin.some-future-key");
  });

  it("未知 Kind 且无 Key 时回退到 Kind 原文", () => {
    expect(describeSource(source({ Kind: "brand-new-kind", Key: "" }))).toBe(
      "brand-new-kind",
    );
  });

  it("全空的来源只报『来源未知』，不崩", () => {
    expect(describeSource(source({ Kind: "", Key: "" }))).toBe("来源未知");
  });

  it("缺字段（后端老版本/字段被裁）也安全", () => {
    expect(() =>
      describeSource({} as Parameters<typeof describeSource>[0]),
    ).not.toThrow();
    expect(describeSource({} as Parameters<typeof describeSource>[0])).toBe(
      "来源未知",
    );
  });
});

describe("describeKind", () => {
  it("十个 Kind 都有中文标签", () => {
    const kinds = [
      "version-json",
      "launcher-auto",
      "global-settings",
      "instance-settings",
      "instance-or-global",
      "direct-connect",
      "authlib-injector",
      "plugin",
      "transform",
      "unknown",
    ];

    for (const kind of kinds) {
      const label = describeKind(kind);

      expect(label, `Kind「${kind}」没有标签`).not.toBe(kind);
      expect(label.length).toBeGreaterThan(0);
    }
  });

  it("未知 Kind 原样回显", () => {
    expect(describeKind("future-kind")).toBe("future-kind");
  });
});

describe("describeSourceWithPlugin", () => {
  it("插件来源带上插件 id", () => {
    const text = describeSourceWithPlugin(
      source({
        Kind: "plugin",
        Key: "plugin.append-jvm",
        PluginID: "my-plugin",
      }),
    );

    expect(text).toContain("插件追加的 JVM 参数");
    expect(text).toContain("my-plugin");
  });

  it("非插件来源不带 id（PluginID 为空）", () => {
    expect(
      describeSourceWithPlugin(
        source({ Kind: "launcher-auto", Key: "jvm.classpath" }),
      ),
    ).toBe("启动器拼出的类路径");
  });
});

describe("describeSection", () => {
  it("三个区段都有标题，未知区段原样回显", () => {
    expect(describeSection("jvm")).toBe("JVM 参数");
    expect(describeSection("main-class")).toBe("主类");
    expect(describeSection("game")).toBe("游戏参数");
    expect(describeSection("future-section")).toBe("future-section");
  });
});

describe("groupBySection", () => {
  it("按 JVM → 主类 → 游戏顺序分组，组内保持命令行顺序", () => {
    const groups = groupBySection([
      entry({ Index: 0, Section: "jvm", Argument: "-Xms512M" }),
      entry({ Index: 1, Section: "jvm", Argument: "-Xmx4096M" }),
      entry({
        Index: 2,
        Section: "main-class",
        Argument: "net.minecraft.Main",
      }),
      entry({ Index: 3, Section: "game", Argument: "--username" }),
    ]);

    expect(groups.map((g) => g.section)).toEqual(["jvm", "main-class", "game"]);
    expect(groups[0].entries.map((e) => e.Argument)).toEqual([
      "-Xms512M",
      "-Xmx4096M",
    ]);
    expect(groups[1].entries).toHaveLength(1);
    expect(groups[2].entries).toHaveLength(1);
  });

  it("后端将来新增的区段排在已知区段之后，不丢失", () => {
    const groups = groupBySection([
      entry({ Section: "future", Argument: "-X" }),
      entry({ Section: "jvm", Argument: "-Xms512M" }),
    ]);

    expect(groups.map((g) => g.section)).toEqual(["jvm", "future"]);
    expect(groups[1].label).toBe("future");
  });

  it("空/缺失输入返回空数组，不抛异常", () => {
    expect(groupBySection([])).toEqual([]);
    expect(groupBySection(null)).toEqual([]);
    expect(groupBySection(undefined)).toEqual([]);
  });

  it("缺 Section 字段的条目落到 unknown 组而不是消失", () => {
    const groups = groupBySection([
      entry({ Argument: "-X", Section: undefined as unknown as string }),
    ]);

    expect(groups).toHaveLength(1);
    expect(groups[0].section).toBe("unknown");
    expect(groups[0].entries[0].Argument).toBe("-X");
  });
});

describe("summarizeReport", () => {
  it("统计冲突数、被覆盖数与生效数", () => {
    const summary = summarizeReport({
      Entries: [entry({ Index: 0 }), entry({ Index: 1 })],
      Effective: [entry({ Index: 1 })],
      Overridden: [entry({ Index: 0, Shadowed: true })],
      Conflicts: [
        {
          Prefix: "-Xmx",
          WinnerIndex: 1,
          LoserIndices: [0],
          WinnerSource: source(),
          LoserSource: source(),
        },
      ],
    } as Parameters<typeof summarizeReport>[0]);

    expect(summary).toEqual({
      conflictCount: 1,
      overriddenCount: 1,
      effectiveCount: 1,
      totalCount: 2,
    });
  });

  it("null 报告给出全零摘要（面板据此走空态）", () => {
    expect(summarizeReport(null)).toEqual({
      conflictCount: 0,
      overriddenCount: 0,
      effectiveCount: 0,
      totalCount: 0,
    });
  });

  it("缺 Overridden 视图时按 Shadowed 现算", () => {
    const summary = summarizeReport({
      Entries: [
        entry({ Index: 0, Shadowed: true }),
        entry({ Index: 1, Shadowed: false }),
      ],
      Effective: [],
      Conflicts: [],
    } as unknown as Parameters<typeof summarizeReport>[0]);

    expect(summary.overriddenCount).toBe(1);
  });
});

describe("describeConflict", () => {
  it("说清出现几次、谁生效、谁被忽略", () => {
    const text = describeConflict({
      Prefix: "-Xmx",
      WinnerIndex: 2,
      LoserIndices: [0, 1],
      WinnerSource: source({
        Kind: "instance-settings",
        Key: "memory.max.instance",
        Detail: "4096",
      }),
      LoserSource: source({
        Kind: "global-settings",
        Key: "memory.max.global",
        Detail: "2048",
      }),
    } as Parameters<typeof describeConflict>[0]);

    expect(text).toContain("-Xmx");
    expect(text).toContain("3 次"); // 1 个赢家 + 2 个输家
    expect(text).toContain("实例独立设置的最大堆（4096 MiB）");
    expect(text).toContain("全局设置的最大堆（2048 MiB）");
    expect(text).not.toContain("{0}");
    expect(text).not.toContain("{3}");
  });

  it("缺字段的冲突不抛异常", () => {
    expect(() =>
      describeConflict({} as Parameters<typeof describeConflict>[0]),
    ).not.toThrow();
  });
});

describe("entriesForView", () => {
  const report = {
    Entries: [entry({ Index: 0 }), entry({ Index: 1 })],
    Effective: [entry({ Index: 1 })],
  } as Parameters<typeof entriesForView>[0];

  it("showAll=true 用 Entries，false 用 Effective", () => {
    expect(entriesForView(report, true)).toHaveLength(2);
    expect(entriesForView(report, false)).toHaveLength(1);
  });

  it("null 报告返回空数组", () => {
    expect(entriesForView(null, true)).toEqual([]);
    expect(entriesForView(null, false)).toEqual([]);
  });
});

describe("长参数截断", () => {
  it("短参数不截断", () => {
    const short = "-Xmx4096M";

    expect(isLongArgument(short)).toBe(false);
    expect(truncateArgument(short)).toBe(short);
  });

  it("超长类路径截断但保留尾部，且短于原文", () => {
    const long = `-cp ${"C:/Users/x/.minecraft/libraries/a/b/1.0/b-1.0.jar;".repeat(20)}`;

    expect(isLongArgument(long)).toBe(true);
    const short = truncateArgument(long);

    expect(short.length).toBeLessThan(long.length);
    expect(short.startsWith("…")).toBe(true);
    // 尾部是类路径里信息量最大的部分（最后那个 jar）
    expect(long.endsWith(short.slice(1))).toBe(true);
  });

  it("空串安全", () => {
    expect(isLongArgument("")).toBe(false);
    expect(truncateArgument("")).toBe("");
  });
});

describe("面板文案在所有语言都有译文（词典一致性）", () => {
  /**
   * 面板与 lib 用到的全部「中文原文」key。
   * 这些是 t() 的参数，漏一条就会在非中文界面回退成中文。
   * 与 crashNotice 的约定一致：文案本身就该进词典。
   */
  const PANEL_KEYS = [
    "启动参数溯源",
    "本次启动",
    "本次启动 · 共 {0} 条参数",
    "本次启动环境",
    "实例 / 版本",
    "Java 可执行文件",
    "工作目录",
    "正在读取启动参数…",
    "还没有成功启动过游戏，暂时没有可以回溯的参数",
    "启动参数溯源会在游戏成功启动后记录。先启动一次游戏，再回到这里查看每个参数是谁加的。",
    "这次启动没有记录到任何参数",
    "有 {0} 组参数被后面的同名参数覆盖",
    "共 {0} 条参数没有生效（列表中已划线标出）。JVM 取最后一次出现的值，被覆盖的那条不会起作用。",
    "没有参数互相覆盖，{0} 条参数全部生效。",
    "显示全部参数",
    "显示全部参数（含被覆盖的）",
    "只显示生效的参数",
    "点击{0}完整参数",
    "点击展开完整参数",
    "点击收起",
    "「{0}」出现 {1} 次：来自「{2}」的那条生效，来自「{3}」的被忽略。",
    "{0} / {1} 条",
    "{0} 条",
    "已被覆盖",
    "压过了「{0}」",
    "互相覆盖的参数",
    "JVM 参数",
    "游戏参数",
    "其他参数",
    "交给 Java 虚拟机，在游戏启动前生效",
    "决定实际启动哪个入口类",
    "原样传给 Minecraft 本体",
    "版本文件自带",
    "启动器自动添加",
    "全局启动设置",
    "实例独立设置",
    "实例或全局设置",
    "直接进服",
    "外置登录注入",
    "插件添加",
    "启动变换",
    "来源未知",
    "启动器自动设置的最小堆（{0} MiB）",
    "实例独立设置的最大堆（{0} MiB）",
    "全局设置的最大堆（{0} MiB）",
    "启动器按系统内存自动计算的最大堆（{0} MiB）",
    "启动器内置的 G1 垃圾回收调优参数",
    "实例的额外 JVM 参数",
    "全局的额外 JVM 参数",
    "Forge / NeoForge 需要的库目录声明",
    "启动器拼出的类路径",
    "版本文件声明的日志配置",
    "版本文件自带的 JVM 参数",
    "版本文件自带的游戏参数",
    "版本文件声明的主类",
    "旧版版本文件的库路径与类路径",
    "实例的额外游戏参数",
    "全局的额外游戏参数",
    "插件前置的 JVM 参数",
    "插件追加的 JVM 参数",
    "插件前置的游戏参数",
    "插件追加的游戏参数",
    // 实例详情页的溯源入口
    "启动参数是怎么来的？",
    "逐条列出上次启动时每个参数由谁添加，以及哪些被后面的同名参数覆盖了。",
  ];
  const LOCALES = [
    "zh-TW",
    "zh-HK",
    "en-US",
    "ja-JP",
    "ru-RU",
    "de-DE",
  ] as const;

  it("每个 key 在六种目标语言里都有译文（不回退成中文原文）", () => {
    const missing: string[] = [];

    for (const locale of LOCALES) {
      for (const key of PANEL_KEYS) {
        // translate 找不到就回退原文；译文等于原文即视为缺条目。
        // zh-CN 不在列表中：它的原文即译文，无需词典。
        if (translate(locale, key) === key)
          missing.push(`${locale} 缺「${key}」`);
      }
    }

    expect(missing).toEqual([]);
  });
});
