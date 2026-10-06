/**
 * WE 内置效果 → GLSL ES 移植。
 *
 * WE 的效果(shader)是对图层纹理的后处理:位移类(shake/waterwaves/…)
 * 在采样时偏移 UV,颜色类(tint/opacity/pulse/…)调整 RGBA。这里把每个
 * 效果做成一个"全屏直通 pass":输入上一级纹理,输出到下一级渲染目标,
 * 由渲染器(renderer.ts)按对象的效果链 ping-pong。
 *
 * 移植自壁纸包内自带的 .frag 源码(shaders/effects/*.frag),对
 *  - 只在 3D 模型上出现的 PERSPECTIVE 变体
 *  - 音频联动(AUDIOPROCESSING)
 *  - 多 pass 合成(godrays/shine/glitter 的 cast+combine)
 * 做了裁剪或近似;未见的效果退化为恒等 pass(图层照常显示,只是没有动效)。
 * 常量默认值取自着色器源码里的 {"default":...} 注解。
 */

import type { WEObjectEffect, WEProperty } from "./types";

import * as THREE from "three";

import {
  constantNumber,
  constantVec2,
  constantVec3,
  resolveBound,
  resolveShaderConstant,
} from "./values";
import { whiteTexture } from "./assets";

/** 公共顶点着色器:直接输出裁剪空间全屏四边形 */
const BLIT_VERTEX = /* glsl */ `
varying vec2 vUv;
void main() {
  vUv = uv;
  gl_Position = vec4(position.xy, 0.0, 1.0);
}
`;

/** 公共片段头:WE 语义的哈希噪声(替代包外的 util/noise 贴图)与工具函数 */
const GLSL_PRELUDE = /* glsl */ `
varying vec2 vUv;
uniform sampler2D uTex;
uniform sampler2D uMask;
uniform sampler2D uMask2;
uniform float uTime;
uniform vec2 uTexel; // 1/宽高

vec2 weRotate(vec2 v, float a) {
	float c = cos(a), s = sin(a);
	return vec2(v.x * c - v.y * s, v.x * s + v.y * c);
}
float weHash(vec2 p) {
	p = fract(p * vec2(443.897, 441.423));
	p += dot(p, p + 19.19);
	return fract((p.x + p.y) * p.x);
}
float weNoise(vec2 p) {
	vec2 i = floor(p), f = fract(p);
	f = f * f * (3.0 - 2.0 * f);
	float a = weHash(i), b = weHash(i + vec2(1.0, 0.0));
	float c = weHash(i + vec2(0.0, 1.0)), d = weHash(i + vec2(1.0, 1.0));
	return mix(mix(a, b, f.x), mix(c, d, f.x), f.y);
}
`;

/** 单个效果 pass 的可执行描述 */
export interface EffectPass {
  material: THREE.ShaderMaterial;
  /** 逐帧更新(time 秒) */
  update: (time: number) => void;
  dispose: () => void;
}

/** 效果构建上下文 */
export interface EffectContext {
  properties: Record<string, WEProperty>;
  loadTexture: (name: string) => Promise<THREE.Texture | null>;
  /** 图层自身的基础纹理(glitter combine 需要同时取原图与链上产物) */
  baseTexture?: THREE.Texture | null;
  /** 视频纹理之类的动态贴图由渲染器维护,这里只拿帧时间 */
  width: number;
  height: number;
}

const FULLSCREEN_QUAD = new THREE.PlaneGeometry(2, 2);

/** 效果名:effects/<name>/effect.json 的中间段;workshop 目录取末段 */
export function effectKeyFromFile(file: string): string {
  const parts = file
    .replace(/\\/g, "/")
    .replace(/^effects\//, "")
    .split("/");
  let key = parts[0] ?? "";

  if (key === "workshop") key = parts[parts.length - 2] ?? key;

  return key.toLowerCase().replace(/\.json$/, "");
}

/** 常见别名(workshop 效果按内置效果实现) */
const EFFECT_ALIASES: Record<string, string> = {
  pulse_: "pulse",
  blurprecise: "blur",
  simple_gradient_audio_bar: "noop",
  simple_audio_bars: "noop",
  audio_bars: "noop",
  gradient_color: "tint",
  gradientopacity: "opacity",
  color_grading: "tint",
  bloom: "noop",
  geometric_transform: "noop",
};

/** 已实现的效果集合;未知效果用恒等 pass */
const IMPLEMENTED = new Set([
  "shake",
  "waterwaves",
  "tint",
  "opacity",
  "scroll",
  "pulse",
  "filmgrain",
  "vhs",
  "blur",
  "foliagesway",
  "swing",
  "waterflow",
  "cloudmotion",
  "waterripple",
  "glitter",
  "shine",
  "iris",
  "lightshafts",
  "godrays",
]);

/** resolveEffectKey 归一化效果名到实现键。 */
export function resolveEffectKey(file: string): string {
  const key = effectKeyFromFile(file);
  const aliased = EFFECT_ALIASES[key] ?? key;

  return IMPLEMENTED.has(aliased) ? aliased : "noop";
}

/** 基础 uniforms(所有 pass 共有) */
function baseUniforms(): Record<string, THREE.IUniform> {
  return {
    uTex: { value: null },
    uMask: { value: whiteTexture() },
    uMask2: { value: whiteTexture() },
    uTime: { value: 0 },
    uTexel: { value: new THREE.Vector2(1 / 1920, 1 / 1080) },
  };
}

/** 把对象 pass 的常量表解析成 uniform 值(数字/向量都收)。
 * WE 材质名可能写作全名(ui_editor_properties_amount)或短名(amount),
 * 统一剥前缀后落键,两种写法都能命中。 */
function constantMap(
  effect: WEObjectEffect,
  properties: Record<string, WEProperty>,
): Record<string, unknown> {
  const out: Record<string, unknown> = {};

  for (const pass of effect.passes ?? []) {
    for (const [name, raw] of Object.entries(pass.constantshadervalues ?? {})) {
      const key = name.toLowerCase().replace(/^ui_editor_properties_/, "");

      out[key] = resolveShaderConstant(raw, properties);
    }
  }

  return out;
}

/** 效果槽位贴图(textures 数组,槽 0 是图层自身)异步填充到 uniforms。 */
async function attachSlotTextures(
  effect: WEObjectEffect,
  uniforms: Record<string, THREE.IUniform>,
  slotMap: Record<number, string>,
  context: EffectContext,
): Promise<void> {
  const textures = effect.passes?.[0]?.textures ?? [];
  const jobs: Promise<void>[] = [];

  for (const [index, uniformName] of Object.entries(slotMap)) {
    const name = textures[Number(index)];

    if (typeof name !== "string" || !name) continue;
    jobs.push(
      context.loadTexture(name).then((texture) => {
        if (texture) uniforms[uniformName].value = texture;
      }),
    );
  }
  await Promise.all(jobs);
}

/** 平坦法线(128,128,255):waterripple 缺法线贴图时零位移 */
function flatNormalTexture(): THREE.Texture {
  const data = new Uint8Array([128, 128, 255, 255]);
  const texture = new THREE.DataTexture(data, 1, 1);

  texture.needsUpdate = true;

  return texture;
}

/** glitter 两 pass:prepare(闪烁图案)+ combine(遮罩加色叠回原图)。
 * 遮罩在第二个 pass 的 textures 槽 2 上(WE glitter_combine 的约定)。 */
async function buildGlitterPasses(
  effect: WEObjectEffect,
  context: EffectContext,
  constants: Record<string, unknown>,
): Promise<EffectPass[]> {
  const c = (name: string, fallback: number) =>
    constantNumber(constants[name] ?? constants[`g_${name}`], fallback);
  const colorOf = (name: string, fallback: [number, number, number]) =>
    constantVec3(constants[name], fallback);

  const prepareUniforms = baseUniforms();

  prepareUniforms.uDensity = { value: c("density", 0.5) };
  prepareUniforms.uGSpeed = { value: c("speed", 1) };
  const combineUniforms = baseUniforms();

  combineUniforms.uGScale = { value: c("scale", 1) };
  combineUniforms.uGOpacity = { value: c("alpha", 1) };
  combineUniforms.uGColor = {
    value: new THREE.Vector3(...colorOf("color", [1, 1, 1])),
  };
  combineUniforms.uBase = { value: context.baseTexture ?? null };

  const prepare = makePass(FRAG_GLITTER_PREPARE, prepareUniforms);
  const combine = makePass(FRAG_GLITTER_COMBINE, combineUniforms);
  const maskName = effect.passes?.[1]?.textures?.[2];

  if (typeof maskName === "string" && maskName) {
    const texture = await context.loadTexture(maskName);

    if (texture) combineUniforms.uMask.value = texture;
  }

  return [prepare, combine];
}

/** 新建 pass 的公共骨架 */
function makePass(
  fragmentBody: string,
  uniforms: Record<string, THREE.IUniform>,
): EffectPass {
  const material = new THREE.ShaderMaterial({
    vertexShader: BLIT_VERTEX,
    fragmentShader: GLSL_PRELUDE + fragmentBody,
    uniforms,
    transparent: true,
    depthTest: false,
    depthWrite: false,
  });

  return {
    material,
    update: (time) => {
      material.uniforms.uTime.value = time;
    },
    dispose: () => {
      material.dispose();
    },
  };
}

// ---- 各效果的片段着色器(与 WE 源码同构,变量名对齐) ----

/* shake:flow mask 驱动的正弦位移 */
const FRAG_SHAKE = /* glsl */ `
uniform float uSpeed;
uniform float uStrength;
uniform vec2 uFriction;
void main() {
  vec2 flowColors = texture2D(uMask, vUv).rg;
  vec2 flowMask = (flowColors - vec2(0.498, 0.498)) * 2.0;
  float time = uSpeed * uTime;
  float offset = sin(fract(time / 1.57079632679) * 1.57079632679);
  offset = offset * 0.498 + 0.5;
  float base = step(0.0, cos(time));
  offset = mix(1.0 - pow(1.0 - offset, uFriction.x), pow(offset, uFriction.y), base);
  offset = offset * 2.0 - 1.0;
  vec2 texCoordOffset = offset * uStrength * uStrength * flowMask;
  gl_FragColor = texture2D(uTex, texCoordOffset + vUv);
}
`;

/* waterwaves:方向波位移,mask 控制幅度 */
const FRAG_WATERWAVES = /* glsl */ `
uniform float uSpeed;
uniform float uScale;
uniform float uExponent;
uniform float uStrength;
uniform float uDirection;
void main() {
  float mask = texture2D(uMask, vUv).r;
  vec2 dir = vec2(-sin(uDirection), cos(uDirection));
  float distance = uTime * uSpeed + dot(vUv, dir) * uScale;
  float val1 = sin(distance);
  float s1 = sign(val1);
  val1 = pow(abs(val1), uExponent);
  vec2 offset = vec2(dir.y, -dir.x);
  vec2 texCoord = vUv + val1 * s1 * offset * (uStrength * uStrength) * mask;
  gl_FragColor = texture2D(uTex, texCoord);
}
`;

/* tint:按 mask 把颜色往 tintColor 混 */
const FRAG_TINT = /* glsl */ `
uniform vec3 uTintColor;
uniform float uBlendAlpha;
void main() {
  vec4 albedo = texture2D(uTex, vUv);
  float mask = texture2D(uMask, vUv).r * uBlendAlpha;
  albedo.rgb = mix(albedo.rgb, uTintColor, mask);
  gl_FragColor = albedo;
}
`;

/* opacity:mask × alpha */
const FRAG_OPACITY = /* glsl */ `
uniform float uAlpha;
void main() {
  vec4 albedo = texture2D(uTex, vUv);
  float mask = texture2D(uMask, vUv).r;
  albedo.a *= mask * uAlpha;
  gl_FragColor = albedo;
}
`;

/* scroll:UV 平移 + 平铺 */
const FRAG_SCROLL = /* glsl */ `
uniform vec2 uScroll;   // sign(s)*s^2
uniform vec2 uRepeat;
void main() {
  vec2 texCoord = fract((vUv + uScroll * uTime) * uRepeat);
  gl_FragColor = texture2D(uTex, texCoord);
}
`;

/* pulse:呼吸透明度/颜色 */
const FRAG_PULSE = /* glsl */ `
uniform float uPulseSpeed;
uniform float uPulseAmount;
uniform float uPower;
uniform vec2 uThresholds;
uniform vec3 uColor1;
uniform vec3 uColor2;
void main() {
  vec4 albedo = texture2D(uTex, vUv);
  float pulse = smoothstep(uThresholds.x, uThresholds.y,
    sin(uTime * uPulseSpeed - 1.57079632679) * 0.5 + 0.5) * uPulseAmount;
  pulse = pow(max(pulse, 0.0), uPower);
  float mask = texture2D(uMask, vUv).r;
  vec3 tinted = mix(albedo.rgb * uColor1, albedo.rgb * uColor2, pulse);
  albedo.rgb = mix(albedo.rgb, tinted, step(0.001, uPulseAmount));
  albedo.a *= mix(1.0, pulse, mask * step(0.001, uPulseAmount));
  gl_FragColor = albedo;
}
`;

/* filmgrain:程序噪声颗粒叠加 */
const FRAG_FILMGRAIN = /* glsl */ `
uniform float uStrength;
uniform float uScale;
void main() {
  vec4 albedo = texture2D(uTex, vUv);
  float n = weNoise(vUv * uScale + vec2(uTime * 13.7, uTime * 7.3));
  n = pow(n, 1.2);
  float mask = texture2D(uMask, vUv).r;
  albedo.rgb = mix(albedo.rgb, vec3(n), uStrength * mask * 0.5);
  gl_FragColor = albedo;
}
`;

/* vhs:色差 + 扫描线扰动(近似) */
const FRAG_VHS = /* glsl */ `
uniform float uStrength;
uniform float uChromatic;
uniform float uTracking;
void main() {
  float wave = sin(vUv.y * 3.14159265 * uTracking * 2.0 + uTime * 8.0) * 0.0015 * uStrength;
  float jitter = step(0.997, weHash(vec2(floor(vUv.y * 240.0), floor(uTime * 12.0)))) * 0.01;
  vec2 uvR = vUv + vec2(wave + jitter + uChromatic * 0.004, 0.0);
  vec2 uvB = vUv + vec2(-wave - jitter - uChromatic * 0.004, 0.0);
  vec4 albedo = texture2D(uTex, vUv);
  albedo.r = texture2D(uTex, uvR).r;
  albedo.b = texture2D(uTex, uvB).b;
  float scanline = 0.92 + 0.08 * sin(vUv.y * 900.0);
  albedo.rgb *= scanline;
  gl_FragColor = albedo;
}
`;

/* blur:可分离高斯(uDirection 定向) */
const FRAG_BLUR = /* glsl */ `
uniform vec2 uDirection; // (1,0) 或 (0,1)
uniform float uRadius;
void main() {
  vec2 step1 = uDirection * uTexel * uRadius;
  vec4 sum = texture2D(uTex, vUv) * 0.227027;
  sum += texture2D(uTex, vUv + step1 * 1.3846) * 0.316216;
  sum += texture2D(uTex, vUv - step1 * 1.3846) * 0.316216;
  sum += texture2D(uTex, vUv + step1 * 3.2308) * 0.070270;
  sum += texture2D(uTex, vUv - step1 * 3.2308) * 0.070270;
  gl_FragColor = sum;
}
`;

/* foliagesway:噪声相位驱动的四相正弦摆动(WE foliagesway.frag/vert 同构移植) */
const FRAG_FOLIAGESWAY = /* glsl */ `
uniform float uSpeed;   // speeduv
uniform float uPower;
uniform float uPhase;
uniform float uStrength;
uniform float uNoiseScale; // scale
uniform float uDir;     // scrolldirection
uniform float uRatio;
void main() {
  float mask = texture2D(uMask, vUv).r;
  // util/noise 贴图的 g 通道 → 程序噪声近似
  float noiseG = weNoise(vUv * uNoiseScale + vec2(37.0, 17.0));
  vec2 params = weRotate(vUv, uDir);
  float amp = uStrength * uStrength * 0.005 * mask;
  float phase = (noiseG * 6.2831853 + params.x * 10.0 + params.y * 5.0) * uPhase;
  vec4 sines = phase + uSpeed * uTime * vec4(1.0, -0.16161616, 0.0083333, -0.00019841);
  vec4 csines = 0.4 + phase + uSpeed * uTime * vec4(-0.5, 0.041666666, -0.0013888889, 0.000024801587);
  sines = sin(sines);
  csines = sin(csines);
  sines = pow(abs(sines), vec4(uPower)) * sign(sines);
  csines = pow(abs(csines), vec4(uPower)) * sign(csines);
  float aspect = (uTexel.y / uTexel.x) * uRatio;
  vec2 dirv = weRotate(vec2(1.0 / aspect, aspect), uDir);
  vec2 off = vec2(dirv.x * dot(sines, vec4(amp)), dirv.y * dot(csines, vec4(amp)));
  gl_FragColor = texture2D(uTex, vUv + off);
}
`;

/* swing:绕轴摆动(近似为纵向梯度位移) */
const FRAG_SWING = /* glsl */ `
uniform float uSpeed;
uniform float uStrength;
void main() {
  float mask = texture2D(uMask, vUv).r;
  float anim = sin(uTime * uSpeed);
  vec2 texCoordOffset = vec2(anim * uStrength * 0.5 * vUv.y * vUv.y, 0.0) * mask;
  gl_FragColor = texture2D(uTex, texCoordOffset + vUv);
}
`;

/* waterflow:flow map 平流(双周期混合防拉伸) */
const FRAG_WATERFLOW = /* glsl */ `
uniform float uFlowSpeed;
uniform float uFlowAmp;
void main() {
  vec2 flowColors = texture2D(uMask, vUv).rg;
  vec2 flowMask = (flowColors - vec2(0.498, 0.498)) * 2.0;
  float flowAmount = length(flowMask);
  float t = uTime * uFlowSpeed;
  vec2 off1 = flowMask * uFlowAmp * 0.1 * fract(t);
  vec2 off2 = flowMask * uFlowAmp * 0.1 * fract(t + 0.5);
  float blend = abs(fract(t) * 2.0 - 1.0);
  vec4 albedo = texture2D(uTex, vUv);
  vec4 f1 = mix(texture2D(uTex, vUv + off1), texture2D(uTex, vUv + off2), blend);
  gl_FragColor = mix(albedo, f1, flowAmount);
}
`;

/* cloudmotion:柏林噪声横向漂移(WE cloudmotion.frag/vert 同构移植) */
const FRAG_CLOUDMOTION = /* glsl */ `
uniform float uAmount;
uniform float uCDirection; // direction
uniform float uCSpeed;     // speed
uniform float uCScale;     // granularity
uniform float uCScaleX;    // granularity_horizontal
void main() {
  float mask = texture2D(uMask, vUv).r;
  vec2 nc = vUv;
  nc.x *= uTexel.y / uTexel.x;
  nc *= uCScale;
  nc.x *= uCScaleX;
  nc.x += uTime * uCSpeed;
  float n = weNoise(nc);
  vec2 offset = vec2((n * 2.0 - 1.0) * uAmount * mask, 0.0);
  offset = weRotate(offset, uCDirection + 1.57079632679);
  // 目标处遮罩为零则不位移(避免把画面外内容拉进无云区域)
  float dstMask = texture2D(uMask, vUv + offset).r;
  vec2 uvs = mix(vUv, vUv + offset, dstMask);
  gl_FragColor = texture2D(uTex, uvs);
}
`;

/* waterripple:法线贴图双相位滚动采样位移(WE waterripple 同构移植) */
const FRAG_WATERRIPPLE = /* glsl */ `
uniform float uAnimSpeed;  // animationspeed
uniform float uScale;      // scale
uniform float uScrollSpeed; // scrollspeed
uniform float uDirection;  // scrolldirection
uniform float uRatio;      // ratio
uniform float uStrength;   // ripplestrength
uniform sampler2D uNormal; // effects/waterripplenormal
void main() {
  float mask = texture2D(uMask, vUv).r;
  vec2 scroll = weRotate(vec2(0.0, 1.0), uDirection) * uScrollSpeed * uScrollSpeed * uTime;
  vec4 rc = vec4(
    vUv + uTime * uAnimSpeed * uAnimSpeed + scroll,
    vUv * 1.333 - uTime * uAnimSpeed * uAnimSpeed + scroll
  ) * uScale;
  float aspect = uTexel.y / uTexel.x;
  rc.xz *= aspect;
  rc.yw *= uRatio;
  vec3 n1 = texture2D(uNormal, rc.xy).xyz * 2.0 - 1.0;
  vec3 n2 = texture2D(uNormal, rc.zw).xyz * 2.0 - 1.0;
  vec3 normal = normalize(vec3(n1.xy + n2.xy, n1.z));
  gl_FragColor = texture2D(uTex, vUv + normal.xy * uStrength * uStrength * mask);
}
`;

/* glitter:两 pass(WE glitter_prepare + glitter_combine 移植)。
 * prepare 生成时序闪烁图案,combine 按遮罩把 glitter 颜色加色叠回原图。 */
const FRAG_GLITTER_PREPARE = /* glsl */ `
uniform float uDensity;
uniform float uGSpeed;
void main() {
  float density = uDensity * uDensity;
  float time = uTime * uGSpeed * density;
  vec2 nc = vUv * 5.0;
  // util/perlin_256 的 r/g 双通道 → 程序噪声近似
  float n0 = weNoise(nc) * (1.0 - weNoise(nc + 11.3));
  float timer = fract(n0 * 100.0 + time);
  float gd = density * 0.5;
  float g = smoothstep(0.5 - gd, 0.5, timer) * smoothstep(0.5 + gd, 0.5, timer);
  g = smoothstep(0.5, 1.0, g);
  g *= g;
  gl_FragColor = vec4(g, g, g, 1.0);
}
`;
const FRAG_GLITTER_COMBINE = /* glsl */ `
uniform float uGScale;   // scale
uniform float uGOpacity; // alpha
uniform vec3 uGColor;    // color
uniform sampler2D uBase; // 图层原始纹理(链内 uTex 是 prepare 的产物)
void main() {
  vec4 albedo = texture2D(uBase, vUv);
  float mask = texture2D(uMask, vUv).r;
  vec2 gc = vUv;
  gc.x *= uTexel.y / uTexel.x;
  float glitter = texture2D(uTex, gc * uGScale).r;
  albedo.rgb += uGColor * glitter * uGOpacity * mask;
  gl_FragColor = albedo;
}
`;

/* shine:扫过的高光带(近似) */
const FRAG_SHINE = /* glsl */ `
uniform float uSpeed;
uniform float uStrength;
uniform float uWidth;
void main() {
  vec4 albedo = texture2D(uTex, vUv);
  float pos = fract(uTime * uSpeed);
  float band = smoothstep(pos - uWidth, pos, vUv.x) * smoothstep(pos + uWidth, pos, vUv.x);
  float mask = texture2D(uMask, vUv).r;
  albedo.rgb += vec3(band * uStrength * mask);
  gl_FragColor = albedo;
}
`;

/* iris / lightshafts / godrays:恒等(眼球追踪与体积光不在近似范围) */
const FRAG_NOOP = /* glsl */ `
void main() {
  gl_FragColor = texture2D(uTex, vUv);
}
`;

/**
 * 构建一个对象效果的全部 pass。
 * 返回 null 表示"整体跳过"(效果不可见/未实现),渲染器直接用原纹理。
 */
export async function buildEffectPasses(
  effect: WEObjectEffect,
  context: EffectContext,
): Promise<EffectPass[] | null> {
  if (effect.visible === false) return null;
  const key = resolveEffectKey(effect.file);

  if (key === "noop") return null;

  const constants = constantMap(effect, context.properties);
  const c = (name: string, fallback: number) =>
    constantNumber(constants[name] ?? constants[`g_${name}`], fallback);
  const cv2 = (name: string, fallback: [number, number]) =>
    constantVec2(constants[name], fallback);
  const cv3 = (name: string, fallback: [number, number, number]) =>
    constantVec3(constants[name], fallback);

  // 用户绑定的颜色属性(schemecolor 等)在常量表里可能是 "r g b" 字符串
  const colorOf = (name: string, fallback: [number, number, number]) =>
    cv3(name, fallback);

  let fragment = FRAG_NOOP;
  let uniforms = baseUniforms();
  let slotMap: Record<number, string> = { 1: "uMask" };

  switch (key) {
    case "shake":
      fragment = FRAG_SHAKE;
      uniforms.uSpeed = { value: c("speed", 1) };
      uniforms.uStrength = { value: c("strength", 0.1) };
      uniforms.uFriction = {
        value: new THREE.Vector2(...cv2("friction", [1, 1])),
      };
      break;
    case "waterwaves":
      fragment = FRAG_WATERWAVES;
      uniforms.uSpeed = { value: c("speed", 5) };
      uniforms.uScale = { value: c("scale", 200) };
      uniforms.uExponent = { value: c("exponent", 1) };
      uniforms.uStrength = { value: c("strength", 0.1) };
      uniforms.uDirection = { value: c("direction", 0) };
      break;
    case "tint":
      fragment = FRAG_TINT;
      uniforms.uTintColor = {
        value: new THREE.Vector3(...colorOf("color", [1, 1, 1])),
      };
      uniforms.uBlendAlpha = { value: c("blendalpha", c("alpha", 1)) };
      break;
    case "opacity":
      fragment = FRAG_OPACITY;
      uniforms.uAlpha = { value: c("alpha", 1) };
      break;
    case "scroll":
      fragment = FRAG_SCROLL;
      {
        const sx = c("speedx", 0.2),
          sy = c("speedy", 0.2);

        uniforms.uScroll = {
          value: new THREE.Vector2(
            Math.sign(sx) * sx * sx,
            Math.sign(sy) * sy * sy,
          ),
        };
        uniforms.uRepeat = {
          value: new THREE.Vector2(...cv2("repeat", [1, 1])),
        };
      }
      break;
    case "pulse":
      fragment = FRAG_PULSE;
      uniforms.uPulseSpeed = { value: c("speed", 2) };
      uniforms.uPulseAmount = { value: c("amount", c("strength", 0.5)) };
      uniforms.uPower = { value: c("power", 1) };
      uniforms.uThresholds = {
        value: new THREE.Vector2(...cv2("thresholds", [0, 1])),
      };
      uniforms.uColor1 = {
        value: new THREE.Vector3(...colorOf("color1", [1, 1, 1])),
      };
      uniforms.uColor2 = {
        value: new THREE.Vector3(...colorOf("color2", [1, 1, 1])),
      };
      break;
    case "filmgrain":
      fragment = FRAG_FILMGRAIN;
      uniforms.uStrength = { value: c("strength", 0.5) };
      uniforms.uScale = { value: c("scale", 10) };
      break;
    case "vhs":
      fragment = FRAG_VHS;
      uniforms.uStrength = { value: c("strength", 1.2) };
      uniforms.uChromatic = { value: c("chromatic", 0.2) };
      uniforms.uTracking = { value: c("tracking", 0.2) };
      break;
    case "blur":
      fragment = FRAG_BLUR;
      {
        // 材质名带 _x/_y 区分方向;对象上只有一份常量,两方向各跑一遍
        const isVertical = /_y\b|vertical/i.test(
          effect.file + JSON.stringify(effect.passes ?? []),
        );

        // blurprecise 的 effect.json 有两个 pass(x 与 y),由调用方按 pass 序号轮换:
        // 这里通过 passIndex 参数无法拿到,简化为水平+垂直各一次由 effectCount 控制
        uniforms.uDirection = { value: new THREE.Vector2(1, 0) };
        uniforms.uRadius = { value: c("strength", c("radius", 3)) };
        uniforms._vertical = { value: isVertical };
      }
      break;
    case "foliagesway":
      fragment = FRAG_FOLIAGESWAY;
      uniforms.uSpeed = { value: c("speeduv", c("speed", 5)) };
      uniforms.uPower = { value: c("power", 1) };
      uniforms.uPhase = { value: c("phase", 0.5) };
      uniforms.uStrength = { value: c("strength", 0.4) };
      uniforms.uNoiseScale = { value: c("scale", 0.05) };
      uniforms.uDir = { value: c("scrolldirection", 0) };
      uniforms.uRatio = { value: c("ratio", 0.3) };
      break;
    case "swing":
      fragment = FRAG_SWING;
      uniforms.uSpeed = { value: c("speed", 1) };
      uniforms.uStrength = { value: c("strength", 0.1) };
      break;
    case "waterflow":
      fragment = FRAG_WATERFLOW;
      uniforms.uFlowSpeed = { value: c("flowspeed", c("speed", 0.5)) };
      uniforms.uFlowAmp = { value: c("flowamp", c("strength", 1)) };
      break;
    case "cloudmotion":
      fragment = FRAG_CLOUDMOTION;
      uniforms.uAmount = { value: c("amount", 0.1) };
      uniforms.uCDirection = { value: c("direction", 1.57079632679) };
      uniforms.uCSpeed = { value: c("speed", 0.02) };
      uniforms.uCScale = { value: c("granularity", 2) };
      uniforms.uCScaleX = { value: c("granularity_horizontal", 0.5) };
      break;
    case "waterripple":
      fragment = FRAG_WATERRIPPLE;
      uniforms.uAnimSpeed = { value: c("animationspeed", 0.15) };
      uniforms.uScale = { value: c("scale", 1) };
      uniforms.uScrollSpeed = { value: c("scrollspeed", 0) };
      uniforms.uDirection = { value: c("scrolldirection", 0) };
      uniforms.uRatio = { value: c("ratio", 1) };
      uniforms.uStrength = { value: c("ripplestrength", c("strength", 0.1)) };
      uniforms.uNormal = { value: flatNormalTexture() };
      slotMap = { 1: "uMask", 2: "uNormal" };
      break;
    case "glitter":
      return buildGlitterPasses(effect, context, constants);
    case "shine":
      fragment = FRAG_SHINE;
      uniforms.uSpeed = { value: c("speed", 0.2) };
      uniforms.uStrength = { value: c("strength", 0.5) };
      uniforms.uWidth = { value: c("width", 0.2) };
      break;
    default:
      // iris/lightshafts/godrays:恒等 pass,保住图层本身
      break;
  }

  await attachSlotTextures(effect, uniforms, slotMap, context);
  const pass = makePass(fragment, uniforms);

  // blur 效果补一个垂直方向 pass(x + y 两趟才收敛)
  const passes: EffectPass[] = [pass];

  if (key === "blur") {
    const vertical = makePass(fragment, {
      ...structuredCloneUniforms(uniforms),
    });

    vertical.material.uniforms.uDirection.value = new THREE.Vector2(0, 1);
    // 共享 mask uniforms 引用
    vertical.material.uniforms.uMask = uniforms.uMask;
    vertical.material.uniforms.uMask2 = uniforms.uMask2;
    passes.push(vertical);
  }

  return passes;
}

/** 复制 uniform 表(纹理引用置空,由共享覆盖) */
function structuredCloneUniforms(
  uniforms: Record<string, THREE.IUniform>,
): Record<string, THREE.IUniform> {
  const out: Record<string, THREE.IUniform> = {};

  for (const [name, uniform] of Object.entries(uniforms)) {
    const value = uniform.value;

    if (value instanceof THREE.Vector2) out[name] = { value: value.clone() };
    else if (value instanceof THREE.Vector3)
      out[name] = { value: value.clone() };
    else if (value instanceof THREE.Texture)
      out[name] = { value: whiteTexture() };
    else out[name] = { value };
  }

  return out;
}

/** 全屏直通用四边形几何(渲染器复用) */
export function blitGeometry(): THREE.BufferGeometry {
  return FULLSCREEN_QUAD;
}

/** 用指定 pass 把 src 纹理直通到 dst 渲染目标。 */
export function blitPass(
  renderer: THREE.WebGLRenderer,
  material: THREE.ShaderMaterial,
  srcTexture: THREE.Texture,
  dst: THREE.WebGLRenderTarget,
  quad: THREE.Mesh,
): void {
  material.uniforms.uTex.value = srcTexture;
  material.uniforms.uTexel.value.set(1 / dst.width, 1 / dst.height);
  quad.material = material;
  renderer.setRenderTarget(dst);
  quad.renderOrder = 0;
  renderer.render(
    quad.userData.scene as THREE.Scene,
    quad.userData.camera as THREE.OrthographicCamera,
  );
  renderer.setRenderTarget(null);
}

/** 创建直通用 scene/quad 组合(一次创建,反复换材质复用)。 */
export function createBlitScene(): {
  scene: THREE.Scene;
  camera: THREE.OrthographicCamera;
  quad: THREE.Mesh;
} {
  const scene = new THREE.Scene();
  const camera = new THREE.OrthographicCamera(-1, 1, 1, -1, 0, 1);
  const quad = new THREE.Mesh(
    FULLSCREEN_QUAD,
    undefined as unknown as THREE.Material,
  );

  quad.frustumCulled = false;
  quad.userData.scene = scene;
  quad.userData.camera = camera;
  scene.add(quad);

  return { scene, camera, quad };
}

/** resolveBound 再导出一次供外部效果模块使用 */
export { resolveBound };
