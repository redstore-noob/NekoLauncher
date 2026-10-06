/**
 * Wallpaper Engine 场景壁纸的类型定义。
 *
 * 字段名与 scene.json / 粒子 JSON / 材质 JSON 原样对应(WE 用全小写键);
 * 数值型字段在 WE 里是 "1.00000 2.00000 3.00000" 形式的空格分隔字符串,
 * 解析交给 values.ts,这里保持原始类型(loose)。
 */

/** 用户属性绑定:{"user": 属性名, "value": 默认值} 或裸值 */
export interface WEUserBound<T = unknown> {
  user?: string | { name: string; condition?: string };
  value: T;
}

/** 可能被用户属性绑定的值 */
export type WEBound<T = unknown> = T | WEUserBound<T>;

/** scene.json 顶层 */
export interface WESceneFile {
  general?: {
    orthogonalprojection?: { width?: number; height?: number };
    clearcolor?: string;
    cameraparallax?: boolean;
    cameraparallaxamount?: number;
  };
  objects?: WESceneObject[];
}

/** scene.json 单个对象(image/particle/text/sound/组合) */
export interface WESceneObject {
  id: number;
  name?: string;
  /** 图层类型判定:image=贴图图层 */
  image?: string;
  particle?: string;
  text?: { value?: WEBound<string>; script?: string; user?: string };
  sound?: string[];
  shape?: string;
  visible?: WEBound<boolean>;
  origin?: string; // "x y z" 设计像素坐标
  size?: string; // "w h"
  scale?: string; // "x y z"
  angles?: string; // "x y z" 弧度
  alpha?: WEBound<number>;
  color?: string; // "r g b"
  parallaxDepth?: string; // "x y"
  parent?: number;
  effects?: WEObjectEffect[];
  /** 文本对象字段 */
  font?: string;
  pointsize?: number;
  horizontalalign?: string;
  verticalalign?: string;
  padding?: number;
  anchor?: string;
  opaquebackground?: boolean;
  backgroundcolor?: string;
  backgroundbrightness?: number;
  /** 粒子对象的实例覆盖 */
  instanceoverride?: { alpha?: number };
}

/** 对象上的效果实例 */
export interface WEObjectEffect {
  file: string; // "effects/waterwaves/effect.json"
  visible?: boolean;
  passes: {
    id?: number;
    constantshadervalues?: Record<string, WEBound<number | string>>;
    textures?: (string | null)[]; // 槽位 → mask 纹理名(无目录与扩展名)
    combos?: Record<string, number>;
  }[];
}

/** models/xxx.json(图像对象 → 材质) */
export interface WEModelFile {
  material?: string;
  autosize?: boolean;
  /** 裁剪偏移 "x y"(设计坐标,像素):作者在编辑器里裁出局部做图层时,
   * 原图画心与裁剪区中心的差。渲染时图层中心 = 对象 origin − cropoffset,
   * 不应用它的话每个裁剪图层都以 origin 为中心画,整张壁纸错位撕裂 */
  cropoffset?: string;
  /** 骨胳/puppet 网格变形定义(.mdl);本渲染器不做网格变形,仅记录 */
  puppet?: string;
}

/** materials/xxx.json(材质 → 着色器与贴图) */
export interface WEMaterialFile {
  passes?: {
    shader?: string; // 如 "genericimage"、"effects/vhs"
    textures?: string[]; // 贴图名(相对 materials/)
    blending?: string;
  }[];
}

/** particles/xxx.json(粒子系统定义) */
export interface WEParticleFile {
  material?: string;
  maxcount?: number;
  starttime?: number;
  flags?: number;
  sequencemultiplier?: number;
  emitter?: {
    name: string;
    origin?: string;
    rate?: number;
    distancemin?: number;
    distancemax?: number;
    directions?: string;
    [key: string]: unknown;
  }[];
  initializer?: Record<string, unknown>[];
  operator?: Record<string, unknown>[];
  renderer?: { name: string }[];
}

/** 后端 /wescene 载荷里的用户属性 */
export interface WEProperty {
  type: string; // bool/color/combo/slider/textinput/...
  value: unknown;
  text?: string;
}

/** GetWallpaperEngineWallpaper 返回的场景载荷(见 Go 端 WallpaperEngineScene) */
export interface WEScenePayload {
  Entry: string;
  DesignWidth: number;
  DesignHeight: number;
  Objects: WESceneObject[];
  GeneralProperties: Record<string, WEProperty>;
}
