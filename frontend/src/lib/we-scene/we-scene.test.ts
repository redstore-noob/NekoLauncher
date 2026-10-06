/**
 * we-scene 模块纯逻辑单测:值解析/用户属性求值/效果键归一/纹理候选/脚本沙箱/粒子。
 * 渲染器本身依赖 WebGL,不做 DOM 级测试(由真机集成验证覆盖)。
 */

import type { WEParticleFile } from "./types";

import { describe, expect, it } from "vitest";

import { textureCandidates } from "./assets";
import { effectKeyFromFile, resolveEffectKey } from "./effects";
import { ParticleSystem } from "./particles";
import { compileTextScript } from "./scripts";
import {
  colorToCss,
  constantNumber,
  constantVec2,
  parseColor,
  parseVec2,
  resolveBound,
  resolveVisible,
} from "./values";

describe("values: 向量与颜色解析", () => {
  it("parseVec2 解析 WE 的空格分隔字符串", () => {
    expect(parseVec2("1280.5 720", [0, 0])).toEqual([1280.5, 720]);
    expect(parseVec2(undefined, [960, 540])).toEqual([960, 540]);
    expect(parseVec2("bad input", [1, 2])).toEqual([1, 2]);
  });

  it("parseColor 支持 rgb 与 rgba(0-1)", () => {
    expect(parseColor("1 0 0")).toEqual([1, 0, 0, 1]);
    expect(parseColor("0.5 0.5 0.5 0.25")).toEqual([0.5, 0.5, 0.5, 0.25]);
    expect(colorToCss("1 0.5 0")).toBe("rgb(255, 128, 0)");
  });

  it("constantNumber/Vec2 容忍字符串向量", () => {
    expect(constantNumber(5, 0)).toBe(5);
    expect(constantNumber("0.25 0.5", 1)).toBe(0.25);
    expect(constantNumber(undefined, 3)).toBe(3);
    expect(constantVec2("1 2", [0, 0])).toEqual([1, 2]);
  });
});

describe("values: 用户属性绑定", () => {
  const properties = {
    schemecolor: { type: "color", value: "0.2 0.4 0.9" },
    showchar: { type: "bool", value: false },
    stars: { type: "slider", value: 42 },
    mode: { type: "combo", value: "2" },
  };

  it("resolveBound 解裸值与绑定值", () => {
    expect(resolveBound(0.7, properties)).toBe(0.7);
    expect(
      resolveBound({ user: "schemecolor", value: "1 1 1" }, properties),
    ).toBe("0.2 0.4 0.9");
    // 绑定的属性不存在 → 回落默认值
    expect(resolveBound({ user: "nosuch", value: 9 }, properties)).toBe(9);
    expect(resolveBound(undefined, properties)).toBeUndefined();
  });

  it("resolveVisible 支持布尔绑定与组合框条件", () => {
    expect(resolveVisible(undefined, properties)).toBe(true);
    expect(resolveVisible({ user: "showchar", value: true }, properties)).toBe(
      false,
    );
    expect(
      resolveVisible(
        { user: { condition: "2", name: "mode" }, value: false },
        properties,
      ),
    ).toBe(true);
    expect(
      resolveVisible(
        { user: { condition: "1", name: "mode" }, value: false },
        properties,
      ),
    ).toBe(false);
  });
});

describe("effects: 效果键归一", () => {
  it("从 effect.json 路径提取效果名", () => {
    expect(effectKeyFromFile("effects/waterwaves/effect.json")).toBe(
      "waterwaves",
    );
    expect(effectKeyFromFile("effects/shake/effect.json")).toBe("shake");
    // workshop 效果取末段目录名
    expect(
      effectKeyFromFile("effects/workshop/2718465779/pulse_/effect.json"),
    ).toBe("pulse_");
  });

  it("别名与未知效果归一到实现键", () => {
    expect(
      resolveEffectKey("effects/workshop/2718465779/pulse_/effect.json"),
    ).toBe("pulse");
    expect(resolveEffectKey("effects/blurprecise/effect.json")).toBe("blur");
    // 音频条这类强交互效果明确退化为 noop(图层照常渲染)
    expect(
      resolveEffectKey(
        "effects/workshop/3373140814/Simple_Audio_Bars/effect.json",
      ),
    ).toBe("noop");
    expect(resolveEffectKey("effects/完全未知/effect.json")).toBe("noop");
  });
});

describe("assets: 纹理候选路径", () => {
  it("裸名字优先 materials/ 前缀 + .tex 后缀", () => {
    const candidates = textureCandidates("1760560458885");

    expect(candidates[0]).toBe("materials/1760560458885.tex");
    expect(candidates).toContain("1760560458885.tex");
  });

  it("带目录的 mask 引用补 .tex 后缀,并尝试 materials/ 前缀", () => {
    const candidates = textureCandidates("masks/waterwaves_mask_c94b3ee2");

    expect(candidates[0]).toBe("masks/waterwaves_mask_c94b3ee2.tex");
    // WE 的 mask 实际存放于 materials/masks/ 下
    expect(candidates).toContain(
      "materials/masks/waterwaves_mask_c94b3ee2.tex",
    );
    // workshop 子目录的粒子贴图同理
    const particle = textureCandidates("workshop/2427252388/particle/Untitled");

    expect(particle).toContain(
      "materials/workshop/2427252388/particle/Untitled.tex",
    );
  });

  it("已是 .tex 的引用原样保留", () => {
    expect(textureCandidates("materials/bg.tex")[0]).toBe("materials/bg.tex");
  });

  it("反斜杠归一为斜杠", () => {
    expect(textureCandidates("materials\\bg.tex")[0]).toBe("materials/bg.tex");
  });
});

describe("scripts: 文本脚本沙箱", () => {
  it("剥离 export 前缀并调用 update", () => {
    const host = { text: "init", userProperties: { name: "neko" } };
    const script = compileTextScript(
      `var count = 0;
       export function update() {
         count++;
         thisLayer.text = 'tick' + count + ':' + engine.userProperties.name;
       }`,
      host,
    );

    expect(script).not.toBeNull();
    script!.update();
    script!.update();
    expect(host.text).toBe("tick2:neko");
  });

  it("无脚本/编译失败返回 null", () => {
    expect(
      compileTextScript(undefined, { text: "", userProperties: {} }),
    ).toBeNull();
    expect(
      compileTextScript("function {{{broken", { text: "", userProperties: {} }),
    ).toBeNull();
  });
});

describe("particles: 粒子系统", () => {
  const definition: WEParticleFile = {
    material: "materials/particle/leaf.json",
    maxcount: 4,
    starttime: 0,
    emitter: [
      {
        name: "sphererandom",
        origin: "0 0 0",
        rate: 10,
        distancemin: 0,
        distancemax: 100,
      },
    ],
    initializer: [
      { name: "lifetimerandom", min: 1, max: 2 },
      { name: "sizerandom", min: 10, max: 20 },
      { name: "velocityrandom", min: "0 -100 0", max: "0 -200 0" },
      { name: "colorrandom", min: "255 255 255" },
    ],
    operator: [
      { name: "movement", gravity: "0 100 0" },
      { name: "alphafade", fadeintime: 0.1, fadeouttime: 0.5 },
      { name: "angularmovement" },
    ],
    renderer: [{ name: "sprite" }],
  };

  it("构造与推进不依赖 WebGL 上下文", () => {
    // three 的 Buffer/材质对象在无渲染器时也可构造
    const fakeTexture = { image: { width: 8, height: 8 } } as never;
    const system = new ParticleSystem(definition, fakeTexture);

    expect(system.mesh).toBeDefined();
    // 两秒推进:发射 + 重力 + 死亡回收,不该抛错
    for (let i = 0; i < 60; i++) system.update(1 / 30, i / 30, [960, 540]);
    system.dispose();
  });

  it("starttime 之前不发射", () => {
    const fakeTexture = { image: { width: 8, height: 8 } } as never;
    const system = new ParticleSystem(
      { ...definition, starttime: 10 },
      fakeTexture,
    );

    system.update(1, 0, [0, 0]);
    system.dispose();
  });
});
