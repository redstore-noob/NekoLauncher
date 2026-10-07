/**
 * Wallpaper Engine 场景渲染器(three.js 正交 2D)。
 *
 * 职责:
 *  - 把 scene.json 的对象树映射成 three 舞台:图像层(PlaneGeometry + 纹理)、
 *    文本层(Canvas 纹理 + 脚本驱动)、粒子层(实例化四边形)、组合层(Group);
 *  - parent 变换嵌套与 parallaxDepth 鼠标视差;
 *  - 对象效果链:逐 pass ping-pong 渲染目标后处理(effects.ts);
 *  - 设计分辨率 → 窗口的等比 cover 缩放;
 *  - 资源经 SceneAssets(/wescene 路由)异步加载,先建层后补纹理,不阻塞启动。
 *
 * 坐标系:y 向下(与 WE 编辑器一致):相机 top=0、bottom=H,
 * 所有纹理 flipY=false。跳过的对象类型:sound(无音频需求)、
 * model/led 等 3D 与外设对象。
 */

import type { WESceneObject, WEScenePayload } from "./types";

import * as THREE from "three";

import { SceneAssets, whiteTexture } from "./assets";
import {
  buildEffectPasses,
  blitPass,
  createBlitScene,
  type EffectPass,
} from "./effects";
import { ParticleSystem } from "./particles";
import { compileTextScript } from "./scripts";
import {
  constantNumber,
  parseColor,
  parseVec,
  parseVec2,
  resolveBound,
  resolveVisible,
} from "./values";

/** 单个图像层的运行时(纹理就绪前后都存在,只是可能先不显示) */
interface LayerNode {
  object: WESceneObject;
  group: THREE.Group;
  mesh: THREE.Mesh;
  material: THREE.MeshBasicMaterial;
  textureApplied: boolean;
  /** 效果链(pass 展开后的序列)与其渲染目标 */
  effectPasses: EffectPass[];
  rtA: THREE.WebGLRenderTarget | null;
  rtB: THREE.WebGLRenderTarget | null;
  baseTexture: THREE.Texture | null;
  parallax: [number, number];
  basePosition: THREE.Vector3;
  /** puppet 蒙皮状态(object.Puppet 存在时非空):骨骼 rest⁻¹、动画轨道
   * 与着色器 bone 矩阵 uniform(每帧更新,见 updatePuppetSkinning) */
  puppet: PuppetSkinState | null;
}

/** puppet 蒙皮 uniform 的骨骼数上限(GLSL uniform 数组需编译期常量;
 * uniform 预算 64×2 个 mat4 ≈ 8KB,远低于 WebGL 保底下限) */
const PUPPET_BONE_LIMIT = 64;

interface PuppetSkinState {
  /** 每骨骼 rest 矩阵的逆(绑定姿势 → 骨骼局部),静态 */
  restInv: THREE.Matrix4[];
  /** 每骨骼 rest(绑定姿势)矩阵,静态;蒙皮公式是 uBoneAnim × rest⁻¹,
   * "不做动画"的骨骼应置为 rest(而非单位阵),顶点才留在绑定位置 */
  rest: THREE.Matrix4[];
  /** 动画轨道(摊平 9 float/帧)与时长;无动画为 null(rest pose) */
  tracks: number[][] | null;
  duration: number;
  /** 层级父索引(Parent 字段,-1 = 根) */
  parents: number[];
  /** 每帧写入的骨骼世界矩阵 uniform(T×R×S ×层级) */
  boneAnim: { value: THREE.Matrix4[] };
  /** 骨骼 rest 逆矩阵 uniform(静态) */
  boneRestInv: { value: THREE.Matrix4[] };
  /** 帧插值的工作矩阵,避免每帧分配 */
  scratch: THREE.Matrix4[];
}

interface TextNode {
  object: WESceneObject;
  group: THREE.Group;
  mesh: THREE.Mesh;
  material: THREE.MeshBasicMaterial;
  canvas: HTMLCanvasElement;
  texture: THREE.CanvasTexture;
  overlayCanvas?: HTMLCanvasElement;
  overlay2d?: CanvasRenderingContext2D | null;
  script: { update: () => void } | null;
  host: { text: string; userProperties: Record<string, unknown> };
  parallax: [number, number];
  basePosition: THREE.Vector3;
  lastScriptTick: number;
}

interface ParticleNode {
  object: WESceneObject;
  group: THREE.Group;
  system: ParticleSystem;
  parallax: [number, number];
  basePosition: THREE.Vector3;
}

/** 视差跟随的平滑系数(每秒) */
const PARALLISMOOTHING = 3;
/** 帧成本采样窗口:连续多帧明显掉出 60fps → 锁 30fps。
 * 重壁纸(多图层 × 多条 4K 效果链)每帧 GPU 开销会拖垮整个 WebView 合成,
 * 把 UI 一起卡住;背景壁纸 30fps 视觉足够,换来界面全程流畅。 */
const FRAME_SAMPLES = 45;
const FRAME_SLOW_MS = 24;
const FRAME_CAPPED_MS = 1000 / 30;

/** 判断"全不透明 + 大面积纯黑"的光晕素材(32×32 降采样统计)。
 * 阈值刻意苛刻(逐通道 < 12 的纯黑):摄影类贴图(哪怕夜景)经 JPEG/压缩
 * 噪声后不会大面积落到纯黑区间,只有无 alpha 的光晕资产才是"黑底=应透明"。 */
function isOpaqueDarkGlow(image: unknown): boolean {
  if (
    !(image instanceof HTMLImageElement) &&
    !(image instanceof HTMLCanvasElement)
  )
    return false;
  try {
    const canvas = document.createElement("canvas");
    const size = 32;

    canvas.width = size;
    canvas.height = size;
    const context = canvas.getContext("2d");

    if (!context) return false;
    context.drawImage(image as CanvasImageSource, 0, 0, size, size);
    const data = context.getImageData(0, 0, size, size).data;
    let dark = 0;
    const total = size * size;

    for (let i = 0; i < data.length; i += 4) {
      if (data[i + 3] < 250) return false; // 存在半透明/透明像素 → 正常素材
      if (data[i] < 12 && data[i + 1] < 12 && data[i + 2] < 12) dark++;
    }

    return dark / total >= 0.6;
  } catch {
    return false;
  }
}

export interface SceneRendererOptions {
  canvas: HTMLCanvasElement;
  payload: WEScenePayload;
  /** 首帧渲染完成(场景至少有一层已上屏) */
  onFirstFrame?: () => void;
  /** 渲染分辨率倍数(相对窗口 CSS 像素,默认 1;HiDPI 屏可调高换精细) */
  pixelRatio?: number;
  /** 刷新率上限(fps);0/未设置 = 自适应(重壁纸自动锁 30) */
  fpsCap?: number;
}

export class SceneRenderer {
  private renderer: THREE.WebGLRenderer;
  private scene = new THREE.Scene();
  private camera: THREE.OrthographicCamera;
  private sceneRoot = new THREE.Group();
  private assets = new SceneAssets();
  private blit = createBlitScene();
  private layers: LayerNode[] = [];
  private texts: TextNode[] = [];
  private particleNodes: ParticleNode[] = [];
  private rafId = 0;
  private clock = new THREE.Clock();
  private sceneTime = 0;
  private pointer: [number, number] = [0, 0];
  private pointerSmoothed: [number, number] = [0, 0];
  private disposed = false;
  private firstFrameSent = false;
  private resizeObserver: ResizeObserver | null = null;
  private pointerHandler: ((event: PointerEvent) => void) | null = null;
  private white: THREE.Texture;

  constructor(private options: SceneRendererOptions) {
    const { canvas, payload } = options;

    this.renderer = new THREE.WebGLRenderer({
      canvas,
      alpha: true,
      antialias: false,
      powerPreference: "high-performance",
    });
    this.renderer.setClearColor(0x000000, 0);
    this.renderer.autoClear = true;

    const designWidth = payload.DesignWidth || 1920;
    const designHeight = payload.DesignHeight || 1080;

    this.camera = new THREE.OrthographicCamera(
      0,
      designWidth,
      0,
      designHeight,
      -2000,
      2000,
    );
    this.scene.add(this.sceneRoot);
    this.white = whiteTexture();
    // 固定刷新率上限直接生效;未设置走自适应(重壁纸观测掉帧后锁 30)
    if ((options.fpsCap ?? 0) > 0) this.frameCapMs = 1000 / options.fpsCap!;
    // 排障句柄:harness/控制台里可现场检查层状态(不影响正常渲染)
    (
      window as unknown as { __weSceneRenderer?: SceneRenderer }
    ).__weSceneRenderer = this;
  }

  /** 加载资源并搭建场景;任何一步致命失败都会抛错(调用方回退静态图)。 */
  async start(): Promise<void> {
    try {
      const { payload } = this.options;

      await this.buildObjects(payload);
      this.attachWindowListeners();
      this.resize();
      this.clock.start();
      this.loop();
      void this.writeDiag("start-ok");
      // 诊断:2 秒后采样文本 canvas 内容与网格投影位置(生产排障)
      window.setTimeout(() => {
        try {
          const node = this.texts[0];

          if (!node) return;
          const projected = node.mesh
            .getWorldPosition(new THREE.Vector3())
            .project(this.camera);
          const screenX = Math.round(
            ((projected.x + 1) / 2) * this.renderer.domElement.clientWidth,
          );
          const screenY = Math.round(
            ((1 - projected.y) / 2) * this.renderer.domElement.clientHeight,
          );
          // canvas 像素统计:非透明像素数
          const ctx = node.canvas.getContext("2d")!;
          const data = ctx.getImageData(
            0,
            0,
            node.canvas.width,
            node.canvas.height,
          ).data;
          let opaque = 0;

          for (let i = 3; i < data.length; i += 4) if (data[i] > 0) opaque++;
          void fetch("/wescene/diag", {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({
              event: "text-canvas",
              text: node.host.text,
              opaquePixels: opaque,
              canvasW: node.canvas.width,
              screenX,
              screenY,
              meshVisible: node.mesh.visible,
              materialOpacity: node.material.opacity,
              renderOrder: node.mesh.renderOrder,
              parentInScene:
                (!!node.mesh.parent &&
                  node.mesh.parent.parent === this.sceneRoot) ||
                !!node.mesh.parent?.parent,
              groupPos: [
                Math.round(node.group.position.x),
                Math.round(node.group.position.y),
              ],
              meshScale: [
                Math.round(node.mesh.scale.x),
                Math.round(node.mesh.scale.y),
              ],
            }),
          });
        } catch (e) {
          void fetch("/wescene/diag", {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({ event: "text-canvas-err", text: String(e) }),
          });
        }
      }, 2000);
    } catch (err) {
      const msg = err instanceof Error ? err.message : String(err);

      void this.writeDiag("start-fail: " + msg);
      throw err;
    }
  }

  /** @internal 诊断:生产环境排障用,失败静默 */
  private async writeDiag(event: string): Promise<void> {
    try {
      await fetch("/wescene/diag", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          event,
          layers: this.layers.length,
          texts: this.texts.length,
          particles: this.particleNodes.length,
          t: new Date().toISOString(),
        }),
      });
    } catch {
      /* 忽略 */
    }
  }

  // ---- 场景搭建 ----

  private async buildObjects(payload: WEScenePayload): Promise<void> {
    const objects = payload.Objects ?? [];
    const properties = payload.GeneralProperties ?? {};
    const groups = new Map<number, THREE.Group>();

    for (const object of objects) {
      if (!object || typeof object.id !== "number") continue;
      const group = new THREE.Group();

      groups.set(object.id, group);
      const [x, y] = parseVec2(object.origin, [0, 0]);
      const designHeight = payload.DesignHeight || 1080;

      // WE 的 origin 是**自底向上**(y-up)的设计坐标:origin.y=0 在画面底边。
      // 本渲染器相机是 y 向下的屏幕坐标系,必须翻转 Y,否则每个图层都垂直
      // 镜像错位(桌子飞到天花板、光环掉到地板,角色被撕成散落碎片)。
      // 实证:29 层样本里桌子 y=244(应贴底)、光环 y=1829(应贴顶)、
      // 睫毛 y=1328(视线高度),翻转后全部落回解剖学正确位置
      group.position.set(x, designHeight - y, 0);
      const scale = parseVec2(object.scale ?? "1 1", [1, 1]);

      if (scale[0] !== 1 || scale[1] !== 1)
        group.scale.set(scale[0], scale[1], 1);
      // 欧拉角 "x y z" 的 z 分量是绕屏幕法线的滚转(样本里非零的都是它)
      const roll = parseVec(object.angles ?? "", [0, 0, 0])[2] ?? 0;

      if (roll) group.rotation.z = roll;
      // 注意:parent 字段不做变换嵌套。WE 导出的 scene.json 里子对象 origin
      // 已经是世界坐标(实证:嵌套会把光斑组挪离预览图所示位置,黑底光晕
      // 盖出巨型黑块),父级信息只用于编辑器分组;绘制顺序按数组序即可
      this.sceneRoot.add(group);
      if (resolveVisible(object.visible, properties) === false) continue;

      if (object.image) await this.buildImageLayer(object, group, properties);
      else if (object.particle)
        await this.buildParticleLayer(object, group, properties);
      else if (object.text) this.buildTextLayer(object, group, properties);
      // sound/空组合层:只留 Group
    }

    // 渲染顺序 = objects 数组顺序(WE 自下而上绘制)
    let order = 0;

    for (const object of objects) {
      const group = groups.get(object.id);

      if (!group) continue;
      group.traverse((child) => {
        child.renderOrder = order;
      });
      order++;
    }
  }

  private makeParallax(object: WESceneObject): [number, number] {
    return parseVec2(object.parallaxDepth ?? "", [0, 0]);
  }

  private async buildImageLayer(
    object: WESceneObject,
    group: THREE.Group,
    properties: Record<string, WEScenePayload["GeneralProperties"][string]>,
  ): Promise<void> {
    const material = new THREE.MeshBasicMaterial({
      map: this.white,
      transparent: true,
      // y 向下相机是镜像投影,会翻转三角形绕序;2D 图层必须双面渲染
      side: THREE.DoubleSide,
      depthTest: false,
      depthWrite: false,
    });
    // puppet 蒙皮层:真实网格(设计像素单位、绕对象中心、y 向上)替代整图
    // quad,顶点由骨骼动画在着色器里驱动(见 updatePuppetSkinning)
    const puppetState = object.Puppet
      ? this.makePuppetState(
          object.Puppet,
          material,
          !(window as unknown as { nyaSkinOff?: boolean }).nyaSkinOff,
        )
      : null;
    const mesh = puppetState
      ? new THREE.Mesh(puppetState.geometry, material)
      : new THREE.Mesh(new THREE.PlaneGeometry(1, 1), material);

    group.add(mesh);

    const node: LayerNode = {
      object,
      group,
      mesh,
      material,
      textureApplied: false,
      effectPasses: [],
      rtA: null,
      rtB: null,
      baseTexture: null,
      parallax: this.makeParallax(object),
      basePosition: group.position.clone(),
      puppet: puppetState,
    };

    this.layers.push(node);

    // 尺寸:对象声明优先,其次贴图原始尺寸
    let size = parseVec2(object.size ?? "", [0, 0]);
    const alpha = constantNumber(resolveBound(object.alpha, properties), 1);

    if (alpha < 1) material.opacity = alpha;
    if (object.color) {
      const [r, g, b] = parseColor(object.color, [1, 1, 1, 1]);

      material.color.setRGB(r, g, b);
    }

    const imageRef = object.image ?? "";
    const isSolid = /util\/solidlayer\.json$/i.test(
      imageRef.replace(/\\/g, "/"),
    );
    let baseTexture: THREE.Texture | null = isSolid ? this.white : null;

    if (!isSolid && imageRef) {
      const textureNames = await this.assets.materialTextures(imageRef);

      if (textureNames.length > 0) {
        baseTexture = await this.assets.texture(textureNames[0]);
      }
    }
    if (!baseTexture) {
      // 贴图缺失:纯色/未知对象按声明尺寸给一块白板(带 color),避免空引用
      baseTexture = this.white;
    }
    node.baseTexture = baseTexture;

    if (size[0] <= 0 || size[1] <= 0) {
      const image = baseTexture.image as
        | { width?: number; height?: number }
        | undefined;

      size = [image?.width ?? 100, image?.height ?? 100];
    }
    // puppet 层顶点已是设计像素单位,不按 size 缩放(quad 路径才需要)
    if (!puppetState) mesh.scale.set(size[0], size[1], 1);
    if (puppetState) {
      // puppet mesh 顶点是**全局设计坐标**(原点=画布中心,y 向上;实测范围
      // 覆盖整张画布)——忽略对象 origin,把层钉在画布中心,y 镜像交给
      // 帧循环里骨骼矩阵就绪后的 mesh.scale.y=-1
      const designWidth = this.options.payload.DesignWidth || 1920;
      const designHeight = this.options.payload.DesignHeight || 1080;

      group.position.set(designWidth / 2, designHeight / 2, 0);
      node.basePosition.copy(group.position);
    }
    material.map = baseTexture;
    material.needsUpdate = true;
    node.textureApplied = true;
    // 黑底光晕素材:贴图全不透明且大面积近黑(典型镜头光晕资产,作者没做
    // alpha 通道)。WE 里这类素材不产生黑块——黑色对画面无贡献,等效加色
    // 混合;普通 alpha 混合会把黑底实体盖上屏(巨型黑矩形)
    if (baseTexture !== this.white && isOpaqueDarkGlow(baseTexture.image)) {
      material.blending = THREE.AdditiveBlending;
    }

    // 效果链:对象声明的每个效果展开成 1..n 个 pass
    for (const effect of object.effects ?? []) {
      try {
        const passes = await buildEffectPasses(effect, {
          properties,
          loadTexture: (name) => this.assets.texture(name),
          baseTexture,
          width: size[0],
          height: size[1],
        });

        if (passes) node.effectPasses.push(...passes);
      } catch {
        // 单个效果失败不影响其余层
      }
    }
    if (node.effectPasses.length > 0) {
      // 效果链分辨率上限 2560:WE 本体也在屏幕分辨率上跑后处理,4K RT
      // 纯属浪费(每个带效果的图层两块 RT,6 块 4K RT 会压爆 GPU 预算)
      const rtWidth = Math.min(2560, Math.max(64, Math.round(size[0])));
      const rtHeight = Math.min(2560, Math.max(64, Math.round(size[1])));

      node.rtA = this.makeRT(rtWidth, rtHeight);
      node.rtB = this.makeRT(rtWidth, rtHeight);
      material.map = node.rtA.texture;
      material.needsUpdate = true;
    }
  }

  /**
   * 搭建 puppet 蒙皮层:几何 + 蒙皮权重 attribute + 注入骨骼矩阵 uniform 的材质。
   *
   * 坐标:mesh 顶点是 y 向上的设计像素(WE 模型空间);本渲染器相机 y 向下,
   * 用 mesh.scale.y = -1 整体镜像(双面材质,翻面无感),蒙皮数学保持 y-up。
   * 蒙皮用 onBeforeCompile 注入 MeshBasicMaterial:贴图/alpha/加色混合等
   * 既有材质特性全部保留,只改顶点位置计算(LBS):
   *   v' = Σ wᵢ × (Mᵢ(t) × Restᵢ⁻¹) × v
   */
  private makePuppetState(
    puppet: NonNullable<WESceneObject["Puppet"]>,
    material: THREE.MeshBasicMaterial,
    skinEnabled: boolean,
  ): PuppetSkinState & { geometry: THREE.BufferGeometry } {
    const vertexCount = puppet.Positions.length / 3;
    const geometry = new THREE.BufferGeometry();
    const positions = new Float32Array(puppet.Positions);
    const uvs = new Float32Array(puppet.Uvs);
    const indices = new Uint16Array(puppet.Indices);
    const boneIdx = new Float32Array(vertexCount * 4);
    const boneWgt = new Float32Array(vertexCount * 4);

    const boneCount = Math.min(puppet.Bones.length, PUPPET_BONE_LIMIT);

    // 骨骼索引钳位:WE 的 blendindices 以 f32 编码,但单骨骼对象的值是 1.0
    // (多骨骼对象为 0/1/2…),语义疑似 1-based 或复用权重位模式;索引越界会让
    // uniform 数组采样未定义、顶点乱飞,这里统一钳到 [0, boneCount-1]
    for (let i = 0; i < vertexCount * 4; i++) {
      const raw = puppet.BlendIndex[i] ?? 0;

      boneIdx[i] = Math.min(boneCount - 1, Math.max(0, Math.round(raw) || 0));
    }
    boneWgt.set(puppet.BlendWeight.slice(0, vertexCount * 4));
    // 防御：后端数组短于 vertexCount*4 时剩余权重为 0，LBS 求和为 0 会让
    // 顶点塌到原点——对全零权重的顶点兜底绑到 0 号骨骼（索引已钳位，安全）
    for (let v = 0; v < vertexCount; v++) {
      const o = v * 4;

      if (
        boneWgt[o] === 0 &&
        boneWgt[o + 1] === 0 &&
        boneWgt[o + 2] === 0 &&
        boneWgt[o + 3] === 0
      ) {
        boneWgt[o] = 1;
      }
    }
    geometry.setAttribute("position", new THREE.BufferAttribute(positions, 3));
    geometry.setAttribute("uv", new THREE.BufferAttribute(uvs, 2));
    geometry.setAttribute("aBoneIdx", new THREE.BufferAttribute(boneIdx, 4));
    geometry.setAttribute("aBoneWgt", new THREE.BufferAttribute(boneWgt, 4));
    geometry.setIndex(new THREE.BufferAttribute(indices, 1));

    const restInv: THREE.Matrix4[] = [];
    const restMatrices: THREE.Matrix4[] = [];
    const boneAnim: THREE.Matrix4[] = [];
    const scratch: THREE.Matrix4[] = [];

    for (let i = 0; i < PUPPET_BONE_LIMIT; i++) {
      boneAnim.push(new THREE.Matrix4());
      scratch.push(new THREE.Matrix4());
      if (i < boneCount) {
        const rest = new THREE.Matrix4().fromArray(puppet.Bones[i].Rest);

        restMatrices.push(rest);
        restInv.push(rest.clone().invert());
      } else {
        restMatrices.push(new THREE.Matrix4());
        restInv.push(new THREE.Matrix4());
      }
    }

    const state: PuppetSkinState = {
      restInv,
      rest: restMatrices,
      tracks: puppet.Animation?.Tracks ?? null,
      duration: Math.max(0.01, puppet.Animation?.Duration ?? 1),
      parents: puppet.Bones.map((bone) => bone.Parent),
      boneAnim: { value: boneAnim },
      // rest⁻¹ 也走 uniform(GLSL ES 对 const 数组初始化器的实现差异难排查,
      // uniform 与 uBoneAnim 同一条数据路径,行为可预期)
      boneRestInv: { value: restInv },
      scratch,
    };

    if (!skinEnabled) {
      // 对照实验模式:顶点原样上屏,不注入蒙皮
      material.customProgramCacheKey = () => "nya-puppet-raw";

      return { ...state, geometry };
    }
    material.onBeforeCompile = (shader) => {
      shader.uniforms.uBoneAnim = state.boneAnim;
      shader.uniforms.uBoneRestInv = state.boneRestInv;
      shader.vertexShader = shader.vertexShader
        .replace(
          "#include <common>",
          `#include <common>
attribute vec4 aBoneIdx;
attribute vec4 aBoneWgt;
uniform mat4 uBoneAnim[${PUPPET_BONE_LIMIT}];
uniform mat4 uBoneRestInv[${PUPPET_BONE_LIMIT}];`,
        )
        .replace(
          "#include <begin_vertex>",
          `#include <begin_vertex>
{
  ivec4 idx = ivec4(aBoneIdx);
  mat4 skin = aBoneWgt.x * (uBoneAnim[idx.x] * uBoneRestInv[idx.x])
            + aBoneWgt.y * (uBoneAnim[idx.y] * uBoneRestInv[idx.y])
            + aBoneWgt.z * (uBoneAnim[idx.z] * uBoneRestInv[idx.z])
            + aBoneWgt.w * (uBoneAnim[idx.w] * uBoneRestInv[idx.w]);
  transformed = (skin * vec4(position, 1.0)).xyz;
}`,
        );
    };
    // 蒙皮注入改变着色器,给 three 一个独立的程序缓存键,避免与普通
    // MeshBasicMaterial 的缓存 program 串用(那段没有 uBoneAnim uniform)
    material.customProgramCacheKey = () => "nya-puppet-skin";

    return { ...state, geometry };
  }

  /** 每帧采样动画轨道并写骨骼矩阵 uniform:T×R×S 局部变换 × 层级组合。 */
  private updatePuppetSkinning(node: LayerNode, time: number): void {
    const state = node.puppet;

    if (!state || !state.tracks) return;
    const t = time % state.duration;
    const frames = state.tracks[0]?.length ?? 0;

    if (frames === 0) return;
    const frameF = (t / state.duration) * (frames - 1);
    const i0 = Math.min(frames - 1, Math.floor(frameF));
    const i1 = Math.min(frames - 1, i0 + 1);
    const blend = frameF - i0;

    for (let b = 0; b < state.restInv.length; b++) {
      const track = state.tracks[b];

      if (!track || track.length < 9) {
        // 有动画但该骨缺轨道:保持绑定姿势(rest),顶点不动;
        // 置单位阵会被 rest⁻¹ 抵消出错误偏移
        state.boneAnim.value[b].copy(state.rest[b]);
        continue;
      }
      const base = i0 * 9;
      const next = i1 * 9;
      const lerp = (offset: number): number =>
        track[base + offset] +
        (track[next + offset] - track[base + offset]) * blend;

      // T×R×S(欧拉 zyx 与 WE 导出样本一致;2D 壁纸绝大多数只有 z 分量)
      const euler = new THREE.Euler(lerp(3), lerp(4), lerp(5), "ZYX");
      const local = state.scratch[b];

      local.makeRotationFromEuler(euler);
      local.scale(new THREE.Vector3(lerp(6), lerp(7), lerp(8)));
      local.setPosition(lerp(0), lerp(1), lerp(2));

      // 层级组合:世界 = 父世界 × 局部(样本里 parent 全 -1,组合退化为恒等)
      const parentIndex = state.parents[b] ?? -1;
      const world = state.boneAnim.value[b];

      if (
        parentIndex >= 0 &&
        parentIndex < b &&
        parentIndex < PUPPET_BONE_LIMIT
      ) {
        world.multiplyMatrices(state.boneAnim.value[parentIndex], local);
      } else {
        world.copy(local);
      }
    }
  }

  private makeRT(width: number, height: number): THREE.WebGLRenderTarget {
    const target = new THREE.WebGLRenderTarget(width, height, {
      minFilter: THREE.LinearFilter,
      magFilter: THREE.LinearFilter,
      wrapS: THREE.RepeatWrapping,
      wrapT: THREE.RepeatWrapping,
      depthBuffer: false,
    });

    // RT 保存线性值:图像纹理已按 SRGB 解码,输出时统一编码
    return target;
  }

  private buildTextLayer(
    object: WESceneObject,
    group: THREE.Group,
    properties: Record<string, WEScenePayload["GeneralProperties"][string]>,
  ): void {
    const size = parseVec2(object.size ?? "", [400, 160]);
    const canvas = document.createElement("canvas");

    canvas.width = Math.min(2048, Math.max(64, Math.round(size[0])));
    canvas.height = Math.min(1024, Math.max(32, Math.round(size[1])));
    const texture = new THREE.CanvasTexture(canvas);

    texture.colorSpace = THREE.SRGBColorSpace;
    texture.flipY = false; // y 向下相机
    texture.minFilter = THREE.LinearFilter;
    const material = new THREE.MeshBasicMaterial({
      map: texture,
      transparent: true,
      side: THREE.DoubleSide,
      depthTest: false,
      depthWrite: false,
    });
    const mesh = new THREE.Mesh(new THREE.PlaneGeometry(1, 1), material);

    mesh.scale.set(canvas.width, canvas.height, 1);
    group.add(mesh);

    const rawText = resolveBound<string>(object.text?.value, properties);
    const initialText = typeof rawText === "string" ? rawText : "";
    const host = {
      text: initialText,
      userProperties: Object.fromEntries(
        Object.entries(properties).map(([key, value]) => [key, value.value]),
      ),
    };
    const script = compileTextScript(object.text?.script, host);

    const node: TextNode = {
      object,
      group,
      mesh,
      material,
      canvas,
      texture,
      script,
      host,
      parallax: this.makeParallax(object),
      basePosition: group.position.clone(),
      lastScriptTick: -1,
    };

    this.texts.push(node);
    this.drawText(node);

    // 文本层双通道显示:canvas 纹理进 three 之外,同步挂一个 HTML overlay
    // (由 drawText→syncTextOverlay 按 sceneRoot 变换把 canvas 内容投射到屏幕)。
    // 部分生产环境(WebView2 + 生产包)里 three 的 CanvasTexture 上屏路径不可靠,
    // 文本会整体消失;overlay 用纯 DOM,绕开该问题。
    const overlayCanvas = document.createElement("canvas");

    overlayCanvas.width = canvas.width;
    overlayCanvas.height = canvas.height;
    overlayCanvas.className = "we-scene-text-overlay";
    overlayCanvas.style.position = "absolute";
    overlayCanvas.style.pointerEvents = "none";
    overlayCanvas.style.left = "0";
    overlayCanvas.style.top = "0";
    node.overlayCanvas = overlayCanvas;
    node.overlay2d = overlayCanvas.getContext("2d");
    this.overlayHost.appendChild(overlayCanvas);
    this.syncTextOverlay(node);

    // 字体异步就绪后重绘一次
    if (object.font) {
      const family = `WEFont_${object.id}`;

      void this.assets
        .ensureFont(object.font, family)
        .then(() => this.drawText(node));
    }
  }

  private drawText(node: TextNode): void {
    const { canvas, object } = node;
    const context = canvas.getContext("2d");

    if (!context) return;
    context.clearRect(0, 0, canvas.width, canvas.height);
    const pointSize = object.pointsize ?? 24;
    const family = object.font
      ? `WEFont_${object.id}, sans-serif`
      : "sans-serif";

    context.font = `${pointSize * 2}px ${family}`;
    context.textAlign = (object.horizontalalign ?? "center") as CanvasTextAlign;
    context.textBaseline =
      (object.verticalalign ?? "center") === "center" ? "middle" : "top";
    if (object.opaquebackground) {
      context.fillStyle = "rgba(0,0,0,0.8)";
      context.fillRect(0, 0, canvas.width, canvas.height);
    }
    const [r, g, b] = parseColor(object.color ?? "1 1 1", [1, 1, 1, 1]);

    context.fillStyle = `rgb(${Math.round(r * 255)},${Math.round(g * 255)},${Math.round(b * 255)})`;
    const lines = node.host.text.split("\n");
    const lineHeight = pointSize * 2 * 1.25;
    const startY =
      object.verticalalign === "top"
        ? lineHeight / 2
        : canvas.height / 2 - ((lines.length - 1) * lineHeight) / 2;

    lines.forEach((line, index) => {
      context.fillText(line, canvas.width / 2, startY + index * lineHeight);
    });
    node.texture.needsUpdate = true;
    this.syncTextOverlay(node);
  }

  /** 把文本 canvas 投射到 HTML overlay(镜像 drawText 的结果 + 场景变换)。 */
  private syncTextOverlay(node: TextNode): void {
    const overlay = node.overlayCanvas;
    const ctx2d = node.overlay2d;

    if (!overlay || !ctx2d) return;
    ctx2d.clearRect(0, 0, overlay.width, overlay.height);
    ctx2d.drawImage(node.canvas, 0, 0);
    // 场景变换:sceneRoot(cover 缩放+居中) × group(origin) × mesh(canvas 尺寸)
    const root = this.sceneRoot;
    const g = node.group;
    const scale = root.scale.x;
    const x =
      root.position.x +
      g.position.x * scale -
      (node.mesh.scale.x * g.scale.x * scale) / 2;
    const y =
      root.position.y +
      g.position.y * scale -
      (node.mesh.scale.y * g.scale.y * scale) / 2;

    overlay.style.left = x + "px";
    overlay.style.top = y + "px";
    overlay.style.width = node.mesh.scale.x * g.scale.x * scale + "px";
    overlay.style.height = node.mesh.scale.y * g.scale.y * scale + "px";
  }

  private async buildParticleLayer(
    object: WESceneObject,
    group: THREE.Group,
    _properties: Record<string, WEScenePayload["GeneralProperties"][string]>,
  ): Promise<void> {
    const definition = await this.assets.json<import("./types").WEParticleFile>(
      object.particle!,
    );

    if (!definition?.material) return;
    const material = await this.assets.json<{
      passes?: { textures?: string[] }[];
    }>(definition.material);
    const textureName = material?.passes?.[0]?.textures?.[0];
    const texture = textureName ? await this.assets.texture(textureName) : null;

    if (!texture) return;

    const system = new ParticleSystem(definition, texture);

    group.add(system.mesh);
    const override = object.instanceoverride;

    if (typeof override?.alpha === "number") {
      // 实例覆盖 alpha:给着色器加全局透明度
      (system.mesh.material as THREE.ShaderMaterial).uniforms.uGlobalAlpha = {
        value: override.alpha,
      };
    }
    this.particleNodes.push({
      object,
      group,
      system,
      parallax: this.makeParallax(object),
      basePosition: group.position.clone(),
    });
  }

  // ---- 帧循环 ----

  /** 最近若干帧的实际到达间隔(rAF 回调间距反映合成器压力) */
  private frameGaps: number[] = [];
  /** 0 = 不限帧;>0 = 两次渲染的最小间隔(自适应降帧后锁定) */
  private frameCapMs = 0;
  private lastFrameAt = 0;
  private lastRenderAt = 0;

  private loop = (): void => {
    if (this.disposed) return;
    this.rafId = requestAnimationFrame(this.loop);
    if (document.hidden) return;

    const now = performance.now();

    // 自适应限帧:统计实际帧间距,持续掉出 60fps 就锁 30fps(不再回升,
    // 避免在阈值附近反复横跳造成节奏抖动)
    if (this.lastFrameAt > 0) {
      const gap = now - this.lastFrameAt;

      this.frameGaps.push(gap);
      if (this.frameGaps.length > FRAME_SAMPLES) this.frameGaps.shift();
      if (
        this.frameCapMs === 0 &&
        this.frameGaps.length === FRAME_SAMPLES &&
        this.frameGaps.filter((g) => g > FRAME_SLOW_MS).length >
          FRAME_SAMPLES / 2
      ) {
        this.frameCapMs = FRAME_CAPPED_MS;
      }
    }
    this.lastFrameAt = now;
    if (this.frameCapMs > 0 && now - this.lastRenderAt < this.frameCapMs - 2)
      return;
    this.lastRenderAt = now;

    const dt = Math.min(this.clock.getDelta(), 0.1);

    this.sceneTime += dt;

    // 视差平滑
    const blend = Math.min(1, dt * PARALLISMOOTHING);

    this.pointerSmoothed[0] +=
      (this.pointer[0] - this.pointerSmoothed[0]) * blend;
    this.pointerSmoothed[1] +=
      (this.pointer[1] - this.pointerSmoothed[1]) * blend;
    // 视差:WE 的 parallaxDepth 是深度百分数(编辑器滑杆 0~100),
    // 位移 = 深度% × 窗口尺寸 × 鼠标归一偏移(-1..1)
    const canvasEl = this.renderer.domElement;
    const amountX = canvasEl.clientWidth || window.innerWidth;
    const amountY = canvasEl.clientHeight || window.innerHeight;
    const applyParallax = (node: {
      parallax: [number, number];
      basePosition: THREE.Vector3;
      group: THREE.Group;
    }) => {
      if (node.parallax[0] === 0 && node.parallax[1] === 0) return;
      node.group.position.set(
        node.basePosition.x -
          (this.pointerSmoothed[0] * node.parallax[0] * amountX) / 100,
        node.basePosition.y -
          (this.pointerSmoothed[1] * node.parallax[1] * amountY) / 100,
        node.basePosition.z,
      );
    };

    for (const layer of this.layers) applyParallax(layer);
    for (const text of this.texts) applyParallax(text);
    for (const particle of this.particleNodes) applyParallax(particle);

    // 粒子推进
    for (const node of this.particleNodes) {
      const origin = parseVec2(node.object.origin ?? "0 0", [0, 0]);

      node.system.update(dt, this.sceneTime, origin);
    }

    // puppet 蒙皮:采样骨骼动画轨道(在视差/效果链之前,uniform 当帧生效)
    for (const layer of this.layers) {
      if (layer.puppet) {
        if ((window as { nyaSkinOff?: boolean }).nyaSkinOff) {
          // 对照模式:蒙皮公式是 uBoneAnim × rest⁻¹,置单位阵会把顶点乘上
          // rest⁻¹(千像素级平移)导致整体扭曲;置 rest 才是"不做动画"的原样
          const { rest } = layer.puppet;

          for (let b = 0; b < layer.puppet.boneAnim.value.length; b++) {
            layer.puppet.boneAnim.value[b].copy(rest[b]);
          }
        } else {
          this.updatePuppetSkinning(layer, this.sceneTime);
        }
        // y 镜像只在骨骼矩阵就绪后生效;材质在 buildImageLayer 里已设
        if (layer.mesh.scale.y === 1) layer.mesh.scale.y = -1;
      }
    }

    // 效果链重绘
    for (const layer of this.layers) {
      if (
        layer.effectPasses.length === 0 ||
        !layer.rtA ||
        !layer.rtB ||
        !layer.baseTexture
      )
        continue;
      let source: THREE.Texture = layer.baseTexture;
      let target = layer.rtA;

      for (const pass of layer.effectPasses) {
        pass.update(this.sceneTime);
        blitPass(this.renderer, pass.material, source, target, this.blit.quad);
        source = target.texture;
        target = target === layer.rtA ? layer.rtB : layer.rtA;
      }
      // 最终输出在"最后一个写入的目标"里;两个 RT 交替,最终的 map 指向最后产物
      const finalTarget =
        layer.effectPasses.length % 2 === 1 ? layer.rtA : layer.rtB;

      if (layer.material.map !== finalTarget.texture) {
        layer.material.map = finalTarget.texture;
        layer.material.needsUpdate = true;
      }
    }

    // 文本脚本 1Hz
    for (const node of this.texts) {
      if (!node.script) continue;
      const tick = Math.floor(this.sceneTime);

      if (tick !== node.lastScriptTick) {
        node.lastScriptTick = tick;
        const before = node.host.text;

        try {
          node.script.update();
        } catch {
          // 脚本抛错按当前文本继续
        }
        if (node.host.text !== before) this.drawText(node);
      }
    }

    this.renderer.render(this.scene, this.camera);

    if (!this.firstFrameSent) {
      this.firstFrameSent = true;
      this.options.onFirstFrame?.();
    }
  };

  // ---- 窗口与尺寸 ----

  private attachWindowListeners(): void {
    // 文本 overlay 宿主插到 canvas 的父级,与 WebGL canvas 同层
    const parent = this.options.canvas.parentElement;

    if (parent) {
      this.overlayHost.style.position = "absolute";
      this.overlayHost.style.inset = "0";
      this.overlayHost.style.overflow = "hidden";
      this.overlayHost.style.pointerEvents = "none";
      parent.appendChild(this.overlayHost);
    }
    this.pointerHandler = (event: PointerEvent) => {
      this.pointer[0] = (event.clientX / window.innerWidth) * 2 - 1;
      this.pointer[1] = (event.clientY / window.innerHeight) * 2 - 1;
    };
    window.addEventListener("pointermove", this.pointerHandler, {
      passive: true,
    });

    const canvas = this.options.canvas;

    if (typeof ResizeObserver !== "undefined" && canvas.parentElement) {
      this.resizeObserver = new ResizeObserver(() => this.resize());
      this.resizeObserver.observe(canvas.parentElement);
    }
    window.addEventListener("resize", this.resizeBound);
  }

  private resizeBound = (): void => this.resize();
  /** 文本 overlay 宿主:覆盖窗口的透明层,文本 canvas 以绝对定位放这里 */
  private overlayHost: HTMLDivElement = document.createElement("div");

  private resize(): void {
    const canvas = this.options.canvas;
    const parent = canvas.parentElement;
    const width = parent?.clientWidth || window.innerWidth;
    const height = parent?.clientHeight || window.innerHeight;

    if (width <= 0 || height <= 0) return;
    // 分辨率倍数默认 1(壁纸是背景艺术,HiDPI 按物理像素渲染意味着数倍
    // 像素量,重壁纸会拖垮整个 WebView 合成);想要精细可在设置里调高
    this.renderer.setPixelRatio(this.options.pixelRatio ?? 1);
    this.renderer.setSize(width, height, false);
    canvas.style.width = "100%";
    canvas.style.height = "100%";

    // 设计分辨率 → 窗口:等比 cover + 居中
    const designWidth = this.options.payload.DesignWidth || 1920;
    const designHeight = this.options.payload.DesignHeight || 1080;
    const scale = Math.max(width / designWidth, height / designHeight);

    this.camera.left = 0;
    this.camera.right = width;
    this.camera.top = 0;
    this.camera.bottom = height;
    this.camera.updateProjectionMatrix();
    this.sceneRoot.scale.set(scale, scale, 1);
    this.sceneRoot.position.set(
      (width - designWidth * scale) / 2,
      (height - designHeight * scale) / 2,
      0,
    );
    // 窗口变化后文本 overlay 跟随场景变换
    for (const node of this.texts) this.syncTextOverlay(node);
  }

  dispose(): void {
    if (this.disposed) return;
    this.disposed = true;
    cancelAnimationFrame(this.rafId);
    if (this.pointerHandler)
      window.removeEventListener("pointermove", this.pointerHandler);
    window.removeEventListener("resize", this.resizeBound);
    this.resizeObserver?.disconnect();
    this.overlayHost.remove();
    for (const layer of this.layers) {
      layer.rtA?.dispose();
      layer.rtB?.dispose();
      layer.mesh.geometry.dispose();
      layer.material.dispose();
      for (const pass of layer.effectPasses) pass.dispose();
    }
    for (const text of this.texts) {
      text.mesh.geometry.dispose();
      text.material.dispose();
      text.texture.dispose();
    }
    for (const node of this.particleNodes) node.system.dispose();
    this.blit.quad.geometry.dispose();
    this.assets.dispose();
    this.white.dispose();
    this.renderer.dispose();
  }
}
