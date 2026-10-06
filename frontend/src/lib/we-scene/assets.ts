/**
 * 场景壁纸资源加载器:统一封装 /wescene 路由的访问。
 *
 * 后端只暴露两类端点:
 *   /wescene/pkg/<包内路径>   原始文件(JSON/字体)
 *   /wescene/tex/<包内路径>   解码后的贴图(PNG/JPEG)与视频纹理(MP4)
 * 材质里的贴图引用是不带目录与扩展名的名字("1760560458885"),实际条目在
 * materials/ 下带 .tex 后缀;效果槽位引用("masks/xxx")同理。这里按
 * 候选顺序逐个尝试,命中即缓存。
 */

import type { WEModelFile, WEMaterialFile } from "./types";

import * as THREE from "three";

/** 名字 → 包内候选路径(materials/ 前缀与 .tex 后缀的常见组合)。
 * 材质里的贴图引用不带目录与扩展名,实际条目在 materials/ 下;带子目录的
 * 引用(masks/xxx、workshop/…/yyy)实际条目也在 materials/ 之下。 */
export function textureCandidates(name: string): string[] {
  const clean = name.replace(/\\/g, "/").replace(/^\/+/, "");
  const hasTex = clean.toLowerCase().endsWith(".tex");
  const hasDir = clean.includes("/");
  const out: string[] = [];
  const push = (value: string) => {
    if (!out.includes(value)) out.push(value);
  };

  if (hasTex) {
    push(clean);
    if (!hasDir) push(`materials/${clean}`);
  } else {
    if (!hasDir) push(`materials/${clean}.tex`);
    push(`${clean}.tex`);
    // 带目录的引用优先原样补 .tex,再试挂到 materials/ 下
    if (hasDir) push(`materials/${clean}.tex`);
    push(clean);
    if (!hasDir) push(`materials/${clean}`);
  }

  return out;
}

/** 场景资源加载器:贴图/JSON/字体带缓存,失败逐候选回退。 */
export class SceneAssets {
  private textureCache = new Map<string, THREE.Texture>();
  private jsonCache = new Map<string, unknown>();
  private pendingTexture = new Map<string, Promise<THREE.Texture | null>>();
  private loadedFonts = new Set<string>();

  /** 按候选路径加载贴图;全部候选 404 时返回 null。 */
  texture(name: string): Promise<THREE.Texture | null> {
    const cacheKey = name.toLowerCase();
    const cached = this.textureCache.get(cacheKey);

    if (cached) return Promise.resolve(cached);
    const inflight = this.pendingTexture.get(cacheKey);

    if (inflight) return inflight;

    const promise = this.tryLoadTexture(textureCandidates(name));

    this.pendingTexture.set(cacheKey, promise);

    return promise.then((texture) => {
      this.pendingTexture.delete(cacheKey);
      if (texture) this.textureCache.set(cacheKey, texture);

      return texture;
    });
  }

  private async tryLoadTexture(
    candidates: string[],
  ): Promise<THREE.Texture | null> {
    for (const candidate of candidates) {
      const url = `/wescene/tex/${encodeURIComponent(candidate).replace(/%2F/gi, "/")}`;

      try {
        return await this.loadTextureFromUrl(url);
      } catch {
        // 图像加载失败:可能是 MP4 视频纹理,换 video 元素再试
      }
      try {
        return await this.loadVideoTexture(url);
      } catch {
        // 尝试下一个候选
      }
    }

    return null;
  }

  /** 视频纹理(WE 的视频贴图,FIF_MP4):静音循环播放 */
  private loadVideoTexture(url: string): Promise<THREE.Texture> {
    return new Promise((resolve, reject) => {
      const video = document.createElement("video");

      video.src = url;
      video.muted = true;
      video.loop = true;
      video.playsInline = true;
      video.crossOrigin = "anonymous";
      const fail = () => reject(new Error(`视频纹理加载失败: ${url}`));

      video.onerror = fail;
      video.onloadeddata = () => {
        void video.play().catch(() => undefined);
        const texture = new THREE.VideoTexture(video);

        texture.colorSpace = THREE.SRGBColorSpace;
        texture.flipY = false;
        texture.minFilter = THREE.LinearFilter;
        resolve(texture);
      };
      video.load();
    });
  }

  private loadTextureFromUrl(url: string): Promise<THREE.Texture> {
    return new Promise((resolve, reject) => {
      const image = new Image();

      image.onload = () => {
        const texture = new THREE.Texture(image);

        texture.colorSpace = THREE.SRGBColorSpace;
        texture.flipY = false; // 渲染器相机 y 向下,不翻转
        texture.minFilter = THREE.LinearFilter;
        texture.magFilter = THREE.LinearFilter;
        texture.generateMipmaps = false;
        texture.needsUpdate = true;
        resolve(texture);
      };
      image.onerror = () => reject(new Error(`贴图加载失败:${url}`));
      image.src = url;
    });
  }

  /** 加载包内 JSON(带缓存);失败返回 null。 */
  async json<T>(pkgPath: string): Promise<T | null> {
    const cacheKey = pkgPath.toLowerCase();
    const cached = this.jsonCache.get(cacheKey);

    if (cached !== undefined) return cached as T;
    try {
      const response = await fetch(
        `/wescene/pkg/${pkgPath.replace(/\\/g, "/")}`,
      );

      if (!response.ok) return null;
      const data = (await response.json()) as T;

      this.jsonCache.set(cacheKey, data);

      return data;
    } catch {
      return null;
    }
  }

  /** models/xxx.json → materials/xxx.json → 贴图名列表。 */
  async materialTextures(imageRef: string): Promise<string[]> {
    // WE 引擎内建的纯色图层,包里没有对应文件:渲染器用白纹理特判
    if (imageRef.replace(/\\/g, "/").endsWith("util/solidlayer.json"))
      return [];
    const model = await this.json<WEModelFile>(imageRef);

    if (!model?.material) return [];
    const material = await this.json<WEMaterialFile>(model.material);

    // 着色器 genericimage*/genericsolid 的贴图都在第一 pass
    return material?.passes?.[0]?.textures ?? [];
  }

  /** 字体文件加载为 FontFace(去重);失败静默(回退系统字体)。 */
  async ensureFont(pkgPath: string, family: string): Promise<void> {
    const key = pkgPath.toLowerCase();

    if (this.loadedFonts.has(key)) return;
    try {
      const response = await fetch(
        `/wescene/pkg/${pkgPath.replace(/\\/g, "/")}`,
      );

      if (!response.ok) return;
      const buffer = await response.arrayBuffer();
      const face = new FontFace(family, buffer);

      await face.load();
      document.fonts.add(face);
      this.loadedFonts.add(key);
    } catch {
      // 字体缺失不致命
    }
  }

  dispose(): void {
    for (const texture of this.textureCache.values()) texture.dispose();
    this.textureCache.clear();
    this.jsonCache.clear();
  }
}

/** 1×1 白色纹理(solidlayer / 无贴图对象用),调用方负责 dispose。 */
export function whiteTexture(): THREE.Texture {
  const data = new Uint8Array([255, 255, 255, 255]);
  const texture = new THREE.DataTexture(data, 1, 1);

  texture.needsUpdate = true;

  return texture;
}
