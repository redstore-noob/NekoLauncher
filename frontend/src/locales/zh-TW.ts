/*
 * 繁體中文（zh-TW）詞典。key 為簡體中文原文。
 * 譯文拆分為 ./parts 下的生成分片，在此合併；缺失條目自動回退原文。
 */
import p0 from "./parts/zh-TW-0";
import p1 from "./parts/zh-TW-1";
import p2 from "./parts/zh-TW-2";
import p3 from "./parts/zh-TW-3";
import p4 from "./parts/zh-TW-4";
import p5 from "./parts/zh-TW-5";
import p6 from "./parts/zh-TW-6";
import p7 from "./parts/zh-TW-7";
import p8 from "./parts/zh-TW-8";
import p9 from "./parts/zh-TW-9";
import p10 from "./parts/zh-TW-10";

const zhTW: Record<string, string> = {
  ...p0,
  ...p1,
  ...p2,
  ...p3,
  ...p4,
  ...p5,
  ...p6,
  ...p7,
  ...p8,
  ...p9,
  ...p10,
};

export default zhTW;
