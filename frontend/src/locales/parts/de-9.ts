/*
 * Absturzdiagnose-Teil (crash diagnosis): Befunde, Handlungsempfehlungen und Dialog-Schaltflächen.
 */
const dict: Record<string, string> = {
  自动下载并安装: "Herunterladen und installieren",
  "已安装陶瓦联机 {0}，保存后生效":
    "Terracotta {0} installiert – wird nach dem Speichern wirksam",
  "安装失败：{0}": "Installation fehlgeschlagen: {0}",
  重启: "Neu starten",
  "重启失败：{0}": "Neustart fehlgeschlagen: {0}",
  崩溃自动重启: "Automatischer Neustart bei Absturz",
  "进程异常退出时自动重新启动（10 分钟内最多 3 次，手动停止不触发）":
    "Startet automatisch neu, wenn der Prozess abnormal beendet wird (max. 3-mal pro 10 Minuten; manuelles Stoppen löst es nicht aus)",
  已开启崩溃自动重启: "Automatischer Neustart bei Absturz aktiviert",
  已关闭崩溃自动重启: "Automatischer Neustart bei Absturz deaktiviert",
  内容: "Inhalte",
  "从 Modrinth 安装模组": "Mods von Modrinth installieren",
  "从 Modrinth 安装插件": "Plugins von Modrinth installieren",
  "只列适配 {0} 的版本":
    "Es werden nur Versionen aufgelistet, die zu {0} passen",
  "搜索名称，例如 sodium / essentials":
    "Nach Namen suchen, z. B. sodium / essentials",
  搜索: "Suchen",
  没有找到匹配的项目: "Keine passenden Projekte gefunden",
  安装: "Installieren",
  已安装模组: "Installierte Mods",
  已安装插件: "Installierte Plugins",
  个: "Einträge",
  改动在重启服务器后生效: "Änderungen werden nach einem Serverneustart wirksam",
  "还没有内容。可以从上面搜索安装，或把 jar 放进服务器目录。":
    "Noch keine Inhalte. Über die Suche oben installieren oder JAR-Dateien in den Serverordner legen.",
  删除内容: "Inhalt löschen",
  "将删除「{0}」，无法撤销。": "„{0}“ wird dauerhaft gelöscht.",
  已删除: "Gelöscht",
  玩家: "Spieler",
  在线玩家: "Spieler online",
  人: "Spieler",
  当前没有玩家在线: "Gerade sind keine Spieler online",
  "服务器未运行（在线列表需要 RCON）":
    "Der Server läuft nicht (die Online-Liste erfordert RCON)",
  踢出: "Kicken",
  封禁: "Bannen",
  输入要广播的内容: "Nachricht für den Broadcast",
  广播: "Broadcast",
  已广播: "Broadcast gesendet",
  "广播失败：{0}": "Broadcast fehlgeschlagen: {0}",
  白名单: "Whitelist",
  "服务器运行中：改动会作为指令立即生效。":
    "Der Server läuft: Änderungen werden sofort als Befehle angewendet.",
  "服务器已停止：改动直接写入名单文件（离线 UUID 自动补齐）。":
    "Der Server ist gestoppt: Änderungen werden direkt in die Listendateien geschrieben (Offline-UUIDs werden automatisch ergänzt).",
  "玩家名（1–16 位字母、数字或下划线）":
    "Spielername (1-16 Buchstaben, Ziffern oder Unterstriche)",
  加入白名单: "Zur Whitelist hinzufügen",
  设为管理员: "Zum Operator machen",
  "管理员（OP）": "Operatoren (OP)",
  权限等级: "Stufe",
  封禁名单: "Gebannte Spieler",
  无理由: "Kein Grund angegeben",
  移除: "Entfernen",
  "（空）": "(leer)",
  "已把 {0} 加入白名单": "{0} wurde zur Whitelist hinzugefügt",
  "已把 {0} 移出白名单": "{0} wurde von der Whitelist entfernt",
  "已把 {0} 设为管理员": "{0} wurde zum Operator gemacht",
  "已取消 {0} 的管理员": "{0} wurde der Operator-Status entzogen",
  封禁玩家: "Spieler bannen",
  "将把「{0}」加入封禁名单。": "„{0}“ wird zur Bannliste hinzugefügt.",
  由启动器封禁: "Über NekoLauncher gebannt",
  "已封禁 {0}": "{0} wurde gebannt",
  "已解封 {0}": "{0} wurde entbannt",
  "已踢出 {0}": "{0} wurde gekickt",
  已开启白名单: "Whitelist aktiviert",
  已关闭白名单: "Whitelist deaktiviert",
  "操作失败：{0}": "Vorgang fehlgeschlagen: {0}",
  备份: "Backups",
  自动备份: "Automatische Backups",
  "开启后每 {0} 小时自动备份一次；每次备份前都会先把世界刷盘。":
    "Erstellt alle {0} Stunden automatisch ein Backup; vor jedem Backup wird die Welt zunächst auf die Festplatte geschrieben.",
  "间隔（小时）": "Intervall (Stunden)",
  "保留份数（0 = 不限）": "Anzahl behalten (0 = unbegrenzt)",
  "保留天数（0 = 不限）": "Tage behalten (0 = unbegrenzt)",
  立即备份: "Jetzt Backup erstellen",
  保存备份策略: "Backup-Richtlinie speichern",
  备份策略已保存: "Backup-Richtlinie gespeichert",
  "服务器已停止，备份会直接打包目录。":
    "Der Server ist gestoppt, daher wird der Ordner direkt archiviert.",
  "服务器运行中：将先暂停世界写入（save-off）再打包，完成后自动恢复。":
    "Der Server läuft: Das Speichern der Welt wird beim Archivieren pausiert (save-off) und danach automatisch fortgesetzt.",
  备份列表: "Backups",
  份: "Einträge",
  刷新: "Aktualisieren",
  "还没有备份。点「立即备份」创建第一份。":
    "Noch keine Backups – klicke auf „Jetzt Backup erstellen“, um das erste anzulegen.",
  热备份: "Hot-Backup",
  冷备份: "Kalt-Backup",
  恢复: "Wiederherstellen",
  恢复备份: "Backup wiederherstellen",
  "将用「{0}」覆盖当前服务器目录（存档、配置、mods 全部回到那一刻）。恢复前会自动留一份当前状态的安全备份。":
    "„{0}“ überschreibt den aktuellen Serverordner (Welten, Konfigurationen und Mods kehren auf diesen Zeitpunkt zurück). Vor der Wiederherstellung wird automatisch ein Sicherheitsbackup des aktuellen Zustands angelegt.",
  "已恢复备份 {0}": "Backup {0} wiederhergestellt",
  "恢复失败：{0}": "Wiederherstellung fehlgeschlagen: {0}",
  "已创建备份 {0}": "Backup {0} erstellt",
  "备份失败：{0}": "Backup fehlgeschlagen: {0}",
  删除备份: "Backup löschen",
  "将删除备份「{0}」，无法撤销。": "Backup „{0}“ wird dauerhaft gelöscht.",
  已删除备份: "Backup gelöscht",
  "迁移/备份整包服务器（含存档与配置）；需先停止服务器":
    "Gesamten Server migrieren/sichern (inkl. Welten und Konfigurationen); der Server muss dafür zuerst gestoppt werden",
  "下载限速（KB/s）": "Download-Limit (KB/s)",
  "0 表示不限速；填 1~1048576 之间的值（单位 KB/s）。限速是所有下载任务共享的总带宽。":
    "0 bedeutet keine Begrenzung; gib einen Wert zwischen 1 und 1048576 ein (in KB/s). Das Limit ist die Gesamtbandbreite, die sich alle Downloadaufgaben teilen.",
  保存下载设置: "Downloadeinstellungen speichern",
  "已保存：{0} 线程，限速 {1} KB/s": "Gespeichert: {0} Threads, Limit {1} KB/s",
  "已保存：{0} 线程，不限速": "Gespeichert: {0} Threads, unbegrenzt",
  复制诊断信息: "Diagnoseinformationen kopieren",
  打开崩溃报告: "Absturzbericht öffnen",
  打开崩溃报告失败: "Absturzbericht konnte nicht geöffnet werden",
  诊断信息已复制到剪贴板: "Diagnoseinformationen in die Zwischenablage kopiert",
  "已生成崩溃报告，并识别出可能的原因（见下方）。":
    "Ein Absturzbericht wurde erstellt und mögliche Ursachen erkannt (siehe unten).",
  "从启动日志里识别出可能的原因（见下方）。":
    "Mögliche Ursachen wurden anhand des Startprotokolls erkannt (siehe unten).",
  "已找到崩溃报告，但没能自动判断原因；可以把诊断信息发给开发者。":
    "Ein Absturzbericht wurde gefunden, aber die Ursache konnte nicht automatisch ermittelt werden – du kannst die Diagnoseinformationen an die Entwickler senden.",
  "没有找到崩溃报告，也没有识别出已知原因；可以把诊断信息发给开发者。":
    "Es wurde kein Absturzbericht gefunden und keine bekannte Ursache erkannt – du kannst die Diagnoseinformationen an die Entwickler senden.",
  "内存不足（Java 堆溢出）":
    "Zu wenig Arbeitsspeicher (Java-Heap ausgeschöpft)",
  "到「实例 → 内存」把最大内存调大（当前值见诊断信息），或减少同时加载的模组/光影。":
    "Erhöhe den maximalen Arbeitsspeicher unter Instanzen → Arbeitsspeicher (der aktuelle Wert steht in den Diagnoseinformationen), oder lade weniger Mods/Shader-Pakete.",
  "Java 版本不匹配": "Java-Version passt nicht",
  "该版本要求更高的 Java（或不能高于某个大版本）：到「设置 → Java」换一个 JDK 后重试。":
    "Diese Version benötigt ein anderes Java (bzw. darf eine bestimmte Hauptversion nicht überschreiten): wähle unter Einstellungen → Java ein anderes JDK und versuche es erneut.",
  缺少前置模组或模组冲突: "Fehlende Abhängigkeits-Mods oder Mod-Konflikt",
  "按报错里提到的模组名补齐前置（Fabric API / Architectury 等），或二分法禁用模组定位冲突项。":
    "Installiere die in der Fehlermeldung genannten Abhängigkeits-Mods (Fabric API, Architectury, …) oder finde den Konflikt, indem du immer die Hälfte der Mods deaktivierst.",
  "显卡驱动 / OpenGL 初始化失败":
    "Grafiktreiber / OpenGL-Initialisierung fehlgeschlagen",
  "更新显卡驱动；笔记本双显卡机型请在显卡控制面板里把 java 指定为独显；必要时改用其它渲染后端。":
    "Aktualisiere den Grafiktreiber; stelle bei Laptops mit zwei GPUs in der Grafiksteuerung java auf die dedizierte GPU um; wechsle falls nötig das Render-Backend.",
  "资源/依赖文件缺失或损坏": "Fehlende oder beschädigte Spieldateien",
  "到「设置 → 启动」打开「启动前校验文件」，或在实例页重新下载该版本以补全缺失文件。":
    "Aktiviere „Dateien vor dem Start prüfen“ unter Einstellungen → Start oder lade die Version auf der Instanzseite erneut herunter, um fehlende Dateien zu ergänzen.",
  登录会话失效: "Anmeldesitzung abgelaufen",
  "到「账户」页重新登录该账号后再启动。":
    "Melde dich auf der Konten-Seite erneut mit diesem Konto an und starte danach.",
  "Minecraft 进程以非零退出码结束（退出代码：{0}）。\n\n{1}\n\n{2}":
    "Der Minecraft-Prozess wurde mit einem Exit-Code ungleich null beendet (Exit-Code: {0}).\n\n{1}\n\n{2}",
  从文件夹安装: "Aus Ordner installieren",
  "选择插件文件夹（需含 plugin.yaml）":
    "Wähle einen Plugin-Ordner (muss plugin.yaml enthalten)",
  作者: "Autor",
  简介: "Beschreibung",
  保存清单: "Manifest speichern",
  "请先填写插件 id 与名称": "Gib zuerst die Plugin-ID und den Namen ein",
  "正在创建插件骨架…": "Plugin-Grundgerüst wird erstellt…",
  "已创建插件骨架：{0}": "Plugin-Grundgerüst erstellt: {0}",
  "创建失败：{0}": "Erstellen fehlgeschlagen: {0}",
  "正在保存清单…": "Manifest wird gespeichert…",
  清单已保存: "Manifest gespeichert",
  关闭: "Schließen",
  编辑清单: "Manifest bearbeiten",
  编辑插件清单: "Plugin-Manifest bearbeiten",
  "正在编辑「{0}」（{1}）；id 不可修改。":
    "„{0}“ ({1}) wird bearbeitet; die ID kann nicht geändert werden.",
  "请先在左侧选择一个插件。": "Wähle links zuerst ein Plugin aus.",
  插件名称不能为空: "Der Plugin-Name darf nicht leer sein",
  "保存只改当前选中插件的清单元数据，id 与入口文件不可修改；要新建插件请到「创作中心 → 插件制作」。":
    "Speichern schreibt nur die Manifest-Metadaten des ausgewählten Plugins neu; ID und Einstiegsdatei lassen sich nicht ändern. Neue Plugins erstellst du unter Kreativzentrum → Plugin-Erstellung.",

  // NekoLauncher-S-Modus und NekoSolo-Installationspaket
  启动: "Starten",
  "NekoLauncher-S 简洁模式": "NekoLauncher-S einfacher Modus",
  简洁模式: "Einfacher Modus",
  "只保留启动、外观、下载、账号与设置五个页面；NekoSolo 安装包装的启动器默认开启":
    "Behält nur fünf Seiten: Starten, Aussehen, Download, Konten und Einstellungen. Bei Launchern aus NekoSolo-Paketen standardmäßig aktiviert.",
  "NekoLauncher-S 模式已开启：侧边栏固定为五个页面，页面管理暂时不可用。关闭 S 模式后可继续调整。":
    "NekoLauncher-S ist aktiv: Die Seitenleiste ist auf fünf Seiten fixiert und die Seitenverwaltung ist vorübergehend nicht verfügbar. Schalte den einfachen Modus aus, um weiter anzupassen.",
  "NekoSolo 安装包": "NekoSolo-Installer",
  "把启动器、Java 与整合包打进单个 exe，玩家双击即玩（仅 Windows）":
    "Packt Launcher, Java und das Modpack in eine einzige exe – Spieler doppelklicken einfach und los geht es (nur Windows).",
  "NekoSolo 安装包（仅 Windows）：以 Modrinth 整合包格式打包，exe 只有几 MB，玩家双击安装即得启动器与整合包（默认开启 NekoLauncher-S 简洁模式）。在 Modrinth 上找不到的 mod 与配置会随包安装；其余 mod、Minecraft 本体与 Java 运行时由玩家首次启动启动器时联网下载。":
    "NekoSolo-Installer (nur Windows): als Modrinth-Modpack gepackt, die exe ist nur wenige MB groß – Spieler installieren per Doppelklick und erhalten Launcher samt Modpack (NekoLauncher-S einfacher Modus standardmäßig aktiv). Mods, die auf Modrinth nicht zu finden sind, und Konfigurationen werden mit dem Paket installiert; alle übrigen Mods, der Minecraft-Client und die Java-Laufzeitumgebung werden beim ersten Start des Launchers heruntergeladen.",
  "同时导出一份 .mrpack 整合包（可导入 Prism / HMCL 等其他启动器）":
    "Zusätzlich ein .mrpack-Modpack exportieren (importierbar in Prism, HMCL und andere Launcher)",
  "已同时导出 .mrpack：{0}": "Zusätzlich als .mrpack exportiert: {0}",
  "正在导出 .mrpack 整合包…": ".mrpack-Modpack wird exportiert…",
  "已保存：{0}（声明直链 {1} 个、随包内容 {2} 个）":
    "Gespeichert: {0} ({1} Direktlink-Dateien, {2} paketierte Dateien)",
  "解析 NekoSolo 安装包失败":
    "NekoSolo-Installer konnte nicht analysiert werden",
  "打包的模组、资源包等第三方内容会随安装包一起分发：请确认你有权再分发它们（CurseForge 上标记为「不允许第三方分发」的模组尤其需要注意）。":
    "Mods, Ressourcenpakete und andere Inhalte von Drittanbietern werden mit diesem Installer weiterverbreitet: Stelle sicher, dass du das Recht hast, sie weiterzuverteilen – besonders bei Mods, die auf CurseForge als „keine Weiterverteilung durch Dritte“ markiert sind, ist Vorsicht geboten.",
  "未找到 NekoSolo 安装器模板（NekoSolo/build/NekoSolo.Installer.exe），请先构建 NekoSolo 安装器，否则导出会失败。":
    "NekoSolo-Installer-Vorlage nicht gefunden (NekoSolo/build/NekoSolo.Installer.exe). Baue zuerst den NekoSolo-Installer, sonst schlägt der Export fehl.",
  "捆绑当前 Java 运行时（推荐，玩家无需自备 Java）":
    "Die aktuelle Java-Laufzeitumgebung einbinden (empfohlen; Spieler brauchen kein eigenes Java)",
  "已保存：{0}（版本文件 {1} 个、整合包内容 {2} 个）":
    "Gespeichert: {0} ({1} Versionsdateien, {2} Modpack-Inhaltsdateien)",
  高级设置: "Erweitert",
  "置顶/取消置顶": "Anheften / Lösen",
  重命名: "Umbenennen",
  编辑并重发: "Bearbeiten und erneut senden",
  保存并重发: "Speichern und erneut senden",
  总是允许: "Immer erlauben",
  系统提示: "System",
  已自动压缩较早的对话历史以释放上下文:
    "Ältere Unterhaltungen wurden automatisch komprimiert, um Kontext freizugeben",
  "正在整理较早的对话…": "Ältere Nachrichten werden aufgeräumt…",
  已总是允许的工具: "Immer erlaubte Tools",
  "本运行期内这些修改类操作不再需要批准，重启启动器后自动重置":
    "Diese ändernden Aktionen benötigen in dieser Sitzung keine Genehmigung mehr; beim Neustart des Launchers wird alles automatisch zurückgesetzt",
  撤销全部: "Alle widerrufen",
  继续执行: "Fortfahren",
  继续: "Weiter",
  模组中文名: "Mod-Namen auf Chinesisch",
  "「已安装模组」列表中的中文译名优先来自 SCL 社区译名数据集，未收录时通过模组文件名检索 MC百科（mcmod.cn）获得，数据与译名版权归 MC百科 所有。":
    "Chinesische Namen in der Liste „Installierte Mods“ stammen vorrangig aus dem SCL-Community-Datensatz; fehlt ein Eintrag, wird über den Mod-Dateinamen auf MC Encyclopedia (mcmod.cn) gesucht. Daten und Übersetzungen sind Eigentum von MC Encyclopedia.",
  "实现方式参考了 PCL2 的同名功能与 SCL 启动器的译名数据集，特此致谢。":
    "Die Implementierung ist von der gleichnamigen Funktion in PCL2 und dem Übersetzungsdatensatz des SCL-Launchers inspiriert – vielen Dank dafür.",
  "重命名…": "Umbenennen…",
  "复制实例…": "Instanz duplizieren…",
  打开版本文件夹: "Versionsordner öffnen",
  打开游戏文件夹: "Spielordner öffnen",
  "让 AI 分析": "KI fragen",
  "版本管理…": "Versionsverwaltung…",
  "缺少可用的 Java": "Kein brauchbares Java gefunden",
  "去配置 Java": "Java konfigurieren",
  全部更新: "Alle aktualisieren",
  开始更新: "Aktualisierung starten",
  "AI 诊断": "KI-Diagnose",
  "已跳过 {0} 个预发布版本": "{0} Vorabversionen übersprungen",
  "全部更新（{0}）": "Alle aktualisieren ({0})",
  "正在更新 {0}…（{1}/{2}）": "{0} wird aktualisiert… ({1}/{2})",
  "全部更新完成：成功 {0} 个，失败 {1} 个。":
    "Alle Updates abgeschlossen: {0} erfolgreich, {1} fehlgeschlagen.",
  "将按顺序为 {0} 个文件下载新版本并备份替换，期间请勿关闭启动器。跳过预发布版本。":
    "Für {0} Dateien werden nacheinander neue Versionen heruntergeladen und mit Backup ersetzt. Schließe den Launcher währenddessen nicht. Vorabversionen werden übersprungen.",
  "帮我分析当前实例里的模组「{0}」：它是做什么的、和其他常见模组有没有已知的兼容性问题？":
    "Analysiere bitte die Mod „{0}“ in meiner aktuellen Instanz: Was macht sie, und gibt es bekannte Kompatibilitätsprobleme mit anderen verbreiteten Mods?",
  "我的游戏启动失败了，请帮我诊断最近的崩溃原因：先看启动日志和崩溃报告，告诉我具体哪里出了问题、怎么修。":
    "Mein Spiel ließ sich nicht starten. Bitte diagnostiziere den letzten Absturz: Sieh dir Startprotokoll und Absturzbericht an und sage mir genau, was schiefgelaufen ist und wie ich es beheben kann.",
  "我装的模组没有生效，请帮我检查当前实例的模组列表和启动日志，找出加载失败或缺少前置的模组。":
    "Eine installierte Mod zeigt keine Wirkung. Bitte prüfe die Mod-Liste und das Startprotokoll meiner aktuellen Instanz und finde Mods, die nicht geladen wurden oder deren Abhängigkeiten fehlen.",
  "已安装模组 {0}。": "Mod {0} installiert.",
  "安装 {0} 失败：{1}": "{0} konnte nicht installiert werden: {1}",
  "已导入存档 {0}。": "Welt {0} importiert.",
  "已导入 {0}。": "{0} importiert.",
  "导入 {0} 失败：{1}": "{0} konnte nicht importiert werden: {1}",
  "请先选择一个实例，再拖入文件。":
    "Wähle zuerst eine Instanz aus und ziehe dann Dateien hinein.",
  "把拖入的文件装到哪？": "Wohin soll die gezogene Datei installiert werden?",
  "松开以安装（模组 .jar / 资源包·光影·存档 .zip）":
    "Zum Installieren loslassen (Mods .jar / Ressourcenpakete · Shader-Pakete · Welten .zip)",
  "不支持的文件类型：{0}（仅支持 .jar 模组与 .zip 资源包/光影/存档）":
    "Nicht unterstützter Dateityp: {0} (nur .jar-Mods und .zip-Ressourcenpakete / Shader-Pakete / Welten)",
  "{0}-副本": "{0} (Kopie)",
  快照目标: "Snapshot-Ziel",
  整个实例: "Gesamte Instanz",
  为该存档快捷创建快照: "Schnell-Snapshot dieser Welt erstellen",
  "已为存档「{0}」创建快照：{1}": "Snapshot für Welt „{0}“ erstellt: {1}",
  定时创建快照: "Geplante Snapshots",
  定时快照: "Geplanter Snapshot",
  "定时快照已创建：{0}": "Geplanter Snapshot erstellt: {0}",
  快照间隔: "Intervall",
  "{0} 分钟": "{0} Min.",
  "每 {0} 分钟自动为当前目标创建一份快照（备注「定时快照」）。":
    "Erstellt alle {0} Minuten automatisch einen Snapshot des aktuellen Ziels (Vermerk „Geplanter Snapshot“).",
  "开启后按固定间隔自动创建快照，适合边玩边备份。":
    "Erstellt automatisch Snapshots in festen Abständen – ideal für Backups während des Spielens.",
  一键快照: "Schnell-Snapshot",
  主页快捷快照: "Schnell-Snapshot auf der Startseite",
  "请先在实例页选择一个实例。":
    "Wähle zuerst auf der Instanz-Seite eine Instanz aus.",
  存档: "Welten",
  "还没有存档：进游戏创建一个世界后，这里就能直接拍快照":
    "Noch keine Welten: Erstelle im Spiel eine Welt, dann kannst du hier direkt einen Snapshot machen",
  在左侧选择一个存档: "Wähle links eine Welt aus",
  该存档还没有快照: "Für diese Welt gibt es noch keine Snapshots",
  "读取中…": "Wird geladen…",
  "{0} 个文件 · {1}": "{0} Dateien · {1}",
  样式文件: "Stildateien",
  "逗号分隔，加载时自动注入为全局 CSS，可自定义控件样式":
    "Kommagetrennt; wird beim Laden automatisch als globales CSS injiziert, um Steuerelemente frei zu gestalten",
  // ---- Neue Texte für Auto-Update / Über / Instanz-Einstellungen usw. ----
  "启动时自动检查 GitHub Releases，有新版本时弹窗询问（不会静默替换）":
    "Prüft GitHub Releases beim Start und fragt bei einer neuen Version per Dialog nach (ersetzt nie stillschweigend)",
  自动检查更新: "Automatische Update-Prüfung",
  "更新通道：预览版最先拿到新功能，稳定版只收正式发布（预览版较少时可能长期无更新）":
    "Update-Kanal: Preview erhält neue Funktionen zuerst; Stable bekommt nur offizielle Releases (bei wenigen Previews kann es längere Zeit keine Updates geben)",
  更新通道: "Update-Kanal",
  "发现新版本 {0}，可到 设置→关于 更新":
    "Neue Version {0} gefunden – unter Einstellungen → Über aktualisieren",
  发现新版本: "Neue Version gefunden",
  "启动器有新版本 {0} 可用{1}，是否立即下载并重启更新？":
    "Version {0} des Launchers ist verfügbar{1}. Jetzt herunterladen und zum Aktualisieren neu starten?",
  稍后: "Später",
  立即更新: "Jetzt aktualisieren",
  "正在下载新版本 {0}…": "Neue Version {0} wird heruntergeladen…",
  "自动更新失败：{0}": "Automatisches Update fehlgeschlagen: {0}",
  "[plugins] {0} 的样式文件 {1} 注入失败：{2}":
    "[plugins] Stildatei {1} von {0} konnte nicht injiziert werden: {2}",
  "版本筛选失败，回退本地筛选":
    "Versionsfilterung fehlgeschlagen; Rückfall auf lokale Filterung",
  "没有找到匹配的{0}": "Keine Treffer für {0}",
  低: "Niedrig",
  低于普通: "Niedriger als normal",
  高于普通: "Höher als normal",
  高: "Hoch",
  "打开文件夹失败：{0}": "Ordner konnte nicht geöffnet werden: {0}",
  "保存失败：设置不合法（窗口尺寸/内存取值超界）。":
    "Speichern fehlgeschlagen: ungültige Einstellungen (Fenstergröße/Arbeitsspeicher außerhalb des gültigen Bereichs).",
  "已复制为 {0}。": "Als {0} dupliziert.",
  "启动指令已发出。": "Startbefehl gesendet.",
  "导入失败：路径无效或已存在。":
    "Import fehlgeschlagen: Pfad ungültig oder bereits vorhanden.",
  "导入失败：{0}": "Import fehlgeschlagen: {0}",
  "切换失败：{0}": "Wechsel fehlgeschlagen: {0}",
  "存档已导出：{0}": "Welt exportiert: {0}",
  "导出失败：{0}": "Export fehlgeschlagen: {0}",
  "存档已导入：{0}": "Welt importiert: {0}",
  "版本隔离（模组、存档等分到实例目录）":
    "Versionsisolierung (Mods, Welten usw. liegen im Instanzordner)",
  "没有找到与「{query}」相关的设置":
    "Keine Einstellungen zu „{query}“ gefunden",
  "默认皮肤：{0}": "Standard-Skin: {0}",
  "创建离线账号失败：{0}": "Offline-Konto konnte nicht erstellt werden: {0}",
  "皮肤上传失败：{0}": "Skin-Upload fehlgeschlagen: {0}",
  "皮肤站账号请到对应皮肤站的网页端更换皮肤。":
    "Bei Skin-Server-Konten wechselst du den Skin bitte auf der Website des jeweiligen Skin-Servers.",
  "披风 {0}": "Cape {0}",
  游戏设置: "Spieleinstellungen",
  进程优先级: "Prozesspriorität",
  "包装命令（需含 %command% 占位）":
    "Wrapper-Befehl (muss den Platzhalter %command% enthalten)",
  全屏启动: "Im Vollbild starten",
  "额外环境变量（每行一个，格式 KEY=VALUE）":
    "Zusätzliche Umgebungsvariablen (eine pro Zeile, Format KEY=VALUE)",
  "搜索{0}名称…（Ctrl+F）": "Nach {0}-Namen suchen… (Ctrl+F)",
  "用文件哈希向 Modrinth 查询这些内容是否有新版本（不会自动改动文件）":
    "Prüft per Datei-Hash bei Modrinth, ob es für diese Inhalte neue Versionen gibt (Dateien werden nie automatisch verändert)",
  版本管理: "Versionsverwaltung",
  "版本管理（升级 / 降级）": "Versionsverwaltung (Upgrade / Downgrade)",
  游戏设置编辑模式: "Bearbeitungsmodus für Spieleinstellungen",
  可视化: "Visuell",
  原始编辑: "Raw-Bearbeitung",
  "加载中...": "Lädt...",
  "直接编辑 options.txt 原始内容，支持所有设置项（包括 Mod 添加的自定义选项）":
    "Den Rohinhalt von options.txt direkt bearbeiten; alle Einstellungen werden unterstützt (einschließlich benutzerdefinierter Optionen aus Mods)",
  "options.txt 原始内容": "Rohinhalt von options.txt",
  "未列出的设置项可切换到「原始编辑」模式":
    "Nicht aufgeführte Einstellungen kannst du im Modus „Raw-Bearbeitung“ ändern",
  "格式为 key:value，每行一个": "Ein key:value pro Zeile",
  "复制实例（拷贝版本文件夹与启动设置，不复制共享的模组与存档）":
    "Instanz duplizieren (kopiert den Versionsordner und die Starteinstellungen; gemeinsam genutzte Mods und Welten werden nicht kopiert)",
  副本名称: "Name der Kopie",
  创建副本: "Kopie erstellen",
  "版本隔离实例会连同其模组、配置与存档一起复制；共享目录实例只复制版本文件。":
    "Bei isolierten Instanzen werden Mods, Konfiguration und Welten mitkopiert; Instanzen mit gemeinsamem Ordner kopieren nur die Versionsdateien.",
  更新详情: "Update-Details",
  "全部版本 / 降级": "Alle Versionen / Downgrade",
  重命名实例: "Instanz umbenennen",
  复制实例: "Instanz duplizieren",
  关于: "Über",
  版本: "Version",
  更新: "Aktualisieren",
  管理: "Verwalten",
  保存: "Speichern",
  光影包: "Shader-Pakete",
  原版: "Vanilla",
  新版本: "Neue Version",
  可更新: "Update verfügbar",
  大小未知: "Größe unbekannt",
  未知: "Unbekannt",
  清空: "Leeren",
  共: "Gesamt",
  个版本: " Versionen",
  已取消: "Abgebrochen",
  取消: "Abbrechen",
  普通: "Normal",
  快照: "Snapshot",
  快照版: "Snapshots",
  正式版: "Releases",
  "正在打开微软登录页，请在页面中完成授权…":
    "Die Microsoft-Anmeldeseite wird geöffnet – bitte schließe die Autorisierung auf der Seite ab…",
  "内嵌登录启动失败：{0}":
    "Anmeldung im Fenster konnte nicht gestartet werden: {0}",
  "启动器内登录（推荐）": "Im Launcher anmelden (empfohlen)",
  "在当前窗口打开微软登录页，授权后自动返回，无需外部浏览器。":
    "Öffnet die Microsoft-Anmeldeseite im aktuellen Fenster und kehrt nach der Autorisierung automatisch zurück – ganz ohne externen Browser.",
  使用设备码登录: "Mit Gerätecode anmelden",
};

const musicDict: Record<string, string> = {
  // ---- Musikplayer (Version mit drei Spalten) ----
  曲库: "Bibliothek",
  收藏: "Favoriten",
  取消收藏: "Aus Favoriten entfernen",
  最近播放: "Zuletzt gespielt",
  播放队列: "Warteschlange",
  暂无歌曲: "Noch keine Songs",
  "还没有收藏，点击曲目右侧的心心加入收藏":
    "Noch keine Favoriten – tippe auf das Herz rechts neben einem Titel, um ihn hinzuzufügen",
  暂无播放记录: "Noch kein Wiedergabeverlauf",
  "队列为空，从曲库点播歌曲加入":
    "Die Warteschlange ist leer – spiel einen Song aus der Bibliothek ab, um sie zu füllen",
  载入全部曲目: "Alle Titel laden",
  清空队列: "Warteschlange leeren",
  从队列移除: "Aus der Warteschlange entfernen",
  "{0} 前播放": "Vor {0} abgespielt",
  很久: "vor langer Zeit",
  分钟: "Min.",
  小时: "Std.",
  天: "T.",
  个月: "Mon.",
  迷你模式: "Mini-Modus",
  退出迷你模式: "Mini-Modus verlassen",
  "快退 10 秒": "10 Sek. zurück",
  "快进 10 秒": "10 Sek. vor",
  倍速播放: "Wiedergabegeschwindigkeit",
  点击切换: "zum Wechseln klicken",
  均衡器: "Equalizer",
  预设: "Preset",
  流行: "Pop",
  摇滚: "Rock",
  古典: "Klassik",
  人声: "Gesang",
  "均衡器由 Web Audio 实时处理，预设与增益自动保存。":
    "Wird in Echtzeit über Web Audio verarbeitet. Presets und Verstärkungen werden automatisch gespeichert.",
  歌词全屏: "Songtext-Vollbild",
  从曲库选一首开始听吧:
    "Such dir einen Song aus der Bibliothek aus und leg los",
  "整合包（.mrpack / .zip / .exe）进导入流程，模组 / 资源包 / 光影 / 存档装进当前实例":
    "Modpacks (.mrpack / .zip / .exe) gehen in den Import; Mods, Ressourcenpakete, Shader und Welten werden in die aktuelle Instanz installiert",
  存档已导入当前实例: "Welt in die aktuelle Instanz importiert",
  "导入存档失败：{0}": "Welt-Import fehlgeschlagen: {0}",
};

Object.assign(dict, musicDict);

export default dict;
