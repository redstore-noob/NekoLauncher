/*
 * Übersetzungsabschnitt für die Online-Seite (联机页).
 * Schlüssel sind vereinfacht-chinesische Ausgangszeichenketten; fehlende Einträge fallen auf die Ausgangszeichenkette zurück.
 */
const dict: Record<string, string> = {
  "{0} 已复制到剪贴板": "{0} wurde in die Zwischenablage kopiert",
  上次操作失败: "Letzter Versuch fehlgeschlagen",
  一键进服: "Mit einem Klick beitreten",
  保存: "Speichern",
  "保存失败：{0}": "Speichern fehlgeschlagen: {0}",
  关闭后台服务: "Hintergrunddienst beenden",
  "关闭失败：{0}": "Beenden fehlgeschlagen: {0}",
  关闭服务: "Dienst beenden",
  准备中: "Wird vorbereitet",
  出错: "Fehler",
  创建房间: "Raum erstellen",
  "创建房间失败：{0}": "Raum konnte nicht erstellt werden: {0}",
  加入房间: "Raum beitreten",
  "加入房间失败：{0}": "Beitritt zum Raum fehlgeschlagen: {0}",
  取消: "Abbrechen",
  可用: "Verfügbar",
  后台服务: "Hintergrunddienst",
  后台服务已关闭: "Hintergrunddienst beendet",
  启动器服务器: "Launcher-Server",
  启动失败: "Start fehlgeschlagen",
  "启动失败：{0}": "Start fehlgeschlagen: {0}",
  "和朋友一起玩：开个房间，或者加入别人的房间。":
    "Gemeinsam spielen: Erstelle einen Raum oder tritt dem Raum von jemandem bei.",
  复制: "Kopieren",
  "复制失败，请手动选中文本复制":
    "Kopieren fehlgeschlagen — bitte markiere den Text und kopiere ihn manuell",
  "将断开隧道并让中继释放端口，房间里的玩家会立刻掉线。":
    "Dadurch wird der Tunnel getrennt und der Relay-Port freigegeben; alle Spieler im Raum fliegen sofort raus.",
  "将结束本机的陶瓦联机进程。如果它是你自己打开的窗口，那个窗口也会一起关闭。":
    "Dadurch wird der lokale Terracotta-Prozess beendet. Hast du das Fenster selbst geöffnet, schließt es sich ebenfalls.",
  "将转发到 127.0.0.1:{0}（端口取自服务器配置）":
    "Weiterleitung an 127.0.0.1:{0} (Port aus den Server-Einstellungen)",
  尚未找到可执行文件: "Noch keine Programmdatei gefunden",
  已停止: "Beendet",
  已加入的房间: "Beigetretener Raum",
  已启动游戏并连接服务器: "Spiel gestartet und mit dem Server verbunden",
  "已生成新的 API Key，保存后生效":
    "Neuer API-Key generiert — zum Übernehmen speichern",
  "已找到可执行文件，路径见右侧设置。":
    "Programmdatei gefunden; der Pfad steht in den Einstellungen rechts.",
  "已经接入房主的局域网，点下面的「一键进服」就能直接进游戏。":
    "Du bist jetzt im Netzwerk des Hosts — klicke unten auf „Mit einem Klick beitreten“, um direkt ins Spiel zu kommen.",
  已连接: "Verbunden",
  已记录房间地址: "Adresse gespeichert",
  已进入房间: "Im Raum",
  "已选择 {0}，保存后生效": "{0} ausgewählt — zum Übernehmen speichern",
  开始于: "Gestartet um",
  当前连接数: "Aktive Verbindungen",
  成员: "Mitglied",
  我创建的房间: "Mein Raum",
  房主: "Host",
  "房主发来的是一段公网地址，粘进来即可保存并一键进服；房客不需要装模组。":
    "Der Host schickt eine öffentliche Adresse; füge sie ein, um sie zu speichern und mit einem Klick beizutreten. Gäste müssen keine Mods installieren.",
  房主需装模组: "Host benötigt das Mod",
  房间地址: "Raumadresse",
  房间已就绪: "Raum bereit",
  房间成员: "Raummitglieder",
  房间码: "Raumcode",
  打开项目主页: "Projektseite öffnen",
  "把地址发给朋友，Ta 在游戏里「直接连接」就能进来；转发目标在下面。":
    "Schicke die Adresse an deine Freunde — sie kommen im Spiel über „Direkt verbinden“ herein; das Weiterleitungsziel steht unten.",
  "把房主发来的房间码粘进来即可加入；连上后在游戏里直连 127.0.0.1 就能进服。":
    "Füge den Raumcode ein, den der Host dir geschickt hat, um beizutreten; nach dem Verbinden erreichst du den Server im Spiel über 127.0.0.1.",
  "把房间码发给朋友，Ta 在联机页粘贴就能加入。":
    "Schicke den Raumcode an deine Freunde — sie können ihn auf der Online-Seite einfügen und beitreten.",
  "按提示处理后可以重新建房或加入。":
    "Folge dem Hinweis, erstelle dann erneut einen Raum oder tritt einem bei.",
  断开隧道: "Tunnel trennen",
  未保存: "Nicht gespeichert",
  未运行: "Läuft nicht",
  本机地址: "Lokale Adresse",
  "本机还没有陶瓦联机：请先下载并解压，再在右侧设置里选中它的可执行文件。":
    "Terracotta ist auf diesem Rechner noch nicht vorhanden: Lade es zuerst herunter und entpacke es, wähle dann seine Programmdatei in den Einstellungen rechts aus.",
  本机隧道: "Lokaler Tunnel",
  "检查中继地址与本地服务端地址后可以重试。":
    "Prüfe die Relay- und die lokale Serveradresse und versuche es dann erneut.",
  "正在与房主建立虚拟局域网，第一次连接通常需要十几秒。":
    "Das virtuelle LAN zum Host wird aufgebaut; die erste Verbindung dauert meist etwa zehn Sekunden.",
  "正在与房主建立虚拟局域网，首次连接通常需要十几秒。":
    "Das virtuelle LAN zum Host wird aufgebaut; die erste Verbindung dauert meist etwa zehn Sekunden.",
  "正在准备陶瓦联机…": "Terracotta wird vorbereitet…",
  "正在创建房间…": "Raum wird erstellt…",
  "正在向中继服务器登记 API Key。": "API-Key wird beim Relay registriert…",
  "正在向中继申请端口，通常一两秒。":
    "Beim Relay wird ein Port angefragt; meist eine bis zwei Sekunden.",
  "正在启动本机服务器…": "Lokaler Server wird gestartet…",
  "要转发的服务器还没运行，先把它启动起来。":
    "Der weiterzuleitende Server läuft noch nicht — er wird jetzt gestartet.",
  "正在寻找局域网世界…": "LAN-Welt wird gesucht…",
  "正在建立虚拟局域网…": "Virtuelles LAN wird aufgebaut…",
  "正在注册联机密钥…": "Online-Schlüssel wird registriert…",
  "正在申请公网隧道…": "Öffentlicher Tunnel wird angefragt…",
  "正在申请联机房间，请稍候。": "Raum wird angefragt, bitte warten.",
  "正在连接房主，请稍候。": "Verbindung zum Host wird aufgebaut, bitte warten.",
  "正在连接房间…": "Verbindung zum Raum wird aufgebaut…",
  "点「创建房间」开一局，或粘贴朋友的房间码加入。":
    "Klicke auf „Raum erstellen“, um loszulegen, oder füge den Raumcode eines Freundes ein, um beizutreten.",
  "点「创建房间」把本机服务器发布到公网，或粘贴房主给的地址加入。":
    "Klicke auf „Raum erstellen“, um deinen lokalen Server öffentlich zu machen, oder füge die Adresse ein, die der Host dir gegeben hat.",
  "点「退出房间」后可以重新建房或加入。":
    "Verlasse den Raum, erstelle dann erneut einen oder tritt einem anderen bei.",
  "生成失败：{0}": "Generieren fehlgeschlagen: {0}",
  留空使用当前账号名: "Leer lassen, um den aktuellen Kontonamen zu verwenden",
  空闲: "Inaktiv",
  红石联机: "Redstone Online",
  "红石联机的房客不需要装任何东西，直接连就行。":
    "Gäste von Redstone Online müssen nichts installieren — einfach direkt verbinden.",
  "红石联机的房客不用装任何东西：点下面的「一键进服」，或在游戏里手动连接上面的地址。":
    "Redstone-Online-Gäste müssen nichts installieren: Klicke unten auf „Mit einem Klick beitreten“ oder verbinde dich im Spiel manuell mit der Adresse oben.",
  多人: "Mehrspieler",
  联机: "Online",
  "联机会话由启动器后台维持：切到别的页面也不会断，退出启动器时会自动收尾。":
    "Die Online-Sitzung läuft im Hintergrund des Launchers weiter: Beim Wechsel auf eine andere Seite bricht sie nicht ab und beim Beenden des Launchers wird sie automatisch sauber beendet.",
  联机地址: "Beitrittsadresse",
  联机设置: "Online-Einstellungen",
  联机设置已保存: "Online-Einstellungen gespeichert",
  "第一次使用会先拉起本机服务，可能要几秒钟。":
    "Beim ersten Mal wird zuerst der lokale Dienst gestartet; das kann ein paar Sekunden dauern.",
  "转发启动器托管的服务器时不需要模组；如果要联机的是「对局域网开放」的存档，请先给这个实例装 RedstoneOnline 模组并用 /rs open 发布（模组只负责发布，隧道由启动器接管）。":
    "Beim Weiterleiten eines Launcher-gehosteten Servers ist kein Mod nötig; willst du eine über „Für LAN öffnen“ freigegebene Welt gemeinsam spielen, installiere zuerst das RedstoneOnline-Mod in dieser Instanz und veröffentliche sie mit /rs open (das Mod veröffentlicht nur, den Tunnel übernimmt der Launcher).",
  "还没找到世界？先进入存档 → Esc → 对局域网开放，陶瓦会自动发现它。":
    "Noch keine Welt gefunden? Betrete eine Welt → Esc → Für LAN öffnen; Terracotta findet sie dann automatisch.",
  "还没有启动器托管的服务器。可以先去「服务器」页创建一台，或者切到「本机地址」直接转发已经开放局域网的存档（例如 127.0.0.1:25565）。":
    "Es gibt noch keinen Launcher-gehosteten Server. Erstelle zuerst einen auf der Server-Seite oder wechsle zu „Lokale Adresse“, um eine bereits für das LAN geöffnete Welt direkt weiterzuleiten (z. B. 127.0.0.1:25565).",
  退出房间: "Raum verlassen",
  "退出房间失败：{0}": "Raum konnte nicht verlassen werden: {0}",
  选择一台本机服务器: "Lokalen Server auswählen",
  选择可执行文件: "Programmdatei auswählen",
  选择陶瓦联机可执行文件: "Terracotta-Programmdatei auswählen",
  重新检测: "Erneut erkennen",
  重新生成: "Neu generieren",
  陶瓦联机: "Terracotta",
  "陶瓦联机是第三方开源项目（AGPL-3.0），需要单独下载；启动器只通过它的本地 HTTP 接口驱动它。":
    "Terracotta ist ein Open-Source-Projekt eines Drittanbieters (AGPL-3.0) und muss separat heruntergeladen werden; der Launcher steuert es ausschließlich über dessen lokale HTTP-Schnittstelle an.",
  隧道已就绪: "Tunnel bereit",
  需要先准备: "Einrichtung erforderlich",
  项目主页: "Projektseite",
  "默认使用官方中继节点，可以在右侧设置里改成你自己的节点。":
    "Standardmäßig wird der offizielle Relay-Knoten verwendet; in den Einstellungen rechts kannst du ihn auf deine eigenen Knoten umstellen.",
  转发到哪台服务器: "Weiterzuleitender Server",
  运行中: "Läuft",
  请先填写房主给你的公网地址:
    "Gib zuerst die öffentliche Adresse ein, die der Host dir gegeben hat",
  请先填写房主给你的房间码:
    "Gib zuerst den Raumcode ein, den der Host dir gegeben hat",
  "请先填写要转发的本机地址，例如 127.0.0.1:25565":
    "Gib zuerst die weiterzuleitende lokale Adresse ein, z. B. 127.0.0.1:25565",
  请先选择要转发的本机服务器:
    "Wähle zuerst den weiterzuleitenden lokalen Server aus",
  "先进入存档 → Esc → 对局域网开放，再回到这里点「创建房间」；陶瓦会自动发现你的世界并生成房间码。":
    "Öffne eine Welt → Esc → Für LAN öffnen, komme dann hierher zurück und klicke auf „Raum erstellen“; Terracotta findet deine Welt automatisch und erzeugt einen Raumcode.",
  "公网中继（frp）：给本机服务器或局域网世界分配一个公网地址，房客直接连，不用装任何东西。":
    "Öffentliches Relay (frp): vergibt deinem lokalen Server oder deiner LAN-Welt eine öffentliche Adresse, mit der Gäste direkt verbinden können — ganz ohne Installation.",
  "基于 EasyTier 的虚拟局域网：贴一个房间码就能连，不需要公网 IP，也不用改路由器。":
    "Ein virtuelles LAN auf EasyTier-Basis: Raumcode einfügen und schon steht die Verbindung — ohne öffentliche IP und ohne Änderungen am Router.",
  // Mehrere Anbieter gleichzeitig aktiv + Redstone-Online-Relay-Knoten (Speedtest & automatische Auswahl)
  不可用: "Nicht verfügbar",
  内置: "Integriert",
  从节点列表选择: "Aus der Knotenliste wählen",
  保存节点列表: "Knotenliste speichern",
  "另一家联机（{0}）的会话也在进行中，两家可以同时开着。":
    "Eine {0}-Sitzung läuft ebenfalls — beide Anbieter können gleichzeitig aktiv bleiben.",
  切过去看看: "Dorthin wechseln",
  "共 {0} 个节点可用，最快 {1} ms": "{0} Knoten erreichbar, schnellster {1} ms",
  "所有节点都连不上，请检查网络或填写自己的节点":
    "Kein Knoten erreichbar — prüfe dein Netzwerk oder trage eigene Knoten ein",
  测速: "Speedtest",
  "测速失败：{0}": "Speedtest fehlgeschlagen: {0}",
  自定义节点列表: "Eigene Knotenliste",
  "自己的节点，每行一条：名称=地址。":
    "Eigene Knoten, einer pro Zeile: Name=Adresse.",
  节点列表已保存: "Knotenliste gespeichert",
  "已选用最快节点 {0}（{1} ms）":
    "Der schnellste Knoten {0} wird verwendet ({1} ms)",
  选择中继节点: "Relay-Knoten auswählen",
  自动选最快节点: "Schnellsten Knoten automatisch wählen",
  "自动选节点失败：{0}": "Automatische Knotenwahl fehlgeschlagen: {0}",
  在线玩家: "Spieler online",
  联机密钥: "Online-Schlüssel",
  "节点列表保存失败：{0}": "Knotenliste konnte nicht gespeichert werden: {0}",
  "留空则自动生成；换新密钥后保存，下次建房生效。":
    "Bleibt das Feld leer, wird automatisch einer erzeugt; nach dem Erneuern des Schlüssels speichern — er gilt ab dem nächsten erstellten Raum.",
};

export default dict;
