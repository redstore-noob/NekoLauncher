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

/**
 * WE 场景壁纸的渲染载荷(见 Go 端 WallpaperEngineScene)。
 *
 * 场景画面完全由 WebWallGL 渲染,宿主只需要把资源基址、设计分辨率与
 * 用户属性覆盖表交给它——scene.pkg 的解析、贴图解码、puppet 骨骼、
 * 脚本沙箱都在库内部完成,这里不再有任何场景结构描述。
 */
export interface WEScenePayload {
  /**
   * WebWallGL 的 httpSource 基址(带会话指纹的 /wescene 前缀):
   * 库会在它下面请求 scene.pkg、project.json 与松散工程文件。
   * 短前缀的旧后端没有这个字段,前端会回落到 "/wescene"。
   */
  Base?: string;
  /** 场景设计分辨率(scene.json general.orthogonalprojection) */
  DesignWidth: number;
  DesignHeight: number;
  /**
   * 属性名(schema 原始大小写) → 当前值,直接交给 WebWallGL 的
   * MountOptions.properties:scene.json 里的 property 引用与 WE 的
   * applyUserProperties 都按原名匹配,不能小写归一。
   */
  Properties?: Record<string, boolean | number | string> | null;
}
