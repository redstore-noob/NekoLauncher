/**
 * WE 粒子系统引擎(子集实现)。
 *
 * 粒子定义(particles/xxx.json)分四段:
 *   emitter[]     发射器:位置/速率/半径——这里支持 sphererandom(球内随机),
 *                 其余类型退化为点发射
 *   initializer[] 初始化:寿命/尺寸/速度/颜色/旋转/湍流(带 random 后缀的
 *                 表示在 [min,max] 区间均匀采样)
 *   operator[]    逐帧算子:movement(重力)、angularmovement(旋转)、
 *                 alphafade(淡入淡出)
 *   renderer[]    渲染器:sprite(贴图四边形);sprite sheet 序列帧不解析
 *
 * 更新在 CPU(粒子量级 ~10²-10³),渲染走实例化四边形,单 draw call。
 * 坐标系:发射器 origin 是相对粒子对象 origin 的设计像素偏移;
 * 速度/重力单位是像素每秒。
 */

import type { WEParticleFile } from "./types";

import * as THREE from "three";

import { parseVec } from "./values";

interface Particle {
  active: boolean;
  x: number;
  y: number;
  vx: number;
  vy: number;
  life: number; // 总寿命(秒)
  age: number; // 已存活
  size: number;
  rotation: number;
  angularVelocity: number;
  color: [number, number, number];
  alphaScale: number; // 初始 alpha(颜色随机可能带)
}

/** 从初始化器/算子里按名字取参数对象 */
function byName(
  list: Record<string, unknown>[] | undefined,
  name: string,
): Record<string, unknown> | null {
  for (const entry of list ?? []) {
    if ((entry as { name?: string }).name === name) return entry;
  }

  return null;
}

function rand(min: number, max: number): number {
  return min + Math.random() * (max - min);
}

/** "255 255 255"(0-255)→ [r,g,b](0-1) */
function particleColor(raw: unknown): [number, number, number] {
  const v = parseVec(raw, [255, 255, 255]);

  return [v[0] / 255, v[1] / 255, v[2] / 255];
}

/** 实例化渲染用着色器:每粒子 位置/尺寸/旋转/颜色 */
const PARTICLE_VERTEX = /* glsl */ `
attribute vec2 iPosition;
attribute float iScale;
attribute float iRotation;
attribute vec4 iColor;
varying vec2 vUv;
varying vec4 vColor;
void main() {
  vec2 corner = position.xy * iScale;
  float c = cos(iRotation), s = sin(iRotation);
  vec2 rotated = vec2(corner.x * c - corner.y * s, corner.x * s + corner.y * c);
  vUv = uv;
  vColor = iColor;
  gl_Position = projectionMatrix * modelViewMatrix * vec4(iPosition + rotated, 0.0, 1.0);
}
`;
const PARTICLE_FRAGMENT = /* glsl */ `
uniform sampler2D uMap;
uniform float uGlobalAlpha;
varying vec2 vUv;
varying vec4 vColor;
void main() {
  gl_FragColor = texture2D(uMap, vUv) * vColor * uGlobalAlpha;
}
`;

/** 单个粒子系统的运行时 */
export class ParticleSystem {
  readonly mesh: THREE.Mesh;
  private particles: Particle[] = [];
  private emitAccumulator = 0;
  private started = false;
  private startTime: number;
  private gravity: [number, number] = [0, 0];
  private fadein = 0;
  private fadeout = 0;
  private turbulentScale = 0;
  private turbulentSpeed = 0;
  private maxCount: number;

  // 实例属性缓冲
  private positionAttr!: THREE.InstancedBufferAttribute;
  private scaleAttr!: THREE.InstancedBufferAttribute;
  private rotationAttr!: THREE.InstancedBufferAttribute;
  private colorAttr!: THREE.InstancedBufferAttribute;
  private material: THREE.ShaderMaterial;
  private geometry: THREE.InstancedBufferGeometry;

  constructor(
    private definition: WEParticleFile,
    texture: THREE.Texture,
  ) {
    this.maxCount = Math.min(definition.maxcount ?? 100, 2000);
    this.startTime = definition.starttime ?? 0;
    this.fadein = Number(
      byName(definition.operator, "alphafade")?.fadeintime ?? 0,
    );
    this.fadeout = Number(
      byName(definition.operator, "alphafade")?.fadeouttime ?? 0,
    );
    const movement = byName(definition.operator, "movement");

    if (movement) {
      const g = parseVec(movement.gravity, [0, 0, 0]);

      this.gravity = [g[0], g[1]]; // 渲染坐标系同为 y 向下,直接使用
    }
    const turbulent = byName(definition.initializer, "turbulentvelocityrandom");

    if (turbulent) {
      this.turbulentScale = Number(turbulent.scale ?? 0.5);
      this.turbulentSpeed = Number(turbulent.speedmax ?? 100);
    }

    this.particles = Array.from({ length: this.maxCount }, () => ({
      active: false,
      x: 0,
      y: 0,
      vx: 0,
      vy: 0,
      life: 1,
      age: 0,
      size: 1,
      rotation: 0,
      angularVelocity: 0,
      color: [1, 1, 1] as [number, number, number],
      alphaScale: 1,
    }));

    const quad = new THREE.PlaneGeometry(1, 1); // 单位四边形,尺寸走 iScale

    this.geometry = new THREE.InstancedBufferGeometry();
    this.geometry.index = quad.index;
    this.geometry.setAttribute("position", quad.getAttribute("position"));
    this.geometry.setAttribute("uv", quad.getAttribute("uv"));
    const zeros = (size: number) => new Float32Array(this.maxCount * size);

    this.positionAttr = new THREE.InstancedBufferAttribute(zeros(2), 2);
    this.scaleAttr = new THREE.InstancedBufferAttribute(zeros(1), 1);
    this.rotationAttr = new THREE.InstancedBufferAttribute(zeros(1), 1);
    this.colorAttr = new THREE.InstancedBufferAttribute(zeros(4), 4);
    this.geometry.setAttribute("iPosition", this.positionAttr);
    this.geometry.setAttribute("iScale", this.scaleAttr);
    this.geometry.setAttribute("iRotation", this.rotationAttr);
    this.geometry.setAttribute("iColor", this.colorAttr);
    this.geometry.instanceCount = this.maxCount;

    this.material = new THREE.ShaderMaterial({
      vertexShader: PARTICLE_VERTEX,
      fragmentShader: PARTICLE_FRAGMENT,
      uniforms: { uMap: { value: texture }, uGlobalAlpha: { value: 1 } },
      transparent: true,
      side: THREE.DoubleSide, // 镜像投影下绕序翻转,双面渲染
      depthTest: false,
      depthWrite: false,
    });
    this.mesh = new THREE.Mesh(this.geometry, this.material);
    this.mesh.frustumCulled = false;
  }

  /** 逐帧推进;time 为场景时钟(秒),emitterOrigin 为对象 origin(x,y)。 */
  update(dt: number, time: number, emitterOrigin: [number, number]): void {
    if (!this.started && time >= this.startTime) this.started = true;
    if (!this.started) return;

    // 发射
    const emitter = this.definition.emitter?.[0];

    if (emitter) {
      const rate = Number(emitter.rate ?? 5);

      this.emitAccumulator += rate * dt;
      while (this.emitAccumulator >= 1) {
        this.emitAccumulator -= 1;
        this.spawn(emitter, emitterOrigin);
      }
    }

    // 推进
    for (const particle of this.particles) {
      if (!particle.active) continue;
      particle.age += dt;
      if (particle.age >= particle.life) {
        particle.active = false;
        continue;
      }
      if (this.turbulentScale > 0) {
        // 简化的平稳湍流:两个不同频率正弦合成伪噪声力
        const t = time * this.turbulentSpeed * 0.01;

        particle.vx +=
          Math.sin(particle.y * 0.01 + t * 1.3) * this.turbulentScale * 40 * dt;
        particle.vy +=
          Math.cos(particle.x * 0.01 + t * 1.7) * this.turbulentScale * 40 * dt;
      }
      particle.vx += this.gravity[0] * dt;
      particle.vy += this.gravity[1] * dt;
      particle.x += particle.vx * dt;
      particle.y += particle.vy * dt;
      particle.rotation += particle.angularVelocity * dt;
    }

    this.writeAttributes();
  }

  private spawn(
    emitter: Record<string, unknown>,
    emitterOrigin: [number, number],
  ): void {
    const dead = this.particles.find((p) => !p.active);

    if (!dead) return;

    // 位置:sphererandom 在 [distancemin, distancemax] 球内取点;其余按原点
    const origin = parseVec(emitter.origin, [0, 0, 0]);
    let dx = origin[0],
      dy = origin[1];
    const name = String(emitter.name ?? "");

    if (name === "sphererandom" || name === "shellsphere") {
      const min = Number(emitter.distancemin ?? 0);
      const max = Number(emitter.distancemax ?? min);
      const angle = Math.random() * Math.PI * 2;
      const distance = name === "shellsphere" ? max : rand(min, max);

      dx += Math.cos(angle) * distance;
      dy += Math.sin(angle) * distance;
    }
    dead.active = true;
    dead.age = 0;
    dead.x = emitterOrigin[0] + dx;
    dead.y = emitterOrigin[1] + dy;

    const lifetime = byName(this.definition.initializer, "lifetimerandom");

    dead.life = lifetime
      ? rand(Number(lifetime.min ?? 1), Number(lifetime.max ?? 1))
      : 5;

    const size = byName(this.definition.initializer, "sizerandom");

    dead.size = size
      ? rand(Number(size.min ?? 10), Number(size.max ?? 10))
      : 10;

    const velocity = byName(this.definition.initializer, "velocityrandom");

    if (velocity) {
      const vmin = parseVec(velocity.min, [0, 0, 0]);
      const vmax = parseVec(velocity.max, vmin);

      dead.vx = rand(vmin[0], vmax[0]);
      dead.vy = rand(vmin[1], vmax[1]); // 坐标系同为 y 向下,直接使用
    } else {
      dead.vx = 0;
      dead.vy = 0;
    }

    const color = byName(this.definition.initializer, "colorrandom");

    if (color) {
      dead.color = particleColor(color.min);
      dead.alphaScale = parseVec(color.min, [255, 255, 255, 255])[3] / 255;
    } else {
      dead.color = [1, 1, 1];
      dead.alphaScale = 1;
    }

    dead.rotation = Math.random() * Math.PI * 2;
    const angular = byName(
      this.definition.initializer,
      "angularvelocityrandom",
    );

    if (angular) {
      const amin = parseVec(angular.min, [0, 0, 0]);
      const amax = parseVec(angular.max, amin);

      // z 分量是绕屏幕法线的角速度
      dead.angularVelocity = rand(amin[2], amax[2]);
    } else {
      dead.angularVelocity = 0;
    }
  }

  /** 把粒子状态写进实例属性(隐藏粒子缩放为 0)。 */
  private writeAttributes(): void {
    const positions = this.positionAttr.array as Float32Array;
    const scales = this.scaleAttr.array as Float32Array;
    const rotations = this.rotationAttr.array as Float32Array;
    const colors = this.colorAttr.array as Float32Array;

    for (let i = 0; i < this.particles.length; i++) {
      const particle = this.particles[i];
      let alpha = particle.active ? particle.alphaScale : 0;

      if (particle.active) {
        if (this.fadein > 0 && particle.age < this.fadein)
          alpha *= particle.age / this.fadein;
        const remaining = particle.life - particle.age;

        if (this.fadeout > 0 && remaining < this.fadeout)
          alpha *= remaining / this.fadeout;
      }
      positions[i * 2] = particle.x;
      positions[i * 2 + 1] = particle.y;
      scales[i] = particle.active ? particle.size : 0;
      rotations[i] = particle.rotation;
      colors[i * 4] = particle.color[0];
      colors[i * 4 + 1] = particle.color[1];
      colors[i * 4 + 2] = particle.color[2];
      colors[i * 4 + 3] = alpha;
    }
    this.positionAttr.needsUpdate = true;
    this.scaleAttr.needsUpdate = true;
    this.rotationAttr.needsUpdate = true;
    this.colorAttr.needsUpdate = true;
  }

  dispose(): void {
    this.geometry.dispose();
    this.material.dispose();
  }
}
