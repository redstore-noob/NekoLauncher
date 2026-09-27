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

/** 气泡里的随机台词：点一下换一句 */
const MASCOT_LINES = [
  t("在实例管理界面存档管理中的Rewind有备份功能喵"),
  t("点我干嘛呀，痒痒的喵"),
  t("启动之前记得存个档哦"),
  t("又在偷偷看我，喵~"),
  t("要不要先喝口水再开游戏？"),
  t("今天的运气看起来不错呢"),
  t("呜…再戳就要生气了喵"),
  t("快去玩吧，我在这里等你"),
  t("模组装太多了会卡哦"),
  t("摸摸头也不是不可以啦"),
  t("今天想玩哪个版本呀？"),
  t("喵呜~ 被你发现啦"),
  t("游戏加载中…要不要撸猫？"),
  t("别熬夜啦，对身体不好喵"),
  t("你点的每一版我都记得哦"),
  t("要不要试试新的整合包？"),
  t("我可不是装饰品喵！"),
  t("嘿嘿，被我萌到了吧"),
  t("困了…让我打个盹喵"),
];

const MascotCard: React.FC = () => {
  const [line, setLine] = useState("");
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
        className="block h-auto min-w-0 w-full cursor-pointer rounded-xl bg-transparent p-0 data-[hover=true]:bg-transparent"
        variant="light"
        onClick={react}
      >
        <div
          className={`relative aspect-square w-full ${bouncing ? "nya-mascot-bounce" : ""}`}
          style={{ containerType: "inline-size" }}
        >
          {imageFailed ? (
            <div className="flex h-full w-full flex-col items-center justify-center gap-2 rounded-2xl border border-dashed border-gray-300/80 text-gray-400 dark:border-gray-700">
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

          {/* 台词：覆盖在立绘里气泡的位置上（key 变化即重播弹出动画） */}
          {line ? (
            <span
              key={line}
              className="nya-bubble-pop pointer-events-none absolute flex items-center justify-center text-center font-bold"
              style={{
                left: "4%",
                top: "3%",
                width: "53%",
                height: "29%",
                padding: "0 6%",
                fontSize: "clamp(11px, 4.6cqw, 18px)",
                lineHeight: 1.35,
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
