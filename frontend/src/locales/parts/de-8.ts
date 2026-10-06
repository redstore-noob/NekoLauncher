/*
 * Ergänzungs-Abschnitt: bei der erneuten Machbarkeitsprüfung ermittelte Einträge,
 * die „im Quellcode vorhanden, aber im Wörterbuch fehlend“ sind (ohne sie würde die
 * englische Oberfläche auf Chinesisch zurückfallen, daher werden sie hier ergänzt).
 * ja / ru greifen über FALLBACKS auf dieses Wörterbuch zurück.
 */
const dict: Record<string, string> = {
  "图形化点选给物品 / 给效果 / 传送等命令，实时拼出可粘贴的指令":
    "Gegenstände / Effekte / Teleporte grafisch auswählen und in Echtzeit einen sofort einfügbaren Befehl erhalten",
  "将立即结束服务器进程，不给保存世界的机会，可能丢失未落盘的进度。仅在服务器卡死、软停止无效时使用。":
    "Beendet den Serverprozess sofort, ohne der Welt die Gelegenheit zum Speichern zu geben; noch nicht gespeicherter Fortschritt kann verloren gehen. Verwende dies nur, wenn der Server hängt und ein sanftes Beenden nichts bewirkt.",
  "整包备份服务器（含存档/配置/mods），可在其他机器导入恢复":
    "Erstellt ein Komplett-Backup des Servers (inkl. Welten/Konfigurationen/Mods), das auf einem anderen Rechner importiert und wiederhergestellt werden kann",
  "预设为社区标准的 Aikar GC 调优参数，适合 12GB 以下的堆。":
    "Trägt die Community-Standard-Parameter für die Aikar-GC-Optimierung ein, geeignet für Heaps unter 12 GB.",
  "已开启自动调整：启动器按游戏版本与系统剩余内存自动分配，无需手动设置。":
    "Die automatische Anpassung ist aktiviert: Der Launcher weist den Arbeitsspeicher automatisch anhand der Spielversion und des freien Systemarbeitsspeichers zu; eine manuelle Einstellung ist nicht nötig.",
  "扫描实例失败：{0}": "Scannen der Instanzen fehlgeschlagen: {0}",
  "该目录下没有发现 Minecraft 版本：{0}":
    "In diesem Verzeichnis wurden keine Minecraft-Versionen gefunden: {0}",
  "搜索失败：{0}": "Suche fehlgeschlagen: {0}",
  "资源搜索走的是 Modrinth 官方接口（api.modrinth.com），网络不通或触发限流时会失败；稍后重试即可。":
    "Die Inhaltssuche läuft über Modrinths offizielle API (api.modrinth.com); sie schlägt fehl, wenn das Netzwerk nicht erreichbar ist oder eine Ratenbegrenzung ausgelöst wird – versuche es gleich einfach noch einmal.",
  重试: "Erneut versuchen",
  "Minecraft 进程以非零退出码结束（退出代码：{0}）。\n\n常见原因：Java 版本不匹配、内存分配不足、模组冲突或缺少前置。可打开启动日志查看具体报错。":
    "Der Minecraft-Prozess wurde mit einem Exit-Code ungleich null beendet (Exit-Code: {0}).\n\nHäufige Ursachen: nicht passende Java-Version, zu wenig zugewiesener Arbeitsspeicher, Mod-Konflikte oder eine fehlende Abhängigkeit. Öffne das Startprotokoll, um die genaue Fehlermeldung zu sehen.",
};

export default dict;
