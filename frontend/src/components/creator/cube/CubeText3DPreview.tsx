/*
 * Copyright 2026 烟花
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */
/*
 * 3D 体素文字预览：把文本体素化成方块（InstancedMesh），可选描边、渐变/彩虹着色、
 * 程序化纹理与背景；鼠标拖拽 / 滚轮由 OrbitControls 控制，支持截图导出。
 *
 * 灵感与功能对齐自 EaseCation/cube-3d-text（MIT）。
 */
import { forwardRef, useEffect, useImperativeHandle, useRef } from "react";
import * as THREE from "three";
import { OrbitControls } from "three/examples/jsm/controls/OrbitControls.js";

import { voxelizeText } from "./voxelize";

export interface CubeTextConfig {
  text: string;
  /** CSS font-family */
  font: string;
  bold: boolean;
  /** 每个方块占用的像素边长 */
  resolution: number;
  /** 方块厚度（Z 轴缩放） */
  depth: number;
  /** 方块之间的间隙比例 0~0.5 */
  spacing: number;
  colorMode: "solid" | "gradient" | "rainbow";
  colorA: string;
  colorB: string;
  texture: "none" | "noise" | "grid";
  outline: boolean;
  outlineColor: string;
  /** 背景色；null 表示透明 */
  background: string | null;
}

export interface CubeText3DHandle {
  /** 以当前视角渲染并返回 PNG data URI */
  screenshot: () => string;
  resetCamera: () => void;
}

interface CubeText3DPreviewProps {
  config: CubeTextConfig;
  autoRotate: boolean;
}

/** 程序化噪点纹理（近似石/土的斑驳感） */
function createNoiseTexture(): THREE.CanvasTexture {
  const size = 32;
  const canvas = document.createElement("canvas");

  canvas.width = size;
  canvas.height = size;
  const ctx = canvas.getContext("2d");

  if (ctx) {
    ctx.fillStyle = "#ffffff";
    ctx.fillRect(0, 0, size, size);
    for (let index = 0; index < size * size * 0.55; index += 1) {
      const shade = 200 + Math.floor(Math.random() * 56);
      const x = Math.floor(Math.random() * size);
      const y = Math.floor(Math.random() * size);

      ctx.fillStyle = `rgb(${shade},${shade},${shade})`;
      ctx.fillRect(x, y, 1, 1);
    }
  }
  const texture = new THREE.CanvasTexture(canvas);

  texture.magFilter = THREE.NearestFilter;
  texture.minFilter = THREE.NearestFilter;
  texture.wrapS = THREE.RepeatWrapping;
  texture.wrapT = THREE.RepeatWrapping;
  texture.repeat.set(2, 2);
  texture.colorSpace = THREE.SRGBColorSpace;

  return texture;
}

/** 程序化网格纹理（近似方块边框） */
function createGridTexture(): THREE.CanvasTexture {
  const size = 16;
  const canvas = document.createElement("canvas");

  canvas.width = size;
  canvas.height = size;
  const ctx = canvas.getContext("2d");

  if (ctx) {
    ctx.fillStyle = "#ffffff";
    ctx.fillRect(0, 0, size, size);
    ctx.strokeStyle = "rgba(0,0,0,0.22)";
    ctx.lineWidth = 1;
    ctx.strokeRect(0.5, 0.5, size - 1, size - 1);
  }
  const texture = new THREE.CanvasTexture(canvas);

  texture.magFilter = THREE.NearestFilter;
  texture.minFilter = THREE.NearestFilter;
  texture.colorSpace = THREE.SRGBColorSpace;

  return texture;
}

function cellColor(
  config: CubeTextConfig,
  gx: number,
  gy: number,
  cols: number,
  rows: number,
): THREE.Color {
  const color = new THREE.Color();

  if (config.colorMode === "gradient") {
    const t = rows > 1 ? gy / (rows - 1) : 0;

    return color.set(config.colorA).lerp(new THREE.Color(config.colorB), t);
  }
  if (config.colorMode === "rainbow") {
    const tx = cols > 1 ? gx / (cols - 1) : 0;
    const ty = rows > 1 ? gy / (rows - 1) : 0;

    return color.setHSL(((tx + ty) / 2) % 1, 0.85, 0.55);
  }

  return color.set(config.colorA);
}

const CubeText3DPreview = forwardRef<CubeText3DHandle, CubeText3DPreviewProps>(
  ({ config, autoRotate }, ref) => {
    const mountRef = useRef<HTMLDivElement>(null);
    const sceneRef = useRef<THREE.Scene | null>(null);
    const cameraRef = useRef<THREE.PerspectiveCamera | null>(null);
    const rendererRef = useRef<THREE.WebGLRenderer | null>(null);
    const controlsRef = useRef<OrbitControls | null>(null);
    const groupRef = useRef<THREE.Group | null>(null);
    const frameRef = useRef<number>(0);
    const builtRef = useRef(false);
    const modelSizeRef = useRef(20);

    const disposeGroup = () => {
      const group = groupRef.current;

      if (!group) return;
      for (const child of [...group.children]) {
        group.remove(child);
        const mesh = child as THREE.Mesh;

        mesh.geometry?.dispose();
        const materials = Array.isArray(mesh.material)
          ? mesh.material
          : [mesh.material];

        for (const material of materials) {
          if (material && "map" in material) {
            (material as THREE.MeshStandardMaterial).map?.dispose();
          }
          material?.dispose();
        }
      }
    };

    const fitCamera = () => {
      const camera = cameraRef.current;
      const controls = controlsRef.current;

      if (!camera || !controls) return;
      const size = Math.max(4, modelSizeRef.current);
      const fov = (camera.fov * Math.PI) / 180;
      const distance = (size / 2 / Math.tan(fov / 2)) * 1.35 + 5;

      camera.position.set(distance * 0.18, distance * 0.12, distance);
      controls.target.set(0, 0, 0);
      controls.update();
    };

    useEffect(() => {
      const mount = mountRef.current;

      if (!mount) return;

      const scene = new THREE.Scene();
      const camera = new THREE.PerspectiveCamera(
        42,
        mount.clientWidth / Math.max(1, mount.clientHeight),
        0.1,
        2000,
      );
      const renderer = new THREE.WebGLRenderer({
        alpha: true,
        antialias: true,
        preserveDrawingBuffer: true,
      });

      renderer.setPixelRatio(Math.min(window.devicePixelRatio, 2));
      renderer.setSize(mount.clientWidth, mount.clientHeight);
      renderer.outputColorSpace = THREE.SRGBColorSpace;
      mount.appendChild(renderer.domElement);

      const controls = new OrbitControls(camera, renderer.domElement);

      controls.enableDamping = true;
      controls.dampingFactor = 0.08;
      controls.minDistance = 4;
      controls.maxDistance = 200;
      controls.autoRotateSpeed = 1.6;

      scene.add(new THREE.AmbientLight(0xffffff, 1.5));

      const key = new THREE.DirectionalLight(0xffffff, 2.4);

      key.position.set(4, 6, 8);
      scene.add(key);

      const fill = new THREE.DirectionalLight(0xbfd4ff, 1.1);

      fill.position.set(-6, -3, -6);
      scene.add(fill);

      const group = new THREE.Group();

      scene.add(group);

      sceneRef.current = scene;
      cameraRef.current = camera;
      rendererRef.current = renderer;
      controlsRef.current = controls;
      groupRef.current = group;
      camera.position.set(8, 5.6, 16);

      const animate = () => {
        frameRef.current = requestAnimationFrame(animate);
        controls.update();
        renderer.render(scene, camera);
      };

      animate();
      fitCamera();

      const observer = new ResizeObserver(() => {
        if (!mount.clientWidth || !mount.clientHeight) return;
        camera.aspect = mount.clientWidth / mount.clientHeight;
        camera.updateProjectionMatrix();
        renderer.setSize(mount.clientWidth, mount.clientHeight);
      });

      observer.observe(mount);

      return () => {
        cancelAnimationFrame(frameRef.current);
        observer.disconnect();
        disposeGroup();
        controls.dispose();
        renderer.dispose();
        if (renderer.domElement.parentElement === mount) {
          mount.removeChild(renderer.domElement);
        }
      };
      // 仅初始化一次；后续通过 config 变化重建体素
    }, []);

    // 配置变化：重建体素方块
    useEffect(() => {
      const group = groupRef.current;
      const scene = sceneRef.current;

      if (!group || !scene) return;
      disposeGroup();

      const { text, font, bold, resolution } = config;
      const { cells, cols, rows } = voxelizeText(text, {
        font,
        bold,
        resolution,
      });

      if (cells.length === 0) return;

      const depth = Math.max(0.2, config.depth);
      const shrink = 1 - Math.min(0.5, Math.max(0, config.spacing));
      const geometry = new THREE.BoxGeometry(1, 1, 1);
      const material = new THREE.MeshLambertMaterial({ color: 0xffffff });

      if (config.texture === "noise") material.map = createNoiseTexture();
      else if (config.texture === "grid") material.map = createGridTexture();

      const total = cells.length;

      modelSizeRef.current = Math.max(cols, rows, depth * 2);

      const buildInstances = (
        target: THREE.InstancedMesh,
        inflate: number,
        zScale: number,
        colorize: boolean,
      ) => {
        const dummy = new THREE.Object3D();

        cells.forEach(([gx, gy], index) => {
          dummy.position.set(gx - cols / 2 + 0.5, rows / 2 - gy - 0.5, 0);
          dummy.scale.set(shrink * inflate, shrink * inflate, zScale);
          dummy.updateMatrix();
          target.setMatrixAt(index, dummy.matrix);
          if (colorize) {
            target.setColorAt(index, cellColor(config, gx, gy, cols, rows));
          }
        });
        target.instanceMatrix.needsUpdate = true;
        if (target.instanceColor) target.instanceColor.needsUpdate = true;
      };

      const mesh = new THREE.InstancedMesh(geometry, material, total);

      buildInstances(mesh, 1, depth, true);
      group.add(mesh);

      if (config.outline) {
        const outlineGeometry = new THREE.BoxGeometry(1, 1, 1);
        const outlineMaterial = new THREE.MeshBasicMaterial({
          color: new THREE.Color(config.outlineColor),
          side: THREE.BackSide,
        });
        const outlineMesh = new THREE.InstancedMesh(
          outlineGeometry,
          outlineMaterial,
          total,
        );

        buildInstances(outlineMesh, 1.18, depth * 1.12, false);
        group.add(outlineMesh);
      }

      scene.background = config.background
        ? new THREE.Color(config.background)
        : null;

      if (!builtRef.current) {
        builtRef.current = true;
        fitCamera();
      }
    }, [config]);

    useEffect(() => {
      if (controlsRef.current) controlsRef.current.autoRotate = autoRotate;
    }, [autoRotate]);

    useImperativeHandle(ref, () => ({
      screenshot: () => {
        const renderer = rendererRef.current;
        const scene = sceneRef.current;
        const camera = cameraRef.current;

        if (!renderer || !scene || !camera) return "";
        renderer.render(scene, camera);

        return renderer.domElement.toDataURL("image/png");
      },
      resetCamera: fitCamera,
    }));

    return <div ref={mountRef} className="h-full w-full" />;
  },
);

CubeText3DPreview.displayName = "CubeText3DPreview";

export default CubeText3DPreview;
