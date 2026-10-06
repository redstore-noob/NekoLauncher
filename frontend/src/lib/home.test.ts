/*
 * lib/home.ts 的纯函数测试（P3-6 前端测试起步）。
 *
 * 这里挑的都是"看起来很简单、但一改就容易错"的格式化与解析：
 * 时间口径、字节/内存单位、MOTD 颜色码、小组件列布局的切分与插入下标。
 * 它们被主页、外观设置、服务器卡片多处复用，错了会同时影响好几个页面。
 */
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";

import {
  accountTypeLabel,
  columnInsertionIndex,
  errorMessage,
  formatBytes,
  formatMemoryMb,
  formatPlaytime,
  formatRelativeTime,
  formatRelativeTimeFrom,
  localFileUrl,
  migrateWidgetIds,
  parseWidgetColumns,
  parseWidgetLayout,
  renderMinecraftFormatting,
  resolveInitialVersion,
  splitWidgetColumns,
  stripMinecraftFormatting,
} from "./home";

describe("formatPlaytime", () => {
  it("零/负数按 0 分钟处理", () => {
    expect(formatPlaytime(0)).toBe("0 分钟");
    expect(formatPlaytime(-10)).toBe("0 分钟");
  });

  it("不足一分钟也至少显示 1 分钟（避免出现 0 分钟）", () => {
    expect(formatPlaytime(30)).toBe("1 分钟");
  });

  it("按四舍五入取分钟，超过一小时改用小时并保留一位小数", () => {
    expect(formatPlaytime(90)).toBe("2 分钟");
    expect(formatPlaytime(3599)).toBe("60 分钟");
    expect(formatPlaytime(3600)).toBe("1.0 小时");
    expect(formatPlaytime(5400)).toBe("1.5 小时");
  });
});

describe("formatRelativeTime", () => {
  it("没有记录时回落「从未游玩」", () => {
    expect(formatRelativeTime(0)).toBe("从未游玩");
    expect(formatRelativeTime(-1)).toBe("从未游玩");
  });

  it("按分钟 / 小时 / 天分档", () => {
    const now = Math.floor(Date.now() / 1000);

    expect(formatRelativeTime(now)).toBe("刚刚");
    expect(formatRelativeTime(now - 120)).toBe("2 分钟前");
    expect(formatRelativeTime(now - 3 * 3600)).toBe("3 小时前");
    expect(formatRelativeTime(now - 2 * 86400)).toBe("2 天前");
  });

  it("超过 30 天改用本地日期", () => {
    const timestamp = Math.floor(Date.now() / 1000) - 60 * 86400;

    expect(formatRelativeTime(timestamp)).toBe(
      new Date(timestamp * 1000).toLocaleDateString(),
    );
  });
});

describe("formatRelativeTimeFrom", () => {
  it("接受 Date 与 ISO 字符串", () => {
    const recent = new Date(Date.now() - 120 * 1000);

    expect(formatRelativeTimeFrom(recent)).toBe("2 分钟前");
    expect(formatRelativeTimeFrom(recent.toISOString())).toBe("2 分钟前");
  });

  it("Go 零值时间（0001-01-01）视为没有记录", () => {
    expect(formatRelativeTimeFrom("0001-01-01T00:00:00Z")).toBe("时间未知");
  });

  it("空值/非法值回落传入的兜底文案", () => {
    expect(formatRelativeTimeFrom(null)).toBe("时间未知");
    expect(formatRelativeTimeFrom("")).toBe("时间未知");
    expect(formatRelativeTimeFrom("not-a-date", "无记录")).toBe("无记录");
    expect(formatRelativeTimeFrom(undefined, "无记录")).toBe("无记录");
  });
});

describe("stripMinecraftFormatting", () => {
  it("去掉 § 样式码，保留原文", () => {
    expect(stripMinecraftFormatting("§aHello §lWorld§r")).toBe("Hello World");
  });

  it("空值不抛异常", () => {
    expect(stripMinecraftFormatting(undefined as unknown as string)).toBe("");
  });
});

describe("renderMinecraftFormatting", () => {
  const render = (text: string) =>
    renderToStaticMarkup(renderMinecraftFormatting(text));

  it("纯文本渲染为一个 span", () => {
    const html = render("你好");

    expect(html).toBe("<span>你好</span>");
  });

  it("颜色码生成对应颜色，装饰码生成对应样式", () => {
    const html = render("§a绿§l粗");

    expect(html).toContain("color:#55FF55");
    expect(html).toContain(">绿<");
    expect(html).toContain("font-weight:700");
    expect(html).toContain(">粗<");
  });

  it("颜色码按原版语义重置装饰样式", () => {
    const html = render("§l粗§c红");

    // §c 之后不该再是粗体：红色那一段单独成 span 且没有 font-weight
    expect(html).toContain("color:#FF5555");
    expect(html.match(/font-weight:700/g)?.length).toBe(1);
  });

  it("§r 重置全部样式", () => {
    const html = render("§a绿§r普通");

    expect(html.endsWith("<span>普通</span>")).toBe(true);
  });

  it("hex 色序列（§x§r§r§g§g§b§b）解析为 #rrggbb", () => {
    const html = render("§x§f§f§0§0§0§0红");

    expect(html).toContain("color:#ff0000");
    expect(html).toContain(">红<");
  });

  it("不完整的 hex 序列按未知码丢弃，不吞掉后面的文本", () => {
    const html = render("§x§f§f之后");

    expect(html).toContain("之后");
    expect(html).not.toContain("§");
  });

  it("§k 用混淆样式类，换行原样保留", () => {
    const html = render("§k乱码\n第二行");

    expect(html).toContain("nya-mc-obfuscated");
    expect(html).toContain("\n第二行");
  });

  it("未知样式码只丢掉 § 本身，后面的字符照常显示（与原版行为一致）", () => {
    const html = render("前§z后");

    expect(html).toBe("<span>前z后</span>");
  });
});

describe("localFileUrl", () => {
  it("拼成 /localfile 并转义路径", () => {
    expect(localFileUrl("C:\\我的 文件\\a.png")).toBe(
      `/localfile?path=${encodeURIComponent("C:\\我的 文件\\a.png")}`,
    );
  });

  it("空路径返回空串", () => {
    expect(localFileUrl("")).toBe("");
  });
});

describe("小组件列数", () => {
  it("只接受 1~4 的整数", () => {
    expect(parseWidgetColumns("1")).toBe(1);
    expect(parseWidgetColumns("3")).toBe(3);
    expect(parseWidgetColumns("4")).toBe(4);
    expect(parseWidgetColumns("0")).toBeNull();
    expect(parseWidgetColumns("5")).toBeNull();
    expect(parseWidgetColumns("1.5")).toBeNull();
    expect(parseWidgetColumns("abc")).toBeNull();
    expect(parseWidgetColumns("")).toBeNull();
  });
});

describe("splitWidgetColumns", () => {
  const ids = ["a", "b", "c", "d", "e"];

  it("1 列时原样返回（保持既有布局语义）", () => {
    expect(splitWidgetColumns(ids, 1)).toEqual([ids]);
  });

  it("多列时按列主序尽量均衡，余数分给前面的列", () => {
    expect(splitWidgetColumns(ids, 2)).toEqual([
      ["a", "b", "c"],
      ["d", "e"],
    ]);
    expect(splitWidgetColumns(["a", "b"], 3)).toEqual([["a"], ["b"], []]);
  });

  it("非法列数收敛到 1~4", () => {
    expect(splitWidgetColumns(ids, 0)).toEqual([ids]);
    expect(splitWidgetColumns(ids, 4)).toEqual([
      ["a", "b"],
      ["c"],
      ["d"],
      ["e"],
    ]);
    expect(splitWidgetColumns(ids, 99)).toEqual([
      ["a", "b"],
      ["c"],
      ["d"],
      ["e"],
    ]);
  });
});

describe("columnInsertionIndex", () => {
  const slices = [["a", "b"], ["c"], ["d", "e", "f"]];

  it("换算成平铺数组下标", () => {
    expect(columnInsertionIndex(slices, 0, 0)).toBe(0);
    expect(columnInsertionIndex(slices, 1, 0)).toBe(2);
    expect(columnInsertionIndex(slices, 2, 1)).toBe(4);
  });

  it("越界的列与位置收敛到合法范围（拖动落点可能来自旧的一帧）", () => {
    expect(columnInsertionIndex(slices, 9, 99)).toBe(6);
    expect(columnInsertionIndex(slices, -3, -5)).toBe(0);
  });

  it("空布局返回 0", () => {
    expect(columnInsertionIndex([], 0, 0)).toBe(0);
  });
});

describe("migrateWidgetIds", () => {
  it("把已下线的 motd 迁到 quickjoin", () => {
    expect(migrateWidgetIds(["motd", "playtime"])).toEqual([
      "quickjoin",
      "playtime",
    ]);
  });

  it("迁移产生的重复位只保留一个，未知 id 原样保留", () => {
    expect(migrateWidgetIds(["motd", "quickjoin", "plugin:x"])).toEqual([
      "quickjoin",
      "plugin:x",
    ]);
  });
});

describe("parseWidgetLayout", () => {
  it("解析字符串数组并丢掉非字符串项", () => {
    expect(parseWidgetLayout('["a",1,null,"b"]')).toEqual(["a", "b"]);
  });

  it("缺失/非法/非数组都返回 null 交给调用方回落默认布局", () => {
    expect(parseWidgetLayout("")).toBeNull();
    expect(parseWidgetLayout("{")).toBeNull();
    expect(parseWidgetLayout('{"a":1}')).toBeNull();
  });
});

describe("单位格式化", () => {
  it("内存：小于 1 GB 用 MB，否则 GB 保留一位小数", () => {
    expect(formatMemoryMb(0)).toBe("0 MB");
    expect(formatMemoryMb(-1)).toBe("0 MB");
    expect(formatMemoryMb(512)).toBe("512 MB");
    expect(formatMemoryMb(2048)).toBe("2.0 GB");
    expect(formatMemoryMb(Number.NaN)).toBe("0 MB");
  });

  it("字节：按 1024 进制进位到 TB 封顶", () => {
    expect(formatBytes(undefined)).toBe("0 B");
    expect(formatBytes(0)).toBe("0 B");
    expect(formatBytes(512)).toBe("512.0 B");
    expect(formatBytes(1536)).toBe("1.5 KB");
    expect(formatBytes(1024 * 1024 * 1024)).toBe("1.0 GB");
    expect(formatBytes(1024 ** 5)).toBe("1024.0 TB");
  });
});

describe("accountTypeLabel", () => {
  it("已知类型给出中文标签，未知回落「第三方」", () => {
    expect(accountTypeLabel("microsoft")).toBe("正版");
    expect(accountTypeLabel("offline")).toBe("离线");
    expect(accountTypeLabel("authlib")).toBe("皮肤站");
    expect(accountTypeLabel("whatever")).toBe("第三方");
  });
});

describe("errorMessage", () => {
  it("兼容 Error / 字符串 / 其它类型", () => {
    expect(errorMessage(new Error("炸了"))).toBe("炸了");
    expect(errorMessage("纯字符串")).toBe("纯字符串");
    expect(errorMessage(42)).toBe("42");
    expect(errorMessage(null)).toBe("");
    expect(errorMessage(undefined)).toBe("");
  });
});

describe("resolveInitialVersion", () => {
  it("会话内已选中的版本优先，刷新列表不被重置", () => {
    expect(
      resolveInitialVersion(["1.20.1", "1.19.4"], "1.19.4", "1.20.1"),
    ).toBe("1.19.4");
  });

  it("首次进入恢复上次持久化的选中版本", () => {
    expect(resolveInitialVersion(["a", "b", "c"], "", "b")).toBe("b");
  });

  it("持久化版本已被删除 / 换了目录时回落首个", () => {
    expect(resolveInitialVersion(["a", "b"], "", "gone")).toBe("a");
  });

  it("匹配版本 id 忽略大小写（Windows 目录不区分大小写）", () => {
    expect(resolveInitialVersion(["Fabric 1.20.1"], "", "fabric 1.20.1")).toBe(
      "Fabric 1.20.1",
    );
  });

  it("没有任何版本时返回空串", () => {
    expect(resolveInitialVersion([], "", "x")).toBe("");
  });
});
