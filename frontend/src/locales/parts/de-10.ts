/*
 * Fragment „Herkunft der Startargumente“ (launch argument provenance):
 * Paneltitel, Quellkategorien und Beschreibungen der stabilen Keys,
 * Konfliktformulierungen sowie Schalter für den Anzeigeumfang.
 *
 * Die Zuordnung Key → Text liegt in src/lib/launchProvenance.ts; hier stehen nur die Übersetzungen.
 * Allgemeine Begriffe aus anderen Fragmenten (Hauptklasse / Aktualisieren / Ausklappen …) werden hier nicht wiederholt.
 */
const dict: Record<string, string> = {
  // ---- Panel-Rahmen ----
  启动参数溯源: "Herkunft der Startargumente",
  本次启动: "Dieser Start",
  "本次启动 · 共 {0} 条参数": "Dieser Start · {0} Argumente insgesamt",
  本次启动环境: "Bei diesem Start verwendet",
  "实例 / 版本": "Instanz / Version",
  "Java 可执行文件": "Java-Programmdatei",
  工作目录: "Arbeitsverzeichnis",
  "正在读取启动参数…": "Startargumente werden gelesen…",
  "还没有成功启动过游戏，暂时没有可以回溯的参数":
    "Das Spiel wurde noch nicht erfolgreich gestartet — es gibt also noch nichts zurückzuverfolgen",
  "启动参数溯源会在游戏成功启动后记录。先启动一次游戏，再回到这里查看每个参数是谁加的。":
    "Die Herkunft der Startargumente wird nach einem erfolgreichen Spielstart aufgezeichnet. Starte das Spiel einmal und sieh dann hier nach, wer welches Argument hinzugefügt hat.",
  这次启动没有记录到任何参数:
    "Bei diesem Start wurden keine Argumente aufgezeichnet",

  // ---- Zusammenfassungszeilen ----
  "有 {0} 组参数被后面的同名参数覆盖":
    "{0} Argumentgruppe(n) wurden durch ein späteres, gleichnamiges Argument überschrieben",
  "共 {0} 条参数没有生效（列表中已划线标出）。JVM 取最后一次出现的值，被覆盖的那条不会起作用。":
    "{0} Argument(e) sind nicht wirksam geworden (in der Liste durchgestrichen). Die JVM verwendet das jeweils letzte Vorkommen, das überschriebene bleibt also wirkungslos.",
  "没有参数互相覆盖，{0} 条参数全部生效。":
    "Keine Argumente überschreiben sich gegenseitig — alle {0} Argumente sind wirksam.",

  // ---- Anzeigeumfang / Werkzeugzeile ----
  显示全部参数: "Alle Argumente anzeigen",
  "显示全部参数（含被覆盖的）": "Alle Argumente anzeigen (auch überschriebene)",
  只显示生效的参数: "Nur wirksame Argumente anzeigen",
  "点击{0}完整参数": "Klicke, um das vollständige Argument {0}",
  点击展开完整参数: "Klicke, um das vollständige Argument auszuklappen",
  点击收起: "Klicke, um es einzuklappen",
  "「{0}」出现 {1} 次：来自「{2}」的那条生效，来自「{3}」的被忽略。":
    "„{0}“ erscheint {1}-mal: Das Argument aus „{2}“ ist wirksam, das aus „{3}“ wird ignoriert.",
  "{0} / {1} 条": "{0} / {1}",
  "{0} 条": "{0}",

  // ---- Eintrags-Badges ----
  已被覆盖: "Überschrieben",
  "压过了「{0}」": "überschreibt „{0}“",

  // ---- Konfliktbereich ----
  互相覆盖的参数: "Argumente, die sich gegenseitig überschreiben",

  // ---- Gruppentitel ----
  "JVM 参数": "JVM-Argumente",
  游戏参数: "Spiel-Argumente",
  其他参数: "Sonstige Argumente",
  "交给 Java 虚拟机，在游戏启动前生效":
    "Wird an die Java Virtual Machine übergeben und greift vor dem Spielstart",
  决定实际启动哪个入口类:
    "Bestimmt, welche Einstiegsklasse tatsächlich gestartet wird",
  "原样传给 Minecraft 本体": "Wird unverändert an Minecraft selbst übergeben",

  // ---- Quellkategorien (Source.Kind) ----
  版本文件自带: "Von der Versionsdatei mitgeliefert",
  启动器自动添加: "Vom Launcher automatisch hinzugefügt",
  全局启动设置: "Globale Starteinstellungen",
  实例独立设置: "Instanzspezifische Einstellungen",
  实例或全局设置: "Instanz- oder globale Einstellungen",
  直接进服: "Direkte Verbindung",
  外置登录注入: "Injektion der externen Anmeldung",
  插件添加: "Von einem Plugin hinzugefügt",
  启动变换: "Start-Transformation",
  来源未知: "Unbekannte Quelle",

  // ---- Quellbeschreibungen (Source.Key-Vorlagen, {0} ist das Detail) ----
  "启动器自动设置的最小堆（{0} MiB）":
    "Minimaler Heap, automatisch vom Launcher gesetzt ({0} MiB)",
  "实例独立设置的最大堆（{0} MiB）":
    "Maximaler Heap aus der instanzspezifischen Einstellung ({0} MiB)",
  "全局设置的最大堆（{0} MiB）":
    "Maximaler Heap aus der globalen Einstellung ({0} MiB)",
  "启动器按系统内存自动计算的最大堆（{0} MiB）":
    "Maximaler Heap, vom Launcher aus dem Systemarbeitsspeicher berechnet ({0} MiB)",
  "启动器内置的 G1 垃圾回收调优参数":
    "Im Launcher eingebaute G1-Garbage-Collector-Tuning-Parameter",
  "实例的额外 JVM 参数": "Zusätzliche JVM-Argumente der Instanz",
  "全局的额外 JVM 参数":
    "Zusätzliche JVM-Argumente aus den globalen Einstellungen",
  "Forge / NeoForge 需要的库目录声明":
    "Von Forge / NeoForge benötigte Deklaration des Bibliotheksverzeichnisses",
  启动器拼出的类路径: "Vom Launcher zusammengesetzter Klassenpfad",
  版本文件声明的日志配置:
    "Von der Versionsdatei deklarierte Protokollkonfiguration",
  "版本文件自带的 JVM 参数":
    "Mit der Versionsdatei mitgelieferte JVM-Argumente",
  版本文件自带的游戏参数: "Mit der Versionsdatei mitgelieferte Spiel-Argumente",
  版本文件声明的主类: "Von der Versionsdatei deklarierte Hauptklasse",
  旧版版本文件的库路径与类路径:
    "Native-Pfad und Klassenpfad aus einer alten Versionsdatei",
  实例的额外游戏参数: "Zusätzliche Spiel-Argumente der Instanz",
  全局的额外游戏参数:
    "Zusätzliche Spiel-Argumente aus den globalen Einstellungen",
  "插件前置的 JVM 参数": "Von einem Plugin vorangestellte JVM-Argumente",
  "插件追加的 JVM 参数": "Von einem Plugin angehängte JVM-Argumente",
  插件前置的游戏参数: "Von einem Plugin vorangestellte Spiel-Argumente",
  插件追加的游戏参数: "Von einem Plugin angehängte Spiel-Argumente",

  // ---- Provenienz-Einstieg auf der Instanz-Detailseite ----
  "启动参数是怎么来的？": "Woher kommen diese Startargumente?",
  "逐条列出上次启动时每个参数由谁添加，以及哪些被后面的同名参数覆盖了。":
    "Listet für den letzten Start auf, wer jedes Argument hinzugefügt hat und welche durch ein späteres, gleichnamiges Argument überschrieben wurden.",

  // ---- Lokale KI-Inferenz-Engines (llama.cpp / Ollama / LM Studio) ----
  "llama.cpp（本地）": "llama.cpp (lokal)",
  "Ollama（本地）": "Ollama (lokal)",
  "LM Studio（本地）": "LM Studio (lokal)",
  "留空使用默认地址；修改过端口或地址时填写":
    "Leer lassen, um die Standardadresse zu verwenden; nur eintragen, wenn du Port oder Adresse geändert hast",
  本地引擎通常无需填写: "Bei lokalen Engines in der Regel nicht nötig",
  "本地引擎默认不校验 Key；若 llama-server 启动时设置了 --api-key，在这里填上即可":
    "Lokale Engines prüfen standardmäßig keinen API-Schlüssel; falls llama-server mit --api-key gestartet wurde, trage ihn einfach hier ein",
  "从下方检测到的模型中选择，或手动输入":
    "Wähle eines der unten erkannten Modelle aus oder gib es manuell ein",
  本地引擎检测: "Erkennung lokaler Engines",
  "检测中…": "Wird erkannt…",
  重新检测: "Erneut erkennen",
  "正在连接 {url} …": "Verbinde mit {url} …",
  "已连接，发现 {count} 个模型，点击选用：":
    "Verbunden, {count} Modelle gefunden. Klicke eines an, um es zu verwenden:",
  "已连接到引擎，但没有发现模型，请先在引擎侧加载 / 拉取模型":
    "Mit der Engine verbunden, aber keine Modelle gefunden. Lade zuerst ein Modell in der Engine oder führe dort einen Pull aus",
  "未检测到本地引擎，请确认它已启动，然后点「重新检测」":
    "Keine lokale Engine erkannt. Stelle sicher, dass sie läuft, und klicke dann auf „Erneut erkennen“",
  "启动方式：终端运行 llama-server -m <模型文件.gguf> --host 127.0.0.1 --port 8080，保持窗口开启后点「重新检测」。":
    "So startest du: Führe llama-server -m <Modelldatei.gguf> --host 127.0.0.1 --port 8080 im Terminal aus, lass das Fenster geöffnet und klicke dann auf „Erneut erkennen“.",
  "启动方式：终端运行 ollama serve，模型用 ollama pull <名称> 拉取。若已在运行仍检测不到，需要设置环境变量 OLLAMA_ORIGINS=* 后重启 Ollama（跨域限制）。":
    "So startest du: Führe ollama serve im Terminal aus und ziehe Modelle mit ollama pull <Name>. Läuft es bereits, wird aber nicht erkannt, setze die Umgebungsvariable OLLAMA_ORIGINS=* und starte Ollama neu (CORS-Einschränkung).",
  "启动方式：LM Studio → Developer → Start Server（默认端口 1234，保持 CORS 开启）。":
    "So startest du: LM Studio → Developer → Start Server (Standardport 1234, CORS aktiviert lassen).",
};

export default dict;
