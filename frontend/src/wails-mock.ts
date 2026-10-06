/*
 * 纯浏览器 dev 兜底：不带 Wails runtime 直接跑 Vite（npm run dev 后用浏览器
 * 打开，而不是 wails dev）时，wailsjs 生成代码会调 window.go / window.runtime，
 * 缺失时整个 React 树在 mount 阶段就崩掉（黑屏）。这里用递归 Proxy 兜底：
 * 任意绑定调用都返回 Promise<空串>，各调用点的 parse/catch 会回落默认值，
 * 界面照常渲染——足够做纯视觉调试（样式 / 毛玻璃等）。wails dev 下
 * window.go 由真实 runtime 注入，此 mock 完全不生效。
 *
 * CALL_STUBS：按「go.bindings.<域>.<方法>」路径精确覆盖返回值的白名单，
 * 让依赖后端数据的页面（实例列表 / 游戏设置等）在纯浏览器 dev 下也有可
 * 调试的假数据；未命中的调用仍走通用 null 兜底。
 */

type StubFn = (...args: unknown[]) => unknown;

const CALL_STUBS: Record<string, StubFn> = {
  // 联机页：两家供应商 + 红石默认设置，供右列设置面板调试
  "go.bindings.OnlineAPI.ListProviders": () =>
    Promise.resolve([
      {
        ID: "terracotta",
        Name: "陶瓦联机",
        Summary: "基于 Terracotta 的虚拟局域网联机，房主需要安装 Terracotta。",
        Homepage: "https://github.com/burningtnt/Terracotta",
        Ready: false,
        NeedsMod: true,
        HostNote: "房主需要安装 Terracotta 并安装联机模组。",
        JoinNote: "房主会给你一个 U/ 开头的房间码。",
        Hint: "",
      },
      {
        ID: "redstone",
        Name: "红石联机",
        Summary: "基于公网中继的联机，房客无需安装任何东西。",
        Homepage: "https://github.com/burningtnt/Terracotta",
        Ready: true,
        NeedsMod: false,
        HostNote: "把启动器托管的服务器或本机地址转发到公网。",
        JoinNote: "直接填房主给你的公网地址即可。",
        Hint: "",
      },
    ]),
  "go.bindings.OnlineAPI.GetSettings": () =>
    Promise.resolve({
      Provider: "redstone",
      Player: "",
      TerracottaPath: "",
      RedstoneRelay: "122.51.108.96",
      RedstoneKey: "rs-demo-key",
      Target: "127.0.0.1:25565",
      ServerID: "",
      MaxPlayers: 8,
    }),
  "go.bindings.OnlineAPI.GetRuntime": () =>
    Promise.resolve({ Running: false, Binary: "", Port: 0, Version: "" }),
  "go.bindings.OnlineAPI.ListLocalServers": () => Promise.resolve([]),
  "go.bindings.OnlineAPI.ListStatuses": () => Promise.resolve([]),
  "go.bindings.OnlineAPI.ListRelayOptions": () =>
    Promise.resolve([
      { Name: "官方节点", Address: "122.51.108.96", Builtin: true },
    ]),
  "go.bindings.OnlineAPI.GetRelayList": () => Promise.resolve(""),
  "go.bindings.OnlineAPI.SaveRelayList": () => Promise.resolve(),
  "go.bindings.OnlineAPI.ProbeRelays": () => Promise.resolve([]),
  "go.bindings.OnlineAPI.GenerateAPIKey": () =>
    Promise.resolve("MOCKKEY1234567890AB"),
  // 实例管理：一个假的 1.20.1 Fabric 实例 + options.txt，供详情/游戏设置调试
  "go.bindings.InstanceAPI.GetCurrentInstanceSnapshot": () =>
    Promise.resolve({
      SourcePath: "E:/mc",
      MinecraftDirectory: "E:/mc",
      GameDirectory: "E:/mc",
      VersionIds: ["1.20.1", "1.20.4"],
      SelectedVersionId: "1.20.1",
      UsesVersionDirectoryAsGameDirectory: false,
      IsLoading: false,
      ErrorMessage: "",
    }),
  "go.bindings.InstanceAPI.GetVersionDetails": () =>
    Promise.resolve({
      VersionId: "1.20.1",
      VersionDirectory: "E:/mc/versions/1.20.1",
      ContentDirectory: "E:/mc",
      LayoutProvider: "standard",
      LayoutEvidence: "",
      IsIsolated: false,
      IsExternallyManaged: false,
      VersionType: "release",
      BaseGameVersion: "1.20.1",
      LoaderName: "Fabric",
      LoaderVersion: "0.15.0",
      InstanceIconPath: "",
      InstanceIconGlyph: "",
      ReleaseTime: "2023-06-12",
      MainClass: "net.minecraft.client.main.Main",
      JavaRequirement: "Java 17",
      Mods: [],
      ResourcePacks: [],
      Shaders: [],
      Saves: [],
      HasShaderDirectory: true,
    }),
  // 主页启动卡：目录 / 版本列表 / 选中版本 / 账号与实例快照保持同一套种子数据
  "go.bindings.ConfigAPI.GetGameDirectory": () => Promise.resolve("E:/mc"),
  "go.bindings.InstanceAPI.EnsureDefaultMinecraftDirectory": () =>
    Promise.resolve("E:/mc"),
  "go.bindings.InstanceAPI.GetInstalledVersionIds": () =>
    Promise.resolve(["1.20.1", "1.20.4"]),
  "go.bindings.InstanceAPI.SelectInstance": () => Promise.resolve(true),
  "go.bindings.ConfigAPI.GetValue": (key: unknown) =>
    Promise.resolve(key === "selectedGameInstance" ? "1.20.1" : ""),
  "go.bindings.ConfigAPI.SetValue": () => Promise.resolve(true),
  "go.bindings.AccountAPI.GetAccounts": () =>
    Promise.resolve([
      { DisplayName: "烟花", Type: "microsoft" },
      { DisplayName: "noob_player", Type: "offline" },
    ]),
  "go.bindings.AccountAPI.GetSelectedAccount": () =>
    Promise.resolve({ DisplayName: "烟花", Type: "microsoft" }),
  "go.bindings.AccountAPI.GetAccountStableKey": (account: unknown) =>
    Promise.resolve(
      `mock:${(account as { DisplayName?: string })?.DisplayName ?? "account"}`,
    ),
  "go.bindings.AccountAPI.GetAvatarUrl": () => Promise.resolve(""),
  "go.bindings.SystemAPI.ReadTextFile": (path: unknown) =>
    Promise.resolve(
      String(path).endsWith("options.txt")
        ? [
            "version:4157",
            "mouseSensitivity:1.0",
            "fov:90.0",
            "renderDistance:12",
            "simulationDistance:10",
            "entityDistance:2.0",
            "guiScale:2",
            "graphics:fancy",
            "particles:all",
            "maxFps:120",
            "brightness:0.5",
            "mipmapLevels:4",
            "biomeBlendRadius:2",
            "cloudType:fancy",
            "fullscreen:false",
            "vsync:true",
            "bobView:false",
            "entityShadows:true",
            "screenEffectScale:1.0",
          ].join("\n")
        : "",
    ),
};

function makeStub(
  path = "",
  overrides: Record<string, (...args: unknown[]) => unknown> = {},
): unknown {
  return new Proxy(function stub() {}, {
    get(_target, prop) {
      if (prop === "then" || prop === "catch" || prop === "finally") {
        return undefined; // 避免被当成 thenable 无限展开
      }

      return makeStub(
        path ? `${path}.${String(prop)}` : String(prop),
        overrides,
      );
    },
    apply(_target, _thisArg, argArray) {
      // null 而非 ""：调用点普遍有 ?? / || 兜底（如 (await ListPlugins()) ?? []），
      // 返回空串会绕过 ?? 兜底导致 .find 等数组操作崩溃
      const stub = overrides[path] ?? CALL_STUBS[path];

      if (stub) return stub(...argArray);

      return Promise.resolve(null);
    },
  });
}

// window.go / window.runtime 的正式类型声明在 wails.d.ts（Wails 生成），
// 这里不做重复声明，直接用类型断言访问，避免与生成类型冲突
if (typeof window !== "undefined") {
  const w = window as unknown as Record<string, unknown>;

  if (!w.go) w.go = makeStub("go");
  if (!w.runtime) {
    w.runtime = makeStub("runtime", {
      // Wails 真实 runtime 的 EventsOnMultiple 返回取消监听函数，组件卸载时会
      // 调用它（EventsOn 内部也转调这里）；代理 stub 返回的是 Promise/undefined，
      // 清理回调一跑就抛 "cancel is not a function" 把整个 React 树炸掉。
      "runtime.EventsOnMultiple": (_event, _callback, _max) => () => {
        /* mock 无事件源，返回空取消函数 */
      },
    });
  }
}

export {};
