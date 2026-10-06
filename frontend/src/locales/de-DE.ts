/*
 * Deutsch (de-DE) dictionary. Keys are the Simplified Chinese source strings.
 * The translations are split into chunks under ./parts and merged here.
 * Missing entries fall back to English, then to the Chinese source.
 */
import p0 from "./parts/de-0";
import p1 from "./parts/de-1";
import p2 from "./parts/de-2";
import p3 from "./parts/de-3";
import p4 from "./parts/de-4";
import p5 from "./parts/de-5";
import p6 from "./parts/de-6";
import p7 from "./parts/de-7";
import p8 from "./parts/de-8";
import p9 from "./parts/de-9";
import p10 from "./parts/de-10";

const deDE: Record<string, string> = {
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

export default deDE;
