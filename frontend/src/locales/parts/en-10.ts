/*
 * 启动参数溯源（launch argument provenance）分片：面板标题、来源大类与
 * 稳定 Key 的说明、冲突话术、显示范围开关。
 *
 * Key → 文案的对应表在 src/lib/launchProvenance.ts，这里只放译文。
 * 已存在于其它分片的通用词（主类 / 刷新 / 展开 …）不在此重复。
 */
const dict: Record<string, string> = {
  // ---- 面板外框 ----
  启动参数溯源: "Launch argument provenance",
  本次启动: "This launch",
  "本次启动 · 共 {0} 条参数": "This launch · {0} arguments in total",
  本次启动环境: "This launch used",
  "实例 / 版本": "Instance / version",
  "Java 可执行文件": "Java executable",
  工作目录: "Working directory",
  "正在读取启动参数…": "Reading launch arguments…",
  "还没有成功启动过游戏，暂时没有可以回溯的参数":
    "The game has not launched successfully yet, so there is nothing to trace back to",
  "启动参数溯源会在游戏成功启动后记录。先启动一次游戏，再回到这里查看每个参数是谁加的。":
    "Launch argument provenance is recorded after the game starts successfully. Launch the game once, then come back here to see who contributed each argument.",
  这次启动没有记录到任何参数: "No arguments were recorded for this launch",

  // ---- 摘要行 ----
  "有 {0} 组参数被后面的同名参数覆盖":
    "{0} argument group(s) were overridden by a later argument with the same name",
  "共 {0} 条参数没有生效（列表中已划线标出）。JVM 取最后一次出现的值，被覆盖的那条不会起作用。":
    "{0} argument(s) never took effect (they are struck through in the list). The JVM uses the last occurrence, so the overridden one does nothing.",
  "没有参数互相覆盖，{0} 条参数全部生效。":
    "No arguments override each other; all {0} arguments take effect.",

  // ---- 显示范围 / 工具行 ----
  显示全部参数: "Show all arguments",
  "显示全部参数（含被覆盖的）":
    "Showing all arguments (including overridden ones)",
  只显示生效的参数: "Showing effective arguments only",
  "点击{0}完整参数": "Click to {0} the full argument",
  点击展开完整参数: "Click to expand the full argument",
  点击收起: "Click to collapse",
  "「{0}」出现 {1} 次：来自「{2}」的那条生效，来自「{3}」的被忽略。":
    "“{0}” appears {1} time(s): the one from “{2}” wins, the one from “{3}” is ignored.",
  "{0} / {1} 条": "{0} / {1}",
  "{0} 条": "{0}",

  // ---- 条目徽章 ----
  已被覆盖: "Overridden",
  "压过了「{0}」": "overrides “{0}”",

  // ---- 冲突区 ----
  互相覆盖的参数: "Arguments that override each other",

  // ---- 分组标题 ----
  "JVM 参数": "JVM arguments",
  游戏参数: "Game arguments",
  其他参数: "Other arguments",
  "交给 Java 虚拟机，在游戏启动前生效":
    "Passed to the Java virtual machine, applied before the game starts",
  决定实际启动哪个入口类: "Decides which entry class is actually started",
  "原样传给 Minecraft 本体": "Passed through to Minecraft itself",

  // ---- 来源大类（Source.Kind） ----
  版本文件自带: "Bundled with the version file",
  启动器自动添加: "Added automatically by the launcher",
  全局启动设置: "Global launch settings",
  实例独立设置: "Instance-specific settings",
  实例或全局设置: "Instance or global settings",
  直接进服: "Direct connect",
  外置登录注入: "External login injector",
  插件添加: "Added by a plugin",
  启动变换: "Launch transform",
  来源未知: "Unknown source",

  // ---- 来源说明（Source.Key 模板，{0} 为 Detail） ----
  "启动器自动设置的最小堆（{0} MiB）":
    "Minimum heap set automatically by the launcher ({0} MiB)",
  "实例独立设置的最大堆（{0} MiB）":
    "Maximum heap from the instance-specific setting ({0} MiB)",
  "全局设置的最大堆（{0} MiB）":
    "Maximum heap from the global setting ({0} MiB)",
  "启动器按系统内存自动计算的最大堆（{0} MiB）":
    "Maximum heap computed automatically from system memory ({0} MiB)",
  "启动器内置的 G1 垃圾回收调优参数":
    "G1 garbage-collector tuning flags built into the launcher",
  "实例的额外 JVM 参数": "Extra JVM arguments from the instance",
  "全局的额外 JVM 参数": "Extra JVM arguments from the global settings",
  "Forge / NeoForge 需要的库目录声明":
    "Library directory declaration required by Forge / NeoForge",
  启动器拼出的类路径: "Classpath assembled by the launcher",
  版本文件声明的日志配置: "Logging configuration declared by the version file",
  "版本文件自带的 JVM 参数": "JVM arguments bundled with the version file",
  版本文件自带的游戏参数: "Game arguments bundled with the version file",
  版本文件声明的主类: "Main class declared by the version file",
  旧版版本文件的库路径与类路径:
    "Native path and classpath from a legacy version file",
  实例的额外游戏参数: "Extra game arguments from the instance",
  全局的额外游戏参数: "Extra game arguments from the global settings",
  "插件前置的 JVM 参数": "JVM arguments prepended by a plugin",
  "插件追加的 JVM 参数": "JVM arguments appended by a plugin",
  插件前置的游戏参数: "Game arguments prepended by a plugin",
  插件追加的游戏参数: "Game arguments appended by a plugin",

  // ---- 实例详情页的溯源入口 ----
  "启动参数是怎么来的？": "Where did these launch arguments come from?",
  "逐条列出上次启动时每个参数由谁添加，以及哪些被后面的同名参数覆盖了。":
    "Lists who contributed each argument in the last launch, and which ones were overridden by a later argument with the same name.",

  // ---- AI 本地推理引擎（llama.cpp / Ollama / LM Studio）----
  "llama.cpp（本地）": "llama.cpp (Local)",
  "Ollama（本地）": "Ollama (Local)",
  "LM Studio（本地）": "LM Studio (Local)",
  "留空使用默认地址；修改过端口或地址时填写":
    "Leave empty to use the default address; fill in only if you changed the port or address",
  本地引擎通常无需填写: "Usually not needed for local engines",
  "本地引擎默认不校验 Key；若 llama-server 启动时设置了 --api-key，在这里填上即可":
    "Local engines don't verify keys by default; if llama-server was started with --api-key, enter it here",
  "从下方检测到的模型中选择，或手动输入":
    "Pick from the detected models below, or type it manually",
  本地引擎检测: "Local engine detection",
  "检测中…": "Detecting…",
  重新检测: "Re-detect",
  "正在连接 {url} …": "Connecting to {url} …",
  "已连接，发现 {count} 个模型，点击选用：":
    "Connected, {count} models found. Click one to select:",
  "已连接到引擎，但没有发现模型，请先在引擎侧加载 / 拉取模型":
    "Connected to the engine but no models found. Load or pull a model in the engine first",
  "未检测到本地引擎，请确认它已启动，然后点「重新检测」":
    'No local engine detected. Make sure it is running, then click "Re-detect"',
  "启动方式：终端运行 llama-server -m <模型文件.gguf> --host 127.0.0.1 --port 8080，保持窗口开启后点「重新检测」。":
    'How to start: run llama-server -m <model.gguf> --host 127.0.0.1 --port 8080 in a terminal, keep the window open, then click "Re-detect".',
  "启动方式：终端运行 ollama serve，模型用 ollama pull <名称> 拉取。若已在运行仍检测不到，需要设置环境变量 OLLAMA_ORIGINS=* 后重启 Ollama（跨域限制）。":
    "How to start: run ollama serve in a terminal, and pull models with ollama pull <name>. If it is already running but still not detected, set the environment variable OLLAMA_ORIGINS=* and restart Ollama (CORS restriction).",
  "启动方式：LM Studio → Developer → Start Server（默认端口 1234，保持 CORS 开启）。":
    "How to start: LM Studio → Developer → Start Server (default port 1234, keep CORS enabled).",
};

export default dict;
