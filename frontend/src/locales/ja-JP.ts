/*
 * 日本語（ja-JP）詞典。key は簡体字中国語の原文。
 * 訳文は ./parts 配下の生成チャンクに分割し、ここで結合する。
 */
import p0 from "./parts/ja-0";
import p1 from "./parts/ja-1";
import p2 from "./parts/ja-2";
import p3 from "./parts/ja-3";
import p4 from "./parts/ja-4";
import p5 from "./parts/ja-5";
import p6 from "./parts/ja-6";
import p7 from "./parts/ja-7";
import p8 from "./parts/ja-8";
import p9 from "./parts/ja-9";

const jaJP: Record<string, string> = {
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
};

export default jaJP;
