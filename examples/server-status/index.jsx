/*
 * 官方示例插件：服务器状态小组件。
 *
 * 本示例演示的 API 能力（对应 docs/Extensions_Guide.md）：
 *   - dev 模式：本文件是 index.jsx 源码，宿主现场编译后加载，无工具链；
 *   - settings 种子：plugin.yaml 的默认设置在激活前已种入 api.config；
 *   - 权限：server-status / clipboard / storage / network 在 plugin.yaml 声明，
 *     调用未声明的权限会直接抛错；
 *   - 用户授权：插件页每个权限一个开关，关掉的权限**返回 null**（本示例对
 *     getServerStatus 的 null 做了提示）——联网默认关闭，用户打开后才查得到；
 *   - onCleanup：轮询定时器在插件卸载/重载时自动清理；
 *   - WidgetRenderContext：isBusy 感知启动状态、onJoin 快速进服。
 *
 * 试用：把整个 server-status 文件夹拷进启动器插件目录，点「重新加载」，
 * 然后到主页组件库添加「服务器状态」。
 *
 * 小技巧：本示例全部使用中文字面量；想适配宿主语言切换的插件可以用
 * api.t() 做占位插值，并把文案登记进宿主词条。
 */

export default function activate(api) {
  // hooks 从注入的 React 命名空间取用；写在组件里则手写 JS / dev JSX 通吃
  function StatusCard({ context }) {
    const { useState, useEffect } = api.react;
    const { isBusy, onJoin } = context;

    const [cfg, setCfg] = useState(null);
    const [state, setState] = useState({
      loading: true,
      online: false,
      status: null,
      error: "",
    });
    const [joining, setJoining] = useState(false);

    useEffect(() => {
      let alive = true;

      void (async () => {
        // settings 已在激活前种入 config，读到的就是 plugin.yaml 的默认值
        const host = (await api.config.get("serverHost")) || "hypixel.net";
        const port = Number((await api.config.get("serverPort")) || 25565);
        const refreshSeconds = Math.max(
          15,
          Number((await api.config.get("refreshSeconds")) || 60),
        );
        if (!alive) return;
        setCfg({ host, port, refreshSeconds });

        const refresh = async () => {
          try {
            const status = await api.getServerStatus(host, port);
            if (!alive) return;
            // 返回 null = 被用户关掉了开关（联网或服务器状态权限），不是错误：
            // 提示用户去插件页打开，而不是显示"离线"
            if (!status) {
              setState({
                loading: false,
                online: false,
                status: null,
                error: "权限已关闭：请在「插件」页打开本插件的对应开关",
              });
              return;
            }
            setState({ loading: false, online: true, status, error: "" });
          } catch (ex) {
            if (alive) {
              setState({
                loading: false,
                online: false,
                status: null,
                error: String(ex?.message ?? ex),
              });
            }
          }
        };

        refresh();
        const timer = setInterval(refresh, refreshSeconds * 1000);
        // 插件卸载/重载时宿主会调用；组件被移除则由下面的 return 兜底
        api.onCleanup(() => {
          alive = false;
          clearInterval(timer);
        });
      })();

      return () => {
        alive = false;
      };
    }, []);

    const copyAddress = async () => {
      await api.setClipboard(`${cfg.host}:${cfg.port}`);
      api.notify.success("服务器地址已复制");
    };

    const join = async () => {
      setJoining(true);
      try {
        const error = await onJoin(cfg.host, cfg.port);
        if (error) api.notify.error(error);
      } finally {
        setJoining(false);
      }
    };

    const address = cfg ? `${cfg.host}:${cfg.port}` : "…";

    // 宿主已把内容装入标准卡片壳（主题描边 + 事件恢复），这里不要自带卡片背景；
    // 头部用 api.HomeCard 获得与内置组件一致的「图标 + 标题 + 大数值」行。
    return (
      <div className="flex flex-col gap-2">
        <api.HomeCard
          icon={<icons.Globe20Regular />}
          label="服务器状态"
          value={
            state.loading ? "…" : state.online ? state.status.OnlinePlayers : "离线"
          }
        />
        <div className="flex items-center justify-between gap-2">
          <span className="min-w-0 truncate text-[13px] font-semibold text-gray-700 dark:text-gray-200">
            {address}
          </span>
          {state.loading ? <ui.Spinner size="sm" /> : null}
        </div>

        {!state.loading && state.online ? (
          <div className="truncate text-[11px] text-gray-400">
            {state.status.VersionName} · {state.status.Motd}
          </div>
        ) : null}
        {!state.loading && !state.online ? (
          <div className="truncate text-[11px] text-gray-400">{state.error}</div>
        ) : null}

        <div className="flex items-center gap-1.5">
          <ui.Button
            isDisabled={!cfg || isBusy || joining}
            isLoading={joining}
            size="sm"
            startContent={<icons.Play20Regular />}
            variant="flat"
            onPress={() => void join()}
          >
            进服
          </ui.Button>
          <ui.Button
            isDisabled={!cfg || joining}
            size="sm"
            startContent={<icons.DocumentText20Regular />}
            variant="flat"
            onPress={() => void copyAddress()}
          >
            复制地址
          </ui.Button>
        </div>
      </div>
    );
  }

  // render 目前是"渲染函数"契约：第一个参数就是 WidgetRenderContext。
  // 组件内要用 hooks，所以这里包一层内层组件（hooks 归属它自己的组件实例）。
  api.registerWidget({
    id: "server-status",
    title: "服务器状态",
    description: "查询服务器在线人数，一键复制地址或快速进服",
    icon: api.h(api.icons.Globe20Regular),
    tileClass: "from-sky-500 to-indigo-500",
    render: (context) => <StatusCard context={context} />,
  });
}
