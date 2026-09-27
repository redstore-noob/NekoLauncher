/*
 * English (en-US) dictionary. Keys are the Simplified Chinese source strings.
 * The translations are split into generated chunks under ./parts and merged here.
 * Missing entries fall back to the Chinese source automatically.
 */
import p0 from "./parts/en-0";
import p1 from "./parts/en-1";
import p2 from "./parts/en-2";
import p3 from "./parts/en-3";
import p4 from "./parts/en-4";
import p5 from "./parts/en-5";
import p6 from "./parts/en-6";
import p7 from "./parts/en-7";
import p8 from "./parts/en-8";
import p9 from "./parts/en-9";

const enUS: Record<string, string> = {
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

export default enUS;
