/*
 * 补漏分片：可行性复检时统计出的「源码里有、词典里缺」的条目
 * （这些条目在英文界面会回退成中文，故补齐）。ja / ru 经 FALLBACKS 走本词典。
 */
const dict: Record<string, string> = {
  "图形化点选给物品 / 给效果 / 传送等命令，实时拼出可粘贴的指令":
    "Pick items / effects / teleports graphically and get a paste-ready command in real time",
  "将立即结束服务器进程，不给保存世界的机会，可能丢失未落盘的进度。仅在服务器卡死、软停止无效时使用。":
    "Ends the server process immediately without giving the world a chance to save; unsaved progress may be lost. Use only when the server is stuck and a graceful stop does not work.",
  "整包备份服务器（含存档/配置/mods），可在其他机器导入恢复":
    "Back up the whole server (worlds / configs / mods); it can be imported on another machine",
  "预设为社区标准的 Aikar GC 调优参数，适合 12GB 以下的堆。":
    "Fills in the community-standard Aikar GC flags, suited to heaps under 12 GB.",
  "已开启自动调整：启动器按游戏版本与系统剩余内存自动分配，无需手动设置。":
    "Automatic sizing is on: the launcher allocates memory from the game version and free system memory, so no manual setting is needed.",
  "扫描实例失败：{0}": "Scanning instances failed: {0}",
  "搜索失败：{0}": "Search failed: {0}",
  "资源搜索走的是 Modrinth 官方接口（api.modrinth.com），网络不通或触发限流时会失败；稍后重试即可。":
    "Content search uses Modrinth's official API (api.modrinth.com); it fails when the network is blocked or rate-limited — just retry in a moment.",
  重试: "Retry",
  "Minecraft 进程以非零退出码结束（退出代码：{0}）。\n\n常见原因：Java 版本不匹配、内存分配不足、模组冲突或缺少前置。可打开启动日志查看具体报错。":
    "The Minecraft process exited with a non-zero code ({0}).\n\nCommon causes: mismatched Java version, too little memory, conflicting mods or a missing dependency. Open the launch log for the exact error.",
};

export default dict;
