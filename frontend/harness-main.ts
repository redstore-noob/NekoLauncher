/* harness 页入口：读 payload.json → SceneRenderer 上屏，控制台/日志区排障 */
import type { WEScenePayload } from "./src/lib/we-scene/types";

import { SceneRenderer } from "./src/lib/we-scene/renderer";

const logEl = document.getElementById("log") as HTMLDivElement;
const lines: string[] = [];

function log(message: string): void {
  lines.push(message);
  logEl.textContent = lines.join("\n");
  // eslint-disable-next-line no-console -- 排障 harness，日志就是要打到控制台
  console.log(message);
}

window.addEventListener("error", (e) => log(`[error] ${e.message}`));

fetch("/payload.json")
  .then((r) => r.json() as Promise<WEScenePayload>)
  .then((payload) => {
    log(
      `payload: ${payload.DesignWidth}x${payload.DesignHeight}, objects=${payload.Objects?.length ?? 0}`,
    );
    const canvas = document.getElementById("canvas") as HTMLCanvasElement;
    const renderer = new SceneRenderer({
      canvas,
      payload,
      onFirstFrame: () => log("[first-frame] 场景已上屏"),
    });

    renderer.start().catch((error) => log(`[start-failed] ${error}`));
    // 10 秒后自动截图采样，排障用
    setTimeout(() => {
      log("[harness] 10s 到，采样像素检查画面是否非空");
      const gl = canvas.getContext("webgl2") ?? canvas.getContext("webgl");

      if (!gl) {
        log("[harness] 无法取 WebGL 上下文");

        return;
      }
      const pixels = new Uint8Array(4 * 64 * 64);

      gl.readPixels(
        Math.max(0, (gl.drawingBufferWidth - 64) >> 1),
        Math.max(0, (gl.drawingBufferHeight - 64) >> 1),
        64,
        64,
        gl.RGBA,
        gl.UNSIGNED_BYTE,
        pixels,
      );
      let nonZero = 0;

      for (let i = 3; i < pixels.length; i += 4) if (pixels[i] !== 0) nonZero++;
      log(`[harness] 中心 64x64 非空像素: ${nonZero}/4096`);
    }, 10000);
  })
  .catch((error) => log(`[payload-failed] ${error}`));
