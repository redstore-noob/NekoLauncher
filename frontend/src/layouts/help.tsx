/*
 * 帮助页（X-2）：把"用户最常卡住的几件事"写进应用内，而不是让 ta 去翻仓库文档。
 *
 * 设计取舍：
 *   - 内容是**结构化数据**（分节 + 条目 + 可选跳转），不是 Markdown 渲染：
 *     条目少而固定，渲染成折叠面板比引一个 Markdown 渲染器更可控，也不会因为
 *     docs/ 目录没随安装包分发而变成空白页；
 *   - 排版用 HeroUI Accordion：默认全收起，一屏能扫完全部问题标题，
 *     搜到/展开才看正文——比全部展开的长页紧凑得多；
 *   - 每条都能"点一下就去能解决问题的地方"（navigateToPage），比只给文字更有用；
 *   - 文案全部走 t()，键是中文原文（与全项目一致），翻译在 locales/parts 里补。
 */
import React, { useMemo, useState } from "react";
import { Accordion, AccordionItem, Button, Chip, Input } from "@heroui/react";
import {
  ArrowDownload20Regular,
  DocumentText20Regular,
  PaintBrush20Regular,
  Person20Regular,
  Search20Regular,
  Settings20Regular,
  Apps20Regular,
  Bot20Regular as BotIcon,
} from "@fluentui/react-icons";

import { navigateToPage } from "../lib/navigation";
import { t } from "../i18n";

/** 一个帮助条目：标题 + 正文（可多段）+ 可选的"去哪儿解决" */
interface HelpEntry {
  id: string;
  section: string;
  title: string;
  body: string[];
  action?: { label: string; pageId: string; detail?: string };
  /** 设置后在该条目下渲染「AI 诊断」按钮：跳转 AI 页并预填提示词 */
  aiPrompt?: string;
}

/** 帮助内容。增删条目只改这里；section 的顺序即展示顺序。 */
export const HELP_SECTIONS = [
  "启动与登录",
  "实例与内容",
  "服务器与联机",
  "个性化与外观",
  "启动器自身",
];

/** 顶部快捷入口：最常被求助问题指向的页面 */
const QUICK_LINKS: Array<{
  label: string;
  pageId: string;
  detail?: string;
  icon: React.ReactNode;
}> = [
  { label: "下载游戏", pageId: "download", icon: <ArrowDownload20Regular /> },
  { label: "实例管理", pageId: "instances", icon: <Apps20Regular /> },
  { label: "账户", pageId: "account", icon: <Person20Regular /> },
  { label: "外观", pageId: "appearance", icon: <PaintBrush20Regular /> },
  { label: "设置", pageId: "settings", icon: <Settings20Regular /> },
];

export const HELP_ENTRIES: HelpEntry[] = [
  {
    id: "launch-failed",
    section: "启动与登录",
    title: "游戏启动失败，先看哪里？",
    body: [
      "先看「运行日志」：启动器会把启动命令、Java 路径与游戏的每一行输出都记在里面，90% 的启动失败在那里能直接看到原因（缺库、Java 版本不符、模组报错）。",
      "如果游戏进程以非零退出码结束，启动器会弹「游戏异常退出」并给出崩溃诊断；直接点「复制诊断信息」就能把关键内容发到群里求助。",
      "常见三类原因：Java 主版本与游戏不符、内存分配过小（大型整合包建议 4 GB 以上）、模组冲突或缺少前置。",
    ],
    action: { label: "打开运行日志", pageId: "settings" },
    aiPrompt:
      "我的游戏启动失败了，请帮我诊断最近的崩溃原因：先看启动日志和崩溃报告，告诉我具体哪里出了问题、怎么修。",
  },
  {
    id: "microsoft-login",
    section: "启动与登录",
    title: "正版登录怎么弄？一直转圈怎么办？",
    body: [
      "推荐用「启动器内登录」：账户页 → 添加账号 → 正版登录，启动器会在当前窗口打开微软登录页，授权后自动跳回来，全程不需要额外窗口。",
      "也可以用设备码登录：启动器显示一个代码与网址，在浏览器打开网页、输入代码、同意授权即可；如果浏览器没自动打开，手动复制地址访问。",
      "登录成功后令牌会加密保存在启动器数据目录里，下次启动不需要重新登录；换机器或换了 Windows 用户则需要重新登录（旧令牌解不开）。",
      "登录卡住多半是网络到微软服务不通：设置页 → 网络 → 代理里配置代理后重试（设备码轮询与授权跳转都会走这个代理）。",
    ],
    action: { label: "去账户页", pageId: "account" },
  },
  {
    id: "offline-account",
    section: "启动与登录",
    title: "没有正版账号能玩吗？",
    body: [
      "可以：账户页能添加离线账号，用任意名字进单机与局域网。",
      "离线账号不能进正版验证服务器；皮肤站账号（外置登录）可以进对应的皮肤站服务器。",
    ],
    action: { label: "去账户页", pageId: "account" },
  },
  {
    id: "isolation",
    section: "实例与内容",
    title: "「版本隔离」是什么？要不要开？",
    body: [
      "隔离 = 每个实例有自己独立的 mods / config / saves 目录，互不影响；不隔离则所有实例共用游戏根目录里那一份。",
      "建议开着：整合包与不同加载器的模组几乎必然冲突，隔离能避免「装了 A 版本，B 版本也变了」。代价是每个实例要单独装一遍模组与资源。",
      "实例详情里能单独改某个实例的隔离开关；全局默认值在设置页。想要一份现成实例，还可以在实例页「管理」里直接「创建副本」。",
    ],
    action: { label: "去实例页", pageId: "instances" },
  },
  {
    id: "modpack",
    section: "实例与内容",
    title: "整合包 / 资源包怎么导入？",
    body: [
      "整合包：下载页支持 .mrpack（Modrinth）与 CurseForge 的 .zip——拖进来或点「导入整合包」，启动器会按清单装好游戏版本、加载器与全部模组。",
      "资源包 / 光影：把 zip 放进实例的 resourcepacks / shaderpacks 目录即可；也可以在实例页的内容列表里导入。",
      "存档：实例页的「存档」页签可以导入/导出 .zip 存档，导入时自动避开重名。",
    ],
    action: { label: "去下载页", pageId: "download" },
  },
  {
    id: "mod-not-loaded",
    section: "实例与内容",
    title: "模组装了却没生效",
    body: [
      "先确认加载器：Fabric 的模组不能放进 Forge 实例，反之亦然；下载页选版本时注意加载器与游戏版本要一致。",
      "再确认目录：实例开了隔离时，模组要放进该实例自己的 mods 目录（实例页「内容」页签里能看到实际路径）。",
      "最后看日志里有没有模组加载失败的堆栈——缺少前置模组是最常见的原因。",
      "更新模组：实例页「内容」页签的「检查更新」能批量比对 Modrinth；点每个模组旁的版本管理按钮还能自由升级或降级到任意版本（原文件会先备份）。",
    ],
    action: { label: "去实例页", pageId: "instances" },
    aiPrompt:
      "我装的模组没有生效，请帮我检查当前实例的模组列表和启动日志，找出加载失败或缺少前置的模组。",
  },
  {
    id: "java",
    section: "实例与内容",
    title: "Java 怎么选？需要自己装吗？",
    body: [
      "不用自己装：启动器会按游戏版本要求自动下载对应 Java（与官方启动器一致），已经装好合适版本时直接复用。",
      "想用自己装的 Java，可以在设置页「Java 运行时」里点「自动检索」扫描本机，或手动添加 java 可执行文件；列表第一项是默认（主）Java。",
      "老版本（1.16 及以前）用 Java 8，1.17–1.20 常用 Java 17，1.20.5 以后与新版快照多用 Java 21——版本不对是最典型的「点了没反应」。",
    ],
    action: { label: "去 Java 设置", pageId: "settings", detail: "java" },
  },
  {
    id: "server",
    section: "服务器与联机",
    title: "怎么开一个服务器给朋友玩？",
    body: [
      "「服务器」页可以创建并托管服务端（启动器负责下载、启动、备份与退出时优雅停服），支持设置内存、端口、白名单与 OP。",
      "同一台机器/局域网内的朋友直接连你的内网地址；不在同一网络就要用联机页的公网方案（见下一条）。",
      "服务端目录、备份与日志都在「服务器」页里能打开；关启动器时会先保存世界再停服，不会留下占用世界的孤儿进程。",
    ],
    action: { label: "去服务器页", pageId: "multiplayer", detail: "servers" },
  },
  {
    id: "online",
    section: "服务器与联机",
    title: "联机用陶瓦还是红石？",
    body: [
      "陶瓦联机（Terracotta）：基于虚拟局域网，房主贴一个房间码，房客粘进来就能连，适合「我开个单人存档给朋友进」。",
      "红石联机（Redstone Online）：公网中继，房主拿到一个公网地址，房客在游戏里直接连接；转发启动器托管的服务器时房主不需要装任何模组。",
      "两家可以同时开着，互不影响；红石的中继节点可以在设置里测速并自动选最快的一个。",
    ],
    action: { label: "去联机页", pageId: "multiplayer", detail: "online" },
  },
  {
    id: "snapshot",
    section: "服务器与联机",
    title: "存档快照（回滚）会不会很占硬盘？",
    body: [
      "快照按内容去重：只有和上一份不同的文件才会新增占盘，所以「同一个世界存十次」通常只比一次多一点。",
      "启动器会自动清理：回滚前生成的安全快照只保留最近若干个，超出体积预算时从最旧的无标记快照开始淘汰。",
      "带备注或标记颜色的快照不会被自动清理——那是你明确要留住的。回滚前也会自动留一份安全快照，随时能回到回滚之前。",
    ],
    action: { label: "去实例页", pageId: "instances" },
  },
  {
    id: "appearance",
    section: "个性化与外观",
    title: "怎么换背景、调毛玻璃效果？",
    body: [
      "最快的方式：主页添加「外观调节」小组件——毛玻璃（系统亚克力）与面板毛玻璃的开关、窗口 / 壁纸不透明度、背景模糊都在里面拖动即调。",
      "完整设置在外观页：背景图源（纯色 / 桌面壁纸 / 必应每日 / 自选图片 / Wallpaper Engine）、背景压暗度、主题色、面板毛玻璃强度等。",
      "毛玻璃（亚克力）需要 Windows 11 22621+；旧系统切换后会提示重启启动器生效。面板毛玻璃是纯前端的模糊效果，任何平台都可用。",
    ],
    action: { label: "去外观页", pageId: "appearance" },
  },
  {
    id: "storage",
    section: "启动器自身",
    title: "启动器的数据存在哪？能做成绿色版吗？",
    body: [
      "默认在用户目录下的 NekoLauncher 文件夹（设置页「关于与维护」里能看到完整路径并直接打开）。",
      "便携模式：在启动器 exe 同级放一个 portable.flag 文件（或建一个 NekoLauncher-data 目录），数据就会跟着程序走，适合放 U 盘。",
      "账户令牌在 Windows 上用 DPAPI 加密保存，其它平台用本地密钥文件 + AES-GCM。",
    ],
    action: { label: "去设置页", pageId: "settings" },
  },
  {
    id: "update",
    section: "启动器自身",
    title: "怎么更新启动器？",
    body: [
      "设置页「关于与维护」里有「检查更新」：默认把预发布版也算进去（本项目以 preview 版为主），查到新版本后 Windows 上可以直接「下载并重启」自动替换。",
      "其它平台会给出发布页地址，手动下载替换即可（AppImage / .app 的替换方式各不相同，自动替换反而容易弄坏）。",
    ],
    action: { label: "去设置页", pageId: "settings" },
  },
  {
    id: "feedback",
    section: "启动器自身",
    title: "遇到问题怎么反馈？",
    body: [
      "最有用的三样东西：运行日志（设置页 → 运行日志 → 导出）、崩溃诊断（崩溃弹窗里的「复制诊断信息」）、以及复现步骤。",
      "如果是某个实例/模组的问题，请附上游戏版本、加载器版本与 Java 版本——这三项决定了九成兼容性问题。",
    ],
    action: { label: "打开运行日志", pageId: "settings" },
  },
];

const HelpPage: React.FC = () => {
  const [keyword, setKeyword] = useState("");
  // 手动展开的条目（非搜索态受控；搜索态强制展开全部命中）
  const [manualExpanded, setManualExpanded] = useState<Set<string>>(new Set());

  const filtered = useMemo(() => {
    const needle = keyword.trim().toLowerCase();

    if (!needle) return HELP_ENTRIES;

    return HELP_ENTRIES.filter((entry) => {
      const haystack = [
        t(entry.title),
        entry.section,
        ...entry.body.map((line) => t(line)),
      ]
        .join(" ")
        .toLowerCase();

      return haystack.includes(needle);
    });
  }, [keyword]);

  const grouped = useMemo(() => {
    return HELP_SECTIONS.map((section) => ({
      section,
      entries: filtered.filter((entry) => entry.section === section),
    })).filter((group) => group.entries.length > 0);
  }, [filtered]);

  const searching = keyword.trim().length > 0;

  return (
    <div className="flex h-full w-full flex-col overflow-hidden">
      <div className="flex flex-none flex-col gap-3 px-6 pb-2 pt-5">
        <div className="flex flex-wrap items-center gap-2">
          <div className="flex size-9 flex-none items-center justify-center rounded-xl bg-primary/15 text-primary">
            <DocumentText20Regular />
          </div>
          <div className="min-w-0">
            <div className="text-xl font-semibold text-gray-800 dark:text-gray-200">
              {t("帮助")}
            </div>
            <div className="text-[11px] text-gray-400">
              {t("常见问题的排查步骤；每条都给一个能直接跳过去解决的位置。")}
            </div>
          </div>
          <Input
            className="ml-auto w-full max-w-xs"
            placeholder={t("搜索问题，例如 Java、联机、闪退")}
            size="sm"
            startContent={<Search20Regular />}
            value={keyword}
            variant="bordered"
            onValueChange={setKeyword}
          />
        </div>

        {/* 快捷入口：求助问题最常指向的页面，一排直达 */}
        <div className="flex flex-wrap items-center gap-1.5">
          <span className="mr-1 text-[11px] text-gray-400">
            {t("快速前往：")}
          </span>
          {QUICK_LINKS.map((link) => (
            <Chip
              key={link.pageId}
              as="button"
              className="cursor-pointer"
              color="default"
              size="sm"
              variant="flat"
              onClick={() => navigateToPage(link.pageId, link.detail)}
            >
              <span className="flex items-center gap-1">
                {link.icon}
                {t(link.label)}
              </span>
            </Chip>
          ))}
        </div>
      </div>

      <div className="nya-scroll flex min-h-0 flex-1 flex-col gap-3 overflow-y-auto px-6 pb-6 pt-2">
        {grouped.length === 0 ? (
          <div className="rounded-2xl border nya-border px-4 py-6 text-center text-[12px] text-gray-400">
            {t("没有匹配的条目，换个关键词试试。")}
          </div>
        ) : null}

        {grouped.map((group) => (
          <div key={group.section} className="flex flex-col gap-1.5">
            <div className="flex items-center gap-2 px-1">
              <span className="text-[12px] font-semibold text-gray-500 dark:text-gray-400">
                {t(group.section)}
              </span>
              <span className="rounded-full bg-default-100 px-1.5 py-px text-[10px] text-gray-400">
                {group.entries.length}
              </span>
            </div>
            <Accordion
              className="px-0"
              selectedKeys={
                searching
                  ? new Set(group.entries.map((entry) => entry.id))
                  : manualExpanded
              }
              selectionMode="multiple"
              variant="splitted"
              onSelectionChange={(keys) =>
                setManualExpanded(new Set(Array.from(keys as Set<string>)))
              }
            >
              {group.entries.map((entry) => (
                <AccordionItem
                  key={entry.id}
                  aria-label={t(entry.title)}
                  title={
                    <span className="text-[13px] font-semibold text-gray-800 dark:text-gray-200">
                      {t(entry.title)}
                    </span>
                  }
                >
                  <div className="flex flex-col gap-2 pb-1">
                    {entry.body.map((line, index) => (
                      <p
                        key={`${entry.id}-${index}`}
                        className="text-[11px] leading-relaxed text-gray-500 dark:text-gray-400"
                      >
                        {t(line)}
                      </p>
                    ))}
                    {entry.aiPrompt ? (
                      <Button
                        className="self-start"
                        color="secondary"
                        size="sm"
                        startContent={<BotIcon />}
                        variant="flat"
                        onPress={() => navigateToPage("ai", entry.aiPrompt)}
                      >
                        {t("AI 诊断")}
                      </Button>
                    ) : null}
                    {entry.action ? (
                      <Button
                        className="self-start"
                        color="primary"
                        size="sm"
                        variant="flat"
                        onPress={() =>
                          navigateToPage(
                            entry.action!.pageId,
                            entry.action!.detail,
                          )
                        }
                      >
                        {t(entry.action.label)}
                      </Button>
                    ) : null}
                  </div>
                </AccordionItem>
              ))}
            </Accordion>
          </div>
        ))}
      </div>
    </div>
  );
};

export default HelpPage;
