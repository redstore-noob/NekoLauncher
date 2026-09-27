/*
 * 崩溃诊断（crash diagnosis）分片：判据结论、处置建议与弹窗按钮。
 */
const dict: Record<string, string> = {
  自动下载并安装: "Download and install",
  "已安装陶瓦联机 {0}，保存后生效": "Terracotta {0} installed — save to apply",
  "安装失败：{0}": "Install failed: {0}",
  重启: "Restart",
  "重启失败：{0}": "Restart failed: {0}",
  崩溃自动重启: "Auto-restart on crash",
  "进程异常退出时自动重新启动（10 分钟内最多 3 次，手动停止不触发）":
    "Restarts automatically when the process exits abnormally (max 3 times per 10 minutes; manual stops do not trigger it)",
  已开启崩溃自动重启: "Auto-restart enabled",
  已关闭崩溃自动重启: "Auto-restart disabled",
  内容: "Content",
  "从 Modrinth 安装模组": "Install mods from Modrinth",
  "从 Modrinth 安装插件": "Install plugins from Modrinth",
  "只列适配 {0} 的版本": "Only versions matching {0} are listed",
  "搜索名称，例如 sodium / essentials":
    "Search by name, e.g. sodium / essentials",
  搜索: "Search",
  没有找到匹配的项目: "No matching projects found",
  安装: "Install",
  已安装模组: "Installed mods",
  已安装插件: "Installed plugins",
  个: "items",
  改动在重启服务器后生效: "Changes take effect after a server restart",
  "还没有内容。可以从上面搜索安装，或把 jar 放进服务器目录。":
    "Nothing here yet — install from the search above, or drop jars into the server folder.",
  删除内容: "Delete content",
  "将删除「{0}」，无法撤销。": "“{0}” will be deleted permanently.",
  已删除: "Deleted",
  玩家: "Players",
  在线玩家: "Online players",
  人: "players",
  当前没有玩家在线: "No players online right now",
  "服务器未运行（在线列表需要 RCON）":
    "Server is not running (the online list needs RCON)",
  踢出: "Kick",
  封禁: "Ban",
  输入要广播的内容: "Message to broadcast",
  广播: "Broadcast",
  已广播: "Broadcast sent",
  "广播失败：{0}": "Broadcast failed: {0}",
  白名单: "Whitelist",
  "服务器运行中：改动会作为指令立即生效。":
    "Server is running: changes are applied immediately as commands.",
  "服务器已停止：改动直接写入名单文件（离线 UUID 自动补齐）。":
    "Server is stopped: changes go straight into the list files (offline UUIDs are filled in).",
  "玩家名（1–16 位字母、数字或下划线）":
    "Player name (1-16 letters, digits or underscores)",
  加入白名单: "Add to whitelist",
  设为管理员: "Make operator",
  "管理员（OP）": "Operators (OP)",
  权限等级: "Level",
  封禁名单: "Banned players",
  无理由: "No reason given",
  移除: "Remove",
  "（空）": "(empty)",
  "已把 {0} 加入白名单": "Added {0} to the whitelist",
  "已把 {0} 移出白名单": "Removed {0} from the whitelist",
  "已把 {0} 设为管理员": "Made {0} an operator",
  "已取消 {0} 的管理员": "Removed operator from {0}",
  封禁玩家: "Ban player",
  "将把「{0}」加入封禁名单。": "“{0}” will be added to the ban list.",
  由启动器封禁: "Banned via NekoLauncher",
  "已封禁 {0}": "Banned {0}",
  "已解封 {0}": "Unbanned {0}",
  "已踢出 {0}": "Kicked {0}",
  已开启白名单: "Whitelist enabled",
  已关闭白名单: "Whitelist disabled",
  "操作失败：{0}": "Operation failed: {0}",
  备份: "Backups",
  自动备份: "Automatic backups",
  "开启后每 {0} 小时自动备份一次；每次备份前都会先把世界刷盘。":
    "Backs up every {0} hours; the world is flushed to disk before each run.",
  "间隔（小时）": "Interval (hours)",
  "保留份数（0 = 不限）": "Keep count (0 = unlimited)",
  "保留天数（0 = 不限）": "Keep days (0 = unlimited)",
  立即备份: "Back up now",
  保存备份策略: "Save backup policy",
  备份策略已保存: "Backup policy saved",
  "服务器已停止，备份会直接打包目录。":
    "The server is stopped, so the directory is archived directly.",
  "服务器运行中：将先暂停世界写入（save-off）再打包，完成后自动恢复。":
    "Server is running: world saving is paused (save-off) while archiving, then restored automatically.",
  备份列表: "Backups",
  份: "items",
  刷新: "Refresh",
  "还没有备份。点「立即备份」创建第一份。":
    "No backups yet — use “Back up now” to create the first one.",
  热备份: "Hot",
  冷备份: "Cold",
  恢复: "Restore",
  恢复备份: "Restore backup",
  "将用「{0}」覆盖当前服务器目录（存档、配置、mods 全部回到那一刻）。恢复前会自动留一份当前状态的安全备份。":
    "“{0}” will overwrite the current server directory (worlds, configs and mods revert to that moment). A safety backup of the current state is taken first.",
  "已恢复备份 {0}": "Restored backup {0}",
  "恢复失败：{0}": "Restore failed: {0}",
  "已创建备份 {0}": "Backup {0} created",
  "备份失败：{0}": "Backup failed: {0}",
  删除备份: "Delete backup",
  "将删除备份「{0}」，无法撤销。": "Backup “{0}” will be deleted permanently.",
  已删除备份: "Backup deleted",
  "迁移/备份整包服务器（含存档与配置）；需先停止服务器":
    "Migrate / back up the whole server (worlds and configs); the server must be stopped first",
  "下载限速（KB/s）": "Download speed limit (KB/s)",
  "0 表示不限速；填 1~1048576 之间的值（单位 KB/s）。限速是所有下载任务共享的总带宽。":
    "0 means unlimited; enter 1-1048576 in KB/s. The limit is shared by all download tasks.",
  保存下载设置: "Save download settings",
  "已保存：{0} 线程，限速 {1} KB/s": "Saved: {0} threads, limited to {1} KB/s",
  "已保存：{0} 线程，不限速": "Saved: {0} threads, unlimited",
  复制诊断信息: "Copy diagnostics",
  打开崩溃报告: "Open crash report",
  打开崩溃报告失败: "Failed to open the crash report",
  诊断信息已复制到剪贴板: "Diagnostics copied to clipboard",
  "已生成崩溃报告，并识别出可能的原因（见下方）。":
    "A crash report was written and likely causes were identified (see below).",
  "从启动日志里识别出可能的原因（见下方）。":
    "Likely causes were identified from the launch log (see below).",
  "已找到崩溃报告，但没能自动判断原因；可以把诊断信息发给开发者。":
    "A crash report was found, but the cause could not be determined automatically — send the diagnostics to the developer.",
  "没有找到崩溃报告，也没有识别出已知原因；可以把诊断信息发给开发者。":
    "No crash report was found and no known cause matched — send the diagnostics to the developer.",
  "内存不足（Java 堆溢出）": "Not enough memory (Java heap exhausted)",
  "到「实例 → 内存」把最大内存调大（当前值见诊断信息），或减少同时加载的模组/光影。":
    "Raise the maximum memory under Instances → Memory (the current value is in the diagnostics), or load fewer mods/shaders.",
  "Java 版本不匹配": "Java version mismatch",
  "该版本要求更高的 Java（或不能高于某个大版本）：到「设置 → Java」换一个 JDK 后重试。":
    "This version needs a different Java: pick another JDK under Settings → Java and try again.",
  缺少前置模组或模组冲突: "Missing dependency mod or mod conflict",
  "按报错里提到的模组名补齐前置（Fabric API / Architectury 等），或二分法禁用模组定位冲突项。":
    "Install the dependency mods named in the error (Fabric API, Architectury, …), or disable mods in halves to find the conflict.",
  "显卡驱动 / OpenGL 初始化失败":
    "Graphics driver / OpenGL initialisation failed",
  "更新显卡驱动；笔记本双显卡机型请在显卡控制面板里把 java 指定为独显；必要时改用其它渲染后端。":
    "Update the graphics driver; on dual-GPU laptops force java onto the discrete GPU; if needed, switch rendering backend.",
  "资源/依赖文件缺失或损坏": "Missing or corrupted game files",
  "到「设置 → 启动」打开「启动前校验文件」，或在实例页重新下载该版本以补全缺失文件。":
    "Enable “Verify files before launch” under Settings → Launch, or re-download this instance to restore missing files.",
  登录会话失效: "Login session expired",
  "到「账户」页重新登录该账号后再启动。":
    "Sign in to that account again on the Accounts page, then launch.",
  "Minecraft 进程以非零退出码结束（退出代码：{0}）。\n\n{1}\n\n{2}":
    "The Minecraft process exited with a non-zero code ({0}).\n\n{1}\n\n{2}",
  从文件夹安装: "Install from folder",
  "选择插件文件夹（需含 plugin.yaml）":
    "Choose a plugin folder (must contain plugin.yaml)",
  作者: "Author",
  简介: "Description",
  保存清单: "Save manifest",
  "请先填写插件 id 与名称": "Enter the plugin id and name first",
  "正在创建插件骨架…": "Creating the plugin scaffold…",
  "已创建插件骨架：{0}": "Plugin scaffold created: {0}",
  "创建失败：{0}": "Creation failed: {0}",
  "正在保存清单…": "Saving the manifest…",
  清单已保存: "Manifest saved",
  关闭: "Close",
  编辑清单: "Edit manifest",
  编辑插件清单: "Edit plugin manifest",
  "正在编辑「{0}」（{1}）；id 不可修改。":
    "Editing “{0}” ({1}); the id cannot be changed.",
  "请先在左侧选择一个插件。": "Select a plugin on the left first.",
  插件名称不能为空: "Plugin name cannot be empty",
  "保存只改当前选中插件的清单元数据，id 与入口文件不可修改；要新建插件请到「创作中心 → 插件制作」。":
    "Saving only rewrites the selected plugin's manifest metadata; the id and entry file cannot change. To create a new plugin use Creator → Plugin maker.",

  // NekoLauncher-S 模式与 NekoSolo 安装包
  启动: "Launch",
  "NekoLauncher-S 简洁模式": "NekoLauncher-S simple mode",
  简洁模式: "Simple mode",
  "只保留启动、外观、下载、账号与设置五个页面；NekoSolo 安装包装的启动器默认开启":
    "Keeps only five pages: Launch, Appearance, Download, Accounts and Settings. Enabled by default for launchers installed via NekoSolo packages.",
  "NekoLauncher-S 模式已开启：侧边栏固定为五个页面，页面管理暂时不可用。关闭 S 模式后可继续调整。":
    "NekoLauncher-S is on: the sidebar is fixed to five pages and page management is unavailable. Turn off simple mode to customize again.",
  "NekoSolo 安装包": "NekoSolo installer",
  "NekoSolo 安装包（仅 Windows）：把启动器、Java 与整合包打进单个 exe，玩家双击即玩；安装的启动器默认开启 NekoLauncher-S 简洁模式。":
    "NekoSolo installer (Windows only): packs the launcher, Java and the modpack into a single exe — players just double-click to play. The installed launcher defaults to NekoLauncher-S simple mode.",
  "未找到 NekoSolo 安装器模板（NekoSolo/build/NekoSolo.Installer.exe），请先构建 NekoSolo 安装器，否则导出会失败。":
    "NekoSolo installer template not found (NekoSolo/build/NekoSolo.Installer.exe). Build the NekoSolo installer first, otherwise the export will fail.",
  "捆绑当前 Java 运行时（推荐，离线也能玩）":
    "Bundle the current Java runtime (recommended; playable offline)",
  "已保存：{0}（版本文件 {1} 个、整合包内容 {2} 个）":
    "Saved: {0} ({1} version files, {2} modpack content files)",
};

export default dict;
