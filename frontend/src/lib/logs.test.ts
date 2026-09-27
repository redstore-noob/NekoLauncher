/*
 * lib/logs.ts 的日志级别解析测试：覆盖两种日志格式与容易解析失败的边角
 * （ANSI 转义、级别不在第二个方括号、堆栈行兜底）。
 */
import { describe, expect, it } from "vitest";

import { logLineClass, parseLogLevel } from "./logs";

describe("parseLogLevel", () => {
  it("Minecraft 格式：[线程/级别]", () => {
    expect(
      parseLogLevel(
        "[12:34:56] [Render thread/INFO] [minecraft/Minecraft]: hi",
      ),
    ).toBe("INFO");
    expect(parseLogLevel("[12:34:56] [main/WARN]: deprecated")).toBe("WARNING");
    expect(parseLogLevel("[12:34:56] [main/FATAL] [neko]: dead")).toBe("ERROR");
  });

  it("Minecraft 格式容忍线程名与级别两侧空白", () => {
    expect(parseLogLevel("[12:34:56] [Worker-Main-12 / INFO]: lazy")).toBe(
      "INFO",
    );
  });

  it("启动器格式：[时间][级别]", () => {
    expect(parseLogLevel("[12:00:00][INFO] 正在启动")).toBe("INFO");
    expect(parseLogLevel("[12:00:00][ERROR] 启动失败")).toBe("ERROR");
  });

  it("级别在前两个方括号中的任一个都能识别", () => {
    expect(parseLogLevel("[INFO][启动] xxx")).toBe("INFO");
    expect(parseLogLevel("[2026-09-25 12:00:00] [INFO] [main] xxx")).toBe(
      "INFO",
    );
  });

  it("混入 ANSI 转义码的行仍能解析", () => {
    expect(parseLogLevel("\x1b[32m[12:34:56] [main/INFO]\x1b[0m colored")).toBe(
      "INFO",
    );
    expect(parseLogLevel("\x1b[31m[12:00:00][ERROR]\x1b[0m boom")).toBe(
      "ERROR",
    );
  });

  it("普通文本返回 OTHER", () => {
    expect(parseLogLevel("just some output")).toBe("OTHER");
  });
});

describe("logLineClass", () => {
  it("有级别按级别着色", () => {
    expect(logLineClass("[12:00:00][ERROR] x")).toContain("text-red-600");
  });

  it("无级别的异常堆栈行按错误特征词兜底", () => {
    expect(logLineClass("java.lang.RuntimeException: boom")).toContain(
      "text-red-600",
    );
    expect(logLineClass("\tat java.base/java.lang.Thread.run")).not.toContain(
      "text-red-600",
    );
  });
});
