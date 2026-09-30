/*
 * Translation chunk for the online-play page（联机页）.
 * Keys are Simplified Chinese source strings; missing entries fall back to the source.
 */
const dict: Record<string, string> = {
  "{0} 已复制到剪贴板": "{0} copied to clipboard",
  上次操作失败: "Last attempt failed",
  一键进服: "Join in one click",
  保存: "Save",
  "保存失败：{0}": "Save failed: {0}",
  关闭后台服务: "Stop background service",
  "关闭失败：{0}": "Failed to stop: {0}",
  关闭服务: "Stop service",
  准备中: "Preparing",
  出错: "Error",
  创建房间: "Create room",
  "创建房间失败：{0}": "Failed to create the room: {0}",
  加入房间: "Join room",
  "加入房间失败：{0}": "Failed to join the room: {0}",
  取消: "Cancel",
  可用: "Ready",
  后台服务: "Background service",
  后台服务已关闭: "Background service stopped",
  启动器服务器: "Launcher server",
  启动失败: "Launch failed",
  "启动失败：{0}": "Launch failed: {0}",
  "和朋友一起玩：开个房间，或者加入别人的房间。":
    "Play together: host a room, or join someone else's.",
  复制: "Copy",
  "复制失败，请手动选中文本复制":
    "Copy failed — please select the text and copy it manually",
  "将断开隧道并让中继释放端口，房间里的玩家会立刻掉线。":
    "This disconnects the tunnel and releases the relay port; everyone in the room drops immediately.",
  "将结束本机的陶瓦联机进程。如果它是你自己打开的窗口，那个窗口也会一起关闭。":
    "This ends the local Terracotta process. If you opened that window yourself, it closes too.",
  "将转发到 127.0.0.1:{0}（端口取自服务器配置）":
    "Traffic goes to 127.0.0.1:{0} (port taken from the server settings)",
  尚未找到可执行文件: "No executable found yet",
  已停止: "Stopped",
  已加入的房间: "Joined room",
  已启动游戏并连接服务器: "Game launched and connected to the server",
  "已生成新的 API Key，保存后生效": "New API key generated — save to apply",
  "已找到可执行文件，路径见右侧设置。":
    "Executable found; the path is shown in the settings panel.",
  "已经接入房主的局域网，点下面的「一键进服」就能直接进游戏。":
    "You are on the host's LAN now — use “Join in one click” below to enter the game.",
  已连接: "Connected",
  已记录房间地址: "Address recorded",
  已进入房间: "In the room",
  "已选择 {0}，保存后生效": "Selected {0} — save to apply",
  开始于: "Started at",
  当前连接数: "Active connections",
  成员: "Member",
  我创建的房间: "My room",
  房主: "Host",
  "房主发来的是一段公网地址，粘进来即可保存并一键进服；房客不需要装模组。":
    "The host sends a public address; paste it to save it and join in one click. Guests need no mods.",
  房主需装模组: "Host needs the mod",
  房间地址: "Room address",
  房间已就绪: "Room ready",
  房间成员: "Room members",
  房间码: "Room code",
  打开项目主页: "Open project page",
  "把地址发给朋友，Ta 在游戏里「直接连接」就能进来；转发目标在下面。":
    "Send the address to your friends — they can use “Direct Connect” in game. The forward target is shown below.",
  "把房主发来的房间码粘进来即可加入；连上后在游戏里直连 127.0.0.1 就能进服。":
    "Paste the room code the host gave you to join; once connected, connect to 127.0.0.1 in game.",
  "把房间码发给朋友，Ta 在联机页粘贴就能加入。":
    "Send the room code to your friends — they can paste it on this page to join.",
  "按提示处理后可以重新建房或加入。":
    "Follow the hint above, then host or join again.",
  断开隧道: "Disconnect tunnel",
  未保存: "Unsaved",
  未运行: "Not running",
  本机地址: "Local address",
  "本机还没有陶瓦联机：请先下载并解压，再在右侧设置里选中它的可执行文件。":
    "Terracotta is not installed yet: download and extract it, then pick its executable in the settings panel.",
  本机隧道: "Local tunnel",
  "检查中继地址与本地服务端地址后可以重试。":
    "Check the relay and local server addresses, then try again.",
  "正在与房主建立虚拟局域网，第一次连接通常需要十几秒。":
    "Setting up the virtual LAN with the host; the first connection usually takes ten-odd seconds.",
  "正在与房主建立虚拟局域网，首次连接通常需要十几秒。":
    "Setting up the virtual LAN with the host; the first connection usually takes ten-odd seconds.",
  "正在准备陶瓦联机…": "Preparing Terracotta…",
  "正在创建房间…": "Creating the room…",
  "正在向中继服务器登记 API Key。": "Registering the API key with the relay.",
  "正在向中继申请端口，通常一两秒。":
    "Requesting a port from the relay; usually a second or two.",
  "正在启动本机服务器…": "Starting the local server…",
  "要转发的服务器还没运行，先把它启动起来。":
    "The server you are forwarding is not running yet — starting it first.",
  "正在寻找局域网世界…": "Looking for a LAN world…",
  "正在建立虚拟局域网…": "Building the virtual LAN…",
  "正在注册联机密钥…": "Registering the online key…",
  "正在申请公网隧道…": "Requesting the public tunnel…",
  "正在申请联机房间，请稍候。": "Requesting a room, please wait.",
  "正在连接房主，请稍候。": "Connecting to the host, please wait.",
  "正在连接房间…": "Connecting to the room…",
  "点「创建房间」开一局，或粘贴朋友的房间码加入。":
    "Use “Create room” to start, or paste a friend's room code to join.",
  "点「创建房间」把本机服务器发布到公网，或粘贴房主给的地址加入。":
    "Use “Create room” to publish your local server, or paste the host's address to join.",
  "点「退出房间」后可以重新建房或加入。":
    "Leave the room, then host or join again.",
  "生成失败：{0}": "Generation failed: {0}",
  留空使用当前账号名: "Leave empty to use the current account name",
  空闲: "Idle",
  红石联机: "Redstone Online",
  "红石联机的房客不需要装任何东西，直接连就行。":
    "Guests on Redstone Online need nothing installed — just connect.",
  "红石联机的房客不用装任何东西：点下面的「一键进服」，或在游戏里手动连接上面的地址。":
    "Redstone Online guests need nothing installed: use “Join in one click” below, or connect to the address above in game.",
  多人: "Multiplayer",
  联机: "Online",
  "联机会话由启动器后台维持：切到别的页面也不会断，退出启动器时会自动收尾。":
    "The session runs in the launcher's background: switching pages won't drop it, and it is cleaned up when you quit.",
  联机地址: "Join address",
  联机设置: "Online settings",
  联机设置已保存: "Online settings saved",
  "第一次使用会先拉起本机服务，可能要几秒钟。":
    "The first run starts the local service; this can take a few seconds.",
  "转发启动器托管的服务器时不需要模组；如果要联机的是「对局域网开放」的存档，请先给这个实例装 RedstoneOnline 模组并用 /rs open 发布（模组只负责发布，隧道由启动器接管）。":
    "Forwarding a launcher-hosted server needs no mod. For a world opened to LAN, install the RedstoneOnline mod in that instance and publish it with /rs open — the mod only publishes, the launcher runs the tunnel.",
  "还没找到世界？先进入存档 → Esc → 对局域网开放，陶瓦会自动发现它。":
    "No world found yet? Enter a world, press Esc → Open to LAN, and Terracotta will find it.",
  "还没有启动器托管的服务器。可以先去「服务器」页创建一台，或者切到「本机地址」直接转发已经开放局域网的存档（例如 127.0.0.1:25565）。":
    "No launcher-hosted server yet. Create one on the Servers page, or switch to “Local address” to forward a world already opened to LAN (e.g. 127.0.0.1:25565).",
  退出房间: "Leave room",
  "退出房间失败：{0}": "Failed to leave the room: {0}",
  选择一台本机服务器: "Select a local server",
  选择可执行文件: "Choose executable",
  选择陶瓦联机可执行文件: "Select the Terracotta executable",
  重新检测: "Detect again",
  重新生成: "Regenerate",
  陶瓦联机: "Terracotta",
  "陶瓦联机是第三方开源项目（AGPL-3.0），需要单独下载；启动器只通过它的本地 HTTP 接口驱动它。":
    "Terracotta is a third-party open-source project (AGPL-3.0) that you download separately; the launcher only drives it through its local HTTP API.",
  隧道已就绪: "Tunnel ready",
  需要先准备: "Setup required",
  项目主页: "Project page",
  "默认使用官方中继节点，可以在右侧设置里改成你自己的节点。":
    "The official relay is used by default; you can point it at your own node in the settings panel.",
  转发到哪台服务器: "Which server to forward",
  运行中: "Running",
  请先填写房主给你的公网地址:
    "Enter the public address the host gave you first",
  请先填写房主给你的房间码: "Enter the room code the host gave you first",
  "请先填写要转发的本机地址，例如 127.0.0.1:25565":
    "Enter the local address to forward first, e.g. 127.0.0.1:25565",
  请先选择要转发的本机服务器: "Select the local server to forward first",
  "先进入存档 → Esc → 对局域网开放，再回到这里点「创建房间」；陶瓦会自动发现你的世界并生成房间码。":
    "Open a world, press Esc → Open to LAN, then come back and hit “Create room”; Terracotta finds your world and generates a room code.",
  "公网中继（frp）：给本机服务器或局域网世界分配一个公网地址，房客直接连，不用装任何东西。":
    "Public relay (frp): gives your local server or LAN world a public address that guests can connect to directly, with nothing installed.",
  "基于 EasyTier 的虚拟局域网：贴一个房间码就能连，不需要公网 IP，也不用改路由器。":
    "An EasyTier-based virtual LAN: share a room code and connect — no public IP, no router changes.",
  // 多供应商同时开着 + 红石联机中继节点（测速与自动优选）
  不可用: "Unavailable",
  内置: "Built-in",
  从节点列表选择: "Pick from the node list",
  保存节点列表: "Save node list",
  "另一家联机（{0}）的会话也在进行中，两家可以同时开着。":
    "A {0} session is also running — both providers can stay up at the same time.",
  切过去看看: "Switch to it",
  "共 {0} 个节点可用，最快 {1} ms": "{0} nodes reachable, fastest {1} ms",
  "所有节点都连不上，请检查网络或填写自己的节点":
    "No node is reachable — check your network or enter your own node",
  测速: "Speed test",
  "测速失败：{0}": "Speed test failed: {0}",
  自定义节点列表: "Custom node list",
  "自己的节点，每行一条：名称=地址。":
    "Your own nodes, one per line: name=address.",
  节点列表已保存: "Node list saved",
  "已选用最快节点 {0}（{1} ms）": "Using the fastest node {0} ({1} ms)",
  选择中继节点: "Choose a relay node",
  自动选最快节点: "Auto-pick the fastest node",
  "自动选节点失败：{0}": "Auto-selecting a node failed: {0}",
  在线玩家: "Players online",
};

export default dict;
