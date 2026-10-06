/*
 * Copyright 2024 Next UI
 * Copyright 2026 烟花
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */
/*
 * 看板娘卡片：点击猫娘会弹跳一下，头顶气泡里冒出随机台词。
 * 立绘（含气泡）是一整张图，台词按百分比定位覆盖在气泡内，
 * 所以换图时只要气泡位置比例大致相同即可。
 */
import React, { useCallback, useRef, useState } from "react";
import { AnimalCat20Regular } from "@fluentui/react-icons";
import { Button } from "@heroui/react";

import { t } from "../../i18n";

/** 立绘路径：图片放在 frontend/public 下，构建后按原路径访问 */
const MASCOT_IMAGE = "/mascot/neko.png";

/** 气泡里的闲聊台词（混合池：俏皮向 + 功能向，文案尽量短以免超出气泡） */
const MASCOT_CHATTER = [
  t("欢迎回来，今天想玩点什么喵？"),
  t("启动之前记得存个档哦"),
  t("要不要先喝口水再开游戏？"),
  t("今天的运气看起来不错呢"),
  t("快去玩吧，我在这里等你"),
  t("今天想玩哪个版本呀？"),
  t("祝你好运，开局就能找到钻石喵"),
  t("游戏加载中…记得放松一下眼睛喵"),
  t("别熬夜啦，对身体不好喵"),
  t("你点的每一版我都记得哦"),
  t("帮助页面有很多教程，遇到问题去看看喵"),
  t("设置里可以换主题色，换个心情也不错喵"),
  t("玩一小时就起来活动一下，劳逸结合喵"),
  t("新快照刚发布哦，敢不敢第一个尝鲜喵"),
  t("困了…让我打个盹喵"),
  t("点我干嘛呀，痒痒的喵"),
  t("又在偷偷看我，喵~"),
  t("呜…再戳就要生气了喵"),
  t("摸摸头也不是不可以啦"),
  t("喵呜~ 被你发现啦"),
  t("游戏加载中…要不要撸猫？"),
  t("嘿嘿，被我萌到了吧"),
];

/** 启动器功能 Tips：混在台词里冒出来，帮玩家发现冷门好功能 */
const MASCOT_TIPS = [
  t("实例的存档管理里有 Rewind 备份功能，手滑删档也不怕喵"),
  t("把 .jar 或 .zip 直接拖进窗口就能装进实例哦"),
  t("下载大厅里能看到模组的中文名呢，搜中文也找得到"),
  t("模组列表右键可以打开版本管理，升降级都在那里喵"),
  t("整合包页面可以把整个实例打包成 .mrpack 分享给朋友"),
  t("导入其它启动器的实例？实例管理里有现成入口喵"),
  t("联机功能可以建房间和朋友一起玩，不用开服务器"),
  t("给实例换个自定义图标吧，在实例设置里就能换喵"),
  t("存档也能拍快照哦，开大工程前存一个很安心"),
  t("外置登录支持皮肤站账号，设置里填地址就行喵"),
  t("模组太多加载慢？在内容页把不用的先禁用掉喵"),
  t("设置里可以调内存上限，分配太多反而会卡哦"),
  t("下载大厅还有光影和资源包，材质一下子就变好看了"),
  t("实例可以一键复制，试新模组前先复制一份喵"),
];

/** 全部台词池：闲聊与 Tips 混合，点击时随机抽取 */
const MASCOT_LINES = [...MASCOT_CHATTER, ...MASCOT_TIPS];

const MascotCard: React.FC = () => {
  // 打开主页就先冒一句话（闲聊/Tips 随机），别让气泡一直空着
  const [line, setLine] = useState(
    () => MASCOT_LINES[Math.floor(Math.random() * MASCOT_LINES.length)],
  );
  const [bouncing, setBouncing] = useState(false);
  const [imageFailed, setImageFailed] = useState(false);
  const lastIndexRef = useRef(-1);

  const react = useCallback(() => {
    // 重触发弹跳动画：先摘掉类名，下一帧再加回来
    setBouncing(false);
    requestAnimationFrame(() => setBouncing(true));

    // 尽量不连着说同一句话
    let index = Math.floor(Math.random() * MASCOT_LINES.length);

    if (index === lastIndexRef.current) {
      index = (index + 1) % MASCOT_LINES.length;
    }
    lastIndexRef.current = index;
    setLine(MASCOT_LINES[index]);
  }, []);

  return (
    <section
      className="
        nya-enter nya-neon-card pointer-events-auto flex w-full flex-none
        flex-col gap-2 p-3
      "
    >
      <Button
        aria-label={t("和看板娘打个招呼")}
        className="block h-auto min-w-0 w-full cursor-pointer rounded-lg bg-transparent p-0 data-[hover=true]:bg-transparent"
        variant="light"
        onClick={react}
      >
        <div
          className={`relative aspect-square w-full ${bouncing ? "nya-mascot-bounce" : ""}`}
          style={{ containerType: "inline-size" }}
        >
          {imageFailed ? (
            <div className="flex h-full w-full flex-col items-center justify-center gap-2 rounded-lg border border-dashed border-gray-300/80 text-gray-400 dark:border-gray-700">
              <AnimalCat20Regular className="h-8 w-8" />
            </div>
          ) : (
            <img
              alt={t("看板娘")}
              className="h-full w-full object-contain"
              draggable={false}
              src={MASCOT_IMAGE}
              onError={() => setImageFailed(true)}
            />
          )}

          {/* 台词：覆盖在立绘里气泡的位置上（key 变化即重播弹出动画）。
              外层 HeroUI Button 自带 whitespace-nowrap，这里必须显式恢复换行 */}
          {line ? (
            <span
              key={line}
              className="nya-bubble-pop pointer-events-none absolute flex items-center justify-center overflow-hidden text-center font-bold"
              style={{
                left: "4%",
                top: "3%",
                width: "53%",
                minHeight: "29%",
                maxHeight: "38%",
                padding: "0 6%",
                fontSize: "clamp(11px, 4.6cqw, 18px)",
                lineHeight: 1.25,
                whiteSpace: "normal",
                wordBreak: "break-word",
                overflowWrap: "anywhere",
                color: "#17331b",
              }}
            >
              {line}
            </span>
          ) : null}
        </div>
      </Button>
    </section>
  );
};

export default MascotCard;
