/*
 * 启动特效（Terminal 版）：点击启动后，主页的启动卡变成一块"绿色终端"，
 * 实时滚动本次启动的日志；进程真正拉起（Running）时文字碎裂成粒子，
 * 越过卡片边界在全窗口飞散。启动失败/退出时文字转红淡出。
 *
 * 实现要点：
 * - 终端只覆盖启动卡（经 data-neko-launch-panel 量取矩形，250ms 重测一次，
 *   跟随面板拖宽/窗口缩放）；卡片被藏起或找不到（如从实例页右键启动）时
 *   回退全窗口——宁可盖满也不画一块看不见的终端；
 * - 文字全部画在 canvas 上（不用 DOM）：碎裂时才能逐像素采样文字，
 *   把亮的像素块变成粒子飞走；
 * - 不直接滚原始日志：终端显示一份固定步骤清单（准备参数 / 校验完整性 /
 *   登录令牌 / Java 运行时 / 启动游戏），每步状态由后端 preparing 快照的
 *   文案关键词驱动（✓ 完成 / 转圈进行中 / 调暗未开始，被跳过的步骤标记
 *   为"跳过"而不是伪装成功）；失败时把失败原因显示为红色错误行。
 * - pointer-events-none 且常驻 Shell 根部：特效只是"看"，绝不挡交互；
 * - prefers-reduced-motion / Linux 低特效路径（data-low-fx）：不做碎裂，
 *   终端整体淡出（一次性动画，不在 low-fx 冻结范围）；
 * - 启动器中途打开时游戏已在跑：不补放特效（只在 Preparing 相位挂载）。
 */
import type { launch } from "../../../wailsjs/go/models";

import React, { useEffect, useRef, useState } from "react";

import { EventsOn } from "../../../wailsjs/runtime/runtime";

/**
 * GameLaunchPhase 取值（与 Go 侧常量一致；crashNotice.ts 只导出了 Exited）。
 * 终端只关心三段：Preparing（挂载）、Running（碎裂/收场）、
 * Failed=3 / Exited=4（转红淡出，走统一的 else 分支，不需要单独常量）。
 */
const LAUNCH_PHASE_PREPARING = 1;
const LAUNCH_PHASE_RUNNING = 2;

/** 字号与行高（CSS 像素）；左右留白。 */
const FONT_SIZE = 13;
const LINE_HEIGHT = 19;
const PADDING_X = 26;
const PADDING_TOP = 30;
/** 碎裂采样步长（CSS 像素）：步长越小粒子越多、越细腻。 */
const SAMPLE_STEP = 4;
/** 粒子数量软上限：超过后按概率丢弃采样点。 */
const MAX_PARTICLES = 3600;
/** 启动卡 DOM 标记（home.tsx 的 <aside>）；找不到时终端退回全窗口。 */
const CARD_SELECTOR = "[data-neko-launch-panel]";
/** 卡片圆角（与 rounded-large = 14px 一致）。 */
const CARD_RADIUS = 14;
/** 画布循环里重新测量卡片矩形的间隔（ms）：面板宽度可被拖动改变。 */
const CARD_MEASURE_INTERVAL = 250;

/** 终端要覆盖的矩形（CSS 像素）。 */
interface CardRect {
  x: number;
  y: number;
  w: number;
  h: number;
  /** 圆角；全窗口回退时为 0。 */
  radius: number;
}

/** 全窗口回退矩形。 */
const fullWindowRect = (): CardRect => ({
  x: 0,
  y: 0,
  w: window.innerWidth,
  h: window.innerHeight,
  radius: 0,
});

/**
 * 量取启动卡的矩形。卡片被藏起（实例库滑出面板时 translate-x-[115%]）、
 * 尺寸过小或不在视口内时回退全窗口——宁可盖满也不画一块看不见的终端。
 */
function measureCardRect(): CardRect {
  const card = document.querySelector(CARD_SELECTOR);

  if (!card) return fullWindowRect();
  const bounds = card.getBoundingClientRect();

  if (
    bounds.width < 80 ||
    bounds.height < 80 ||
    bounds.right < 0 ||
    bounds.bottom < 0 ||
    bounds.left >= window.innerWidth ||
    bounds.top >= window.innerHeight
  ) {
    return fullWindowRect();
  }

  return {
    x: bounds.left,
    y: bounds.top,
    w: bounds.width,
    h: bounds.height,
    radius: CARD_RADIUS,
  };
}

/** 手画圆角矩形路径（不用 ctx.roundRect：老 WebKitGTK 4.0 上不一定有）。 */
function roundRectPath(
  context: CanvasRenderingContext2D,
  rect: CardRect,
): void {
  const radius = Math.min(rect.radius, rect.w / 2, rect.h / 2);

  context.beginPath();
  context.moveTo(rect.x + radius, rect.y);
  context.arcTo(
    rect.x + rect.w,
    rect.y,
    rect.x + rect.w,
    rect.y + rect.h,
    radius,
  );
  context.arcTo(
    rect.x + rect.w,
    rect.y + rect.h,
    rect.x,
    rect.y + rect.h,
    radius,
  );
  context.arcTo(rect.x, rect.y + rect.h, rect.x, rect.y, radius);
  context.arcTo(rect.x, rect.y, rect.x + rect.w, rect.y, radius);
  context.closePath();
}

interface TerminalFXState {
  revision: number;
  versionId: string;
  phase: number;
  /** 后端 preparing 快照的当前文案（步骤清单据此点亮对应步骤）。 */
  message: string;
}

const LaunchFXOverlay: React.FC = () => {
  const [fx, setFx] = useState<TerminalFXState | null>(null);
  // 挂载时机只有 Preparing（点击启动必然先经过它）；挂上之后随快照同步相位
  // 与文案，直到画布自己宣布结束（碎裂/淡出完成）才卸载。
  const previousPhaseRef = useRef(0);

  useEffect(
    () =>
      EventsOn("launch:changed", (snapshot: launch.GameLaunchSnapshot) => {
        const phase = snapshot?.Phase ?? 0;

        previousPhaseRef.current = phase;
        setFx((current) => {
          if (!current) {
            if (phase !== LAUNCH_PHASE_PREPARING) return current;

            return {
              revision: snapshot?.Revision ?? 0,
              versionId: snapshot?.VersionId ?? "",
              phase,
              message: snapshot?.Message ?? "",
            };
          }

          return { ...current, phase, message: snapshot?.Message ?? "" };
        });
      }),
    [],
  );

  if (!fx) return null;

  return (
    <TerminalFX
      key={fx.revision}
      message={fx.message}
      phase={fx.phase}
      versionId={fx.versionId}
      onDone={() => setFx(null)}
    />
  );
};

type TerminalMode = "log" | "shatter" | "fade" | "fail";

interface ShatterParticle {
  x: number;
  y: number;
  vx: number;
  vy: number;
  size: number;
  r: number;
  g: number;
  b: number;
  alpha: number;
  decay: number;
}

/**
 * 启动步骤清单：顺序即后端启动管线的真实顺序（见 game_launch_service.go 的
 * publishPreparing 文案）。keys 命中快照 Message 即认为该步骤进行中。
 */
const LAUNCH_STEPS: Array<{ label: string; keys: string[] }> = [
  { label: "准备启动参数", keys: ["启动参数"] },
  { label: "校验游戏文件完整性", keys: ["校验游戏文件"] },
  { label: "处理登录令牌", keys: ["账号凭据", "校验账号"] },
  { label: "匹配 Java 运行时", keys: ["Java 运行时"] },
];
const FINAL_STEP_LABEL = "启动游戏";

/** 步骤状态：glyph 同时是终端里的显示符号。 */
type StepStatus = "done" | "active" | "skipped" | "pending";

const TerminalFX: React.FC<{
  versionId: string;
  phase: number;
  message: string;
  onDone: () => void;
}> = ({ versionId, phase, message, onDone }) => {
  const canvasRef = useRef<HTMLCanvasElement | null>(null);
  const [fading, setFading] = useState(false);
  // 画布循环共享的可变状态（走 ref，不进 React 渲染）
  const messageRef = useRef("");
  // drawText 定义在空依赖的画布 effect 里，props 闭包会停在首帧——
  // phase/message 都必须经 ref 传递，循环内才能读到最新值
  const phaseRef = useRef(phase);
  const modeRef = useRef<TerminalMode>("log");
  const modeStartRef = useRef(0);
  const particlesRef = useRef<ShatterParticle[]>([]);
  const reducedRef = useRef(false);
  const doneRef = useRef(onDone);
  // 出现过的步骤索引：清单里排在前面却从没出现的步骤（如关掉了启动前校验）
  // 标"跳过"，而不是伪装成已完成
  const seenStepsRef = useRef<Set<number>>(new Set());

  doneRef.current = onDone;
  messageRef.current = message;
  phaseRef.current = phase;

  // 整体淡出 + 延迟卸载。fade = 成功路径（保持绿色），fail = 失败/退出（转红）。
  const finish = (unmountAfter: number, mode: "fade" | "fail") => {
    if (modeRef.current !== "log") return;
    modeRef.current = mode;
    modeStartRef.current = performance.now();
    setFading(true);
    window.setTimeout(() => doneRef.current(), unmountAfter);
  };

  useEffect(() => {
    reducedRef.current =
      window.matchMedia("(prefers-reduced-motion: reduce)").matches ||
      document.documentElement.dataset.lowFx === "true";
  }, []);

  // 相位推进：Running → 碎裂（降级路径直接淡出）；Failed/Exited → 转红淡出
  useEffect(() => {
    if (phase < LAUNCH_PHASE_RUNNING) return;
    if (modeRef.current !== "log") return;

    if (phase === LAUNCH_PHASE_RUNNING && !reducedRef.current) {
      modeRef.current = "shatter";
      modeStartRef.current = performance.now();

      return;
    }
    // Running + 减弱动态：不做碎裂，直接淡出；Failed/Exited 同样淡出
    finish(
      phase === LAUNCH_PHASE_RUNNING ? 700 : 1000,
      phase === LAUNCH_PHASE_RUNNING ? "fade" : "fail",
    );
  }, [phase]);

  // ---- 画布主循环 ----
  useEffect(() => {
    const canvas = canvasRef.current;

    if (!canvas) return;
    const context = canvas.getContext("2d");

    if (!context) {
      doneRef.current();

      return;
    }

    let raf = 0;
    const start = performance.now();
    let shatterSampled = false;
    let lastMeasure = 0;
    // 终端覆盖区域：启动卡（找不到/被藏起时回退全窗口），循环内定期重测
    let cardRect = fullWindowRect();

    const resize = () => {
      const dpr = Math.min(window.devicePixelRatio || 1, 2);

      canvas.width = Math.round(window.innerWidth * dpr);
      canvas.height = Math.round(window.innerHeight * dpr);
      context.setTransform(dpr, 0, 0, dpr, 0, 0);
    };

    resize();
    window.addEventListener("resize", resize);

    const header = `neko-launcher --start ${versionId || "minecraft"}`;

    /** 由当前快照文案推导各步骤状态；顺带记录"出现过"的步骤。 */
    const stepStatuses = (): StepStatus[] => {
      const text = messageRef.current;
      const currentPhase = phaseRef.current;
      let activeIndex = -1;

      LAUNCH_STEPS.forEach((step, index) => {
        if (step.keys.some((key) => text.includes(key))) activeIndex = index;
      });
      if (activeIndex >= 0) seenStepsRef.current.add(activeIndex);

      // Running = 全部就绪（最后一行"启动游戏"打勾后随即碎裂）
      const allDone = currentPhase >= LAUNCH_PHASE_RUNNING;

      return LAUNCH_STEPS.map((_, index) => {
        if (allDone || index < activeIndex) {
          return seenStepsRef.current.has(index) ? "done" : "skipped";
        }
        if (index === activeIndex) return "active";

        return "pending";
      });
    };

    /** 画步骤清单（含命令行头、进行中的转圈与底部状态行）；裁剪由调用方负责。 */
    const drawText = (
      target: CanvasRenderingContext2D,
      rect: CardRect,
      now: number,
      color: string,
      alpha: number,
    ) => {
      const statuses = stepStatuses();
      const maxRows = Math.max(
        1,
        Math.floor((rect.h - PADDING_TOP - 20) / LINE_HEIGHT),
      );

      target.font = `${FONT_SIZE}px ui-monospace, "Cascadia Mono", Consolas, monospace`;
      target.textBaseline = "top";
      target.globalAlpha = alpha;

      // 命令行头（亮绿）
      target.fillStyle = "hsl(120 100% 72%)";
      target.fillText(`$ ${header}`, rect.x + PADDING_X, rect.y + PADDING_TOP);

      // 步骤清单：✓ 完成 / ASCII 转圈进行中 / – 跳过 / · 待执行
      const spinnerFrames = ["|", "/", "-", "\\"];
      const spinner =
        spinnerFrames[Math.floor((now - start) / 140) % spinnerFrames.length];
      // 失败后转圈停住：进行中的那一步换成 ✗（颜色随整体转红）
      const failing = modeRef.current === "fail";
      const activeGlyph = failing ? "✗" : spinner;
      const rows: Array<{ glyph: string; label: string; dim: boolean }> = [];

      LAUNCH_STEPS.forEach((step, index) => {
        switch (statuses[index]) {
          case "done":
            rows.push({ glyph: "✓", label: step.label, dim: false });
            break;
          case "active":
            rows.push({ glyph: activeGlyph, label: step.label, dim: false });
            break;
          case "skipped":
            rows.push({ glyph: "-", label: step.label, dim: true });
            break;
          default:
            rows.push({ glyph: "·", label: step.label, dim: true });
        }
      });
      // 收尾行：Running 后打勾（随即碎裂，多数用户只看到一瞬）
      if (phaseRef.current >= LAUNCH_PHASE_RUNNING) {
        rows.push({ glyph: "✓", label: FINAL_STEP_LABEL, dim: false });
      } else {
        rows.push({ glyph: activeGlyph, label: FINAL_STEP_LABEL, dim: false });
      }

      rows.slice(0, maxRows).forEach((row, index) => {
        const rowY = rect.y + PADDING_TOP + (index + 2) * LINE_HEIGHT;

        target.fillStyle = row.dim
          ? "hsl(120 30% 45%)"
          : row.glyph === "✓"
            ? "hsl(120 100% 72%)"
            : color;
        target.fillText(row.glyph, rect.x + PADDING_X, rowY);
        target.fillText(
          row.label,
          rect.x + PADDING_X + 22,
          rowY,
          Math.max(40, rect.w - PADDING_X - 30),
        );
      });

      // 底部状态行：后端当前文案（失败时就是失败原因），单行截断
      const detail = messageRef.current;

      if (detail) {
        target.fillStyle = failing ? "hsl(0 95% 70%)" : "hsl(120 25% 52%)";
        target.fillText(
          detail,
          rect.x + PADDING_X,
          rect.y + rect.h - 26,
          Math.max(40, rect.w - PADDING_X * 2),
        );
      }

      // 光标：0.6s 闪烁的方块，跟在最后一行行尾（碎裂后不再画）
      if (
        modeRef.current === "log" &&
        Math.floor((now - start) / 600) % 2 === 0
      ) {
        target.fillStyle = "hsl(120 100% 60%)";
        target.fillRect(
          rect.x + PADDING_X + 4,
          rect.y + rect.h - 26 + 16,
          9,
          2,
        );
      }
      target.globalAlpha = 1;
    };

    /** 碎裂采样：把卡片内文字像素（不含背景）取出来变成粒子。 */
    const sampleShatter = () => {
      const rect = cardRect;
      const width = window.innerWidth;
      const height = window.innerHeight;
      const offscreen = document.createElement("canvas");

      offscreen.width = width;
      offscreen.height = height;
      const offContext = offscreen.getContext("2d");

      if (!offContext) return;
      roundRectPath(offContext, rect);
      offContext.clip();
      drawText(offContext, rect, performance.now(), "hsl(120 100% 60%)", 1);

      const data = offContext.getImageData(0, 0, width, height).data;
      // 粒子从卡片中心向外炸（而不是窗口中心）：碎裂的"源头"是这块终端
      const cx = rect.x + rect.w / 2;
      const cy = rect.y + rect.h / 2;

      particlesRef.current = [];
      for (
        let y = Math.max(0, Math.floor(rect.y));
        y < Math.min(height, rect.y + rect.h);
        y += SAMPLE_STEP
      ) {
        for (
          let x = Math.max(0, Math.floor(rect.x));
          x < Math.min(width, rect.x + rect.w);
          x += SAMPLE_STEP
        ) {
          const index = (y * width + x) * 4;

          if (data[index + 3] < 60) continue;
          if (
            particlesRef.current.length >= MAX_PARTICLES &&
            Math.random() < 0.6
          ) {
            continue;
          }
          const dx = x - cx;
          const dy = y - cy;
          const distance = Math.max(1, Math.hypot(dx, dy));
          const speed = 1.5 + Math.random() * 4.5;

          particlesRef.current.push({
            x: x + SAMPLE_STEP / 2,
            y: y + SAMPLE_STEP / 2,
            vx: (dx / distance) * speed + (Math.random() - 0.5) * 1.6,
            vy: (dy / distance) * speed + (Math.random() - 0.5) * 1.6 - 1.2,
            size: 1.2 + Math.random() * 1.6,
            r: data[index],
            g: data[index + 1],
            b: data[index + 2],
            alpha: 0.95,
            decay: 0.008 + Math.random() * 0.012,
          });
        }
      }
    };

    const frame = (now: number) => {
      const width = window.innerWidth;
      const height = window.innerHeight;
      const mode = modeRef.current;

      if (now - lastMeasure > CARD_MEASURE_INTERVAL) {
        lastMeasure = now;
        cardRect = measureCardRect();
      }

      context.clearRect(0, 0, width, height);

      if (mode === "log") {
        // 背景：深绿黑 + 扫描线（只铺启动卡区域），文字带轻微辉光
        context.save();
        roundRectPath(context, cardRect);
        context.clip();
        context.fillStyle = "rgba(3, 12, 6, 0.8)";
        context.fillRect(cardRect.x, cardRect.y, cardRect.w, cardRect.h);
        context.fillStyle = "rgba(120, 255, 160, 0.03)";
        for (let y = cardRect.y; y < cardRect.y + cardRect.h; y += 3)
          context.fillRect(cardRect.x, y, cardRect.w, 1);
        context.shadowColor = "hsl(120 100% 50% / 0.55)";
        context.shadowBlur = 6;
        drawText(context, cardRect, now, "hsl(120 90% 62%)", 1);
        context.restore();
      } else if (mode === "shatter") {
        const sinceMode = now - modeStartRef.current;

        if (!shatterSampled) {
          shatterSampled = true;
          sampleShatter();
        }

        // 残留背景快速淡出（仍限卡片区域）
        const backdropAlpha = Math.max(0, 1 - sinceMode / 400);

        if (backdropAlpha > 0.01) {
          context.save();
          roundRectPath(context, cardRect);
          context.clip();
          context.fillStyle = `rgba(3, 12, 6, ${0.8 * backdropAlpha})`;
          context.fillRect(cardRect.x, cardRect.y, cardRect.w, cardRect.h);
          context.restore();
        }

        // 粒子：从卡片中心向外飞散，越过卡片边界、全窗口自由下坠
        context.globalCompositeOperation = "lighter";
        for (const particle of particlesRef.current) {
          if (particle.alpha <= 0) continue;
          particle.vy += 0.1;
          particle.vx *= 0.985;
          particle.vy *= 0.985;
          particle.x += particle.vx;
          particle.y += particle.vy;
          particle.alpha -= particle.decay;
          context.fillStyle = `rgb(${particle.r} ${particle.g} ${particle.b} / ${particle.alpha})`;
          context.fillRect(
            particle.x,
            particle.y,
            particle.size,
            particle.size,
          );
        }
        context.globalCompositeOperation = "source-over";

        if (sinceMode > 2300) {
          doneRef.current();

          return;
        }
      } else {
        // fade / fail：整体淡出；fail 转红，fade 保持绿色（成功收场），
        // 外层同时有 CSS opacity 过渡兜底
        const sinceMode = now - modeStartRef.current;
        const alpha = Math.max(0, 1 - sinceMode / 900);

        if (alpha <= 0) {
          doneRef.current();

          return;
        }
        context.save();
        roundRectPath(context, cardRect);
        context.clip();
        if (mode === "fail") {
          context.fillStyle = `rgba(20, 4, 4, ${0.8 * alpha})`;
          context.fillRect(cardRect.x, cardRect.y, cardRect.w, cardRect.h);
          drawText(context, cardRect, now, "hsl(0 95% 65%)", alpha);
        } else {
          context.fillStyle = `rgba(3, 12, 6, ${0.8 * alpha})`;
          context.fillRect(cardRect.x, cardRect.y, cardRect.w, cardRect.h);
          drawText(context, cardRect, now, "hsl(120 90% 62%)", alpha);
        }
        context.restore();
      }

      raf = requestAnimationFrame(frame);
    };

    raf = requestAnimationFrame(frame);

    return () => {
      cancelAnimationFrame(raf);
      window.removeEventListener("resize", resize);
    };
    // 一次性循环；日志/相位经 ref 与 props 传入
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  return (
    <div
      aria-hidden
      className={`pointer-events-none fixed inset-0 z-[60] transition-opacity duration-500 ${
        fading ? "opacity-0" : "opacity-100"
      }`}
    >
      <canvas ref={canvasRef} className="absolute inset-0 h-full w-full" />
    </div>
  );
};

export default LaunchFXOverlay;
