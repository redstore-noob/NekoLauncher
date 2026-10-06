/*
 * 后端闸门：让插件碰不到 window.go 上的宿主绑定。
 *
 * 背景与威胁模型（见 docs/Extensions_Guide.md §1 的历史版本）：Wails 把全部
 * Go 绑定挂在 window.go 上，插件又与宿主跑在同一个 JS realm，过去插件技术上
 * 可以直呼任意绑定（读写任意文件、删实例、卸载别的插件）。本模块在启动最早
 * 时刻（main.tsx，先于任何插件加载）做三件事：
 *
 *   ① 捕获真实的 window.go，换成不可配置、只抛错的 getter——插件从全局拿不到了；
 *   ② 深包装捕获到的绑定树给宿主专用（hostGo()），包装函数在同步调用帧内
 *      打开"宿主帧"标记；
 *   ③ 封堵所有能直呼 Go 方法的原始出口：window.WailsInvoke / window.ObfuscatedCall /
 *      window.chrome.webview.postMessage（WebView2）/ window.webkit（WKWebView）。
 *      Wails 的任意方法调用一律走 'C'/'c' 前缀消息（v2 runtime calls.js），
 *      闸门只放行宿主帧内的 C/c 消息；窗口拖动（"drag"/"resize:"）、事件
 *      （"E*"）、日志（"L"）等运行时自身消息不受影响。
 *
 * 宿主侧的 wailsjs 生成代码在构建/开发期被 vite 插件重写为调 hostGo()，
 * 生成文件本身不动（wails generate 会覆盖）。纯浏览器 dev 的 wails-mock
 * 同样先于本模块执行、被同样捕获，行为不变。
 *
 * 已知残留（单 realm 隔离的边界，接受并在文档标注）：wails dev 下 Vite 开发
 * 服务器向所有同源代码开放 /wailsjs/ 模块，dev 插件仍可 import 它们拿到
 * hostGo() 视图；生产构建里这些模块已打进宿主 bundle、没有可 import 的 URL。
 * window.runtime（关窗/退出/发事件等固定动作）与 window.wails（flags）保留
 * 在全局——运行时内部代码按全局名反复查找它们，且它们只能触发固定消息类型，
 * 无法任意调用 Go 方法。
 */

/** 宿主帧深度：>0 表示当前同步调用栈源自宿主包装函数 */
let hostDepth = 0;

let hostGoView: unknown;

/**
 * 深包装绑定树：对对象/函数统一返回懒代理——
 *   - get 陷阱按需递归包装属性（WeakMap 缓存保证同引用同代理），
 *     这样既支持真 wails 的普通对象树，也支持 wails-mock 那种"可调用
 *     Proxy 桩"（属性经 get 陷阱动态生成，快照式复制抓不到任何东西）；
 *   - apply 陷阱在同步调用帧内打开"宿主帧"标记，真实绑定在同步段内
 *     发出 WailsInvoke('C'…)。
 */
function deepWrap(node: unknown, cache: WeakMap<object, unknown>): unknown {
  if (
    node === null ||
    (typeof node !== "object" && typeof node !== "function")
  ) {
    return node;
  }

  const cached = cache.get(node);

  if (cached !== undefined) return cached;

  const proxy = new Proxy(node, {
    get(target: object, key: string | symbol) {
      // then/toPrimitive 等协议探针必须原样应答，否则代理会被当成 thenable
      if (key === "then" || key === Symbol.toPrimitive) {
        return (target as Record<string | symbol, unknown>)[key];
      }

      return deepWrap((target as Record<string | symbol, unknown>)[key], cache);
    },
    apply(target: object, thisArg: unknown, args: unknown[]): unknown {
      hostDepth += 1;
      try {
        return (target as (...a: unknown[]) => unknown).apply(thisArg, args);
      } finally {
        hostDepth -= 1;
      }
    },
  });

  cache.set(node, proxy);

  return proxy;
}

/** Wails 任意方法调用消息的前缀（calls.js：Call → 'C'，ObfuscatedCall → 'c'） */
function isMethodCallMessage(message: unknown): boolean {
  return (
    typeof message === "string" &&
    (message.startsWith("C") || message.startsWith("c"))
  );
}

/** 允许通过的判定：非方法调用消息一律放行；方法调用必须在宿主帧内 */
function gateCheck(sink: string, message: unknown): void {
  if (hostDepth === 0 && isMethodCallMessage(message)) {
    throw new Error(
      `[plugins] 已拦截插件对宿主后端的直接调用（${sink}）；插件只能使用注入的 api 对象`,
    );
  }
}

/** 把一个 postMessage 风格的原始出口换成带闸版本；失败（属性不可重定义）则跳过 */
function gateSink(
  holder: Record<string, unknown>,
  key: string,
  describe: (message: unknown) => string,
): void {
  const real = holder[key];

  if (typeof real !== "function") return;

  const descriptor = Object.getOwnPropertyDescriptor(holder, key);

  if (!descriptor?.configurable || !("value" in descriptor)) return;

  try {
    Object.defineProperty(holder, key, {
      ...descriptor,
      value(this: unknown, ...args: unknown[]) {
        gateCheck(describe(args[0]), args[0]);

        return (real as (...a: unknown[]) => unknown).apply(this, args);
      },
    });
  } catch {
    /* 平台差异导致改不动就保留原样，至少 WailsInvoke 一层仍在 */
  }
}

/** 封堵 window.chrome.webview.postMessage（Windows/WebView2）与 window.webkit（mac/Linux） */
function gatePlatformSinks(win: Window & typeof globalThis): void {
  const w = win as unknown as Record<string, unknown>;

  for (const globalName of ["chrome", "webkit"]) {
    const platform = w[globalName];

    if (typeof platform !== "object" || platform === null) continue;

    const webview = (platform as Record<string, unknown>).webview;

    if (webview && typeof webview === "object") {
      gateSink(
        webview as Record<string, unknown>,
        "postMessage",
        () => `${globalName}.webview.postMessage`,
      );
    }
    const handlers = (platform as Record<string, unknown>).messageHandlers as
      | Record<string, unknown>
      | undefined;
    const external = handlers?.external;

    if (external && typeof external === "object") {
      gateSink(
        external as Record<string, unknown>,
        "postMessage",
        () => `${globalName}.messageHandlers.external.postMessage`,
      );
    }
  }
}

let installed = false;

/** 捕获并隐藏 window.go；installBackendGate 与 hostGo 的懒补捕获共用 */
function captureAndHideGo(): void {
  if (hostGoView !== undefined) return;

  const w = window as unknown as Record<string, unknown>;
  const realGo = w.go;

  if (realGo === undefined) return;

  hostGoView = deepWrap(realGo, new WeakMap());

  try {
    //刻意 configurable：就算插件把它覆盖成自己的对象也无害——真绑定树
    // 已在闭包里，覆盖换不回任何后端访问能力；不可配置反而让测试环境
    // 无法还原全局状态
    Object.defineProperty(window, "go", {
      configurable: true,
      enumerable: true,
      get() {
        throw new Error(
          "[plugins] window.go 已被后端闸门收起：宿主代码请走 wailsjs 生成绑定（已被重写到 hostGo()），插件请使用注入的 api 对象",
        );
      },
    });
  } catch {
    /* 改不动（理论上不会）则至少深包装与出口闸仍生效 */
  }
}

/**
 * installBackendGate 捕获并隐藏 window.go、封堵原始调用出口。
 * 必须先于插件加载执行（main.tsx 顶部调用一次）；幂等。
 */
export function installBackendGate(): void {
  if (installed) return;
  installed = true;

  const w = window as unknown as Record<string, unknown>;

  // 纯浏览器 dev（wails-mock）与真实 wails 下 window.go 都已就位；万一
  // 注入晚于前端启动（异常时序），hostGo() 首次调用时会懒补捕获
  captureAndHideGo();

  gateSink(
    w,
    "WailsInvoke",
    (message: unknown) => `WailsInvoke(${String(message).slice(0, 40)}…)`,
  );

  // ObfuscatedCall 是混淆构建专用的任意方法调用入口；本项目未启用混淆，
  // 宿主也不经它调用——直接换成只抛错的函数，堵死这条支路
  const obfuscatedDescriptor = Object.getOwnPropertyDescriptor(
    w,
    "ObfuscatedCall",
  );

  if (obfuscatedDescriptor?.configurable) {
    try {
      Object.defineProperty(w, "ObfuscatedCall", {
        ...obfuscatedDescriptor,
        value() {
          throw new Error("[plugins] ObfuscatedCall 已被后端闸门禁用");
        },
      });
    } catch {
      /* 同上，改不动就算了 */
    }
  }

  gatePlatformSinks(window);
}

/**
 * hostGo 宿主专用的绑定树视图（vite 重写后的 wailsjs 生成代码调用）。
 * 安装时 window.go 尚未注入（异常时序）的话，这里懒补捕获；之后仍取不到
 * 才回落原始 window.go（如单测直接 import 生成绑定的场景）。
 */
export function hostGo(): unknown {
  if (hostGoView === undefined) captureAndHideGo();

  return hostGoView ?? (window as unknown as Record<string, unknown>).go;
}

/** @internal 仅供单测在用例间还原闸门状态 */
export function __resetBackendGateForTest(): void {
  installed = false;
  hostGoView = undefined;
  hostDepth = 0;
}
