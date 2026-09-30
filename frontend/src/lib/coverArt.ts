/*
 * 本地音频内嵌封面提取（纯前端，零后端改动）。
 * 音频经 /localfile?path= 流式返回（http.ServeFile 支持 Range），因此只需
 * 拉取文件头部（个别容器拉尾部）几十~几百 KB 即可解析出封面图：
 *   - MP3/AAC: ID3v2.2 PIC / v2.3-2.4 APIC 帧
 *   - FLAC:    METADATA_BLOCK_PICTURE (type 6)
 *   - OGG:     Vorbis 注释 METADATA_BLOCK_PICTURE（Base64 内嵌 FLAC 图片块）
 *   - M4A/MP4: moov.meta.ilst.covr 原子（moov 在尾部时兜底拉尾部 1MB 扫描）
 * 均未命中时探测同目录 cover/folder/front 常见命名的外挂图片。
 * 结果用 ObjectURL 缓存（LRU 上限 16，淘汰时 revoke）。
 */
import { resolveTrackUrl } from "./audioBridge";

const HEAD_BYTES = 768 * 1024;
const TAIL_BYTES = 1024 * 1024;

const cache = new Map<string, string>(); // filePath → objectURL
const inflight = new Map<string, Promise<string | null>>();

function cachePut(filePath: string, url: string | null) {
  if (!url) return;
  cache.set(filePath, url);
  if (cache.size > 16) {
    const oldest = cache.keys().next().value as string | undefined;

    if (oldest && oldest !== filePath) {
      const old = cache.get(oldest);

      cache.delete(oldest);
      if (old) URL.revokeObjectURL(old);
    }
  }
}

/** 拉取指定区间字节；Range 不被支持时退回整文件前 HEAD_BYTES。 */
async function fetchBytes(
  filePath: string,
  start: number,
  end: number,
): Promise<Uint8Array> {
  const url = resolveTrackUrl(filePath);
  const res = await fetch(url, {
    headers: { Range: `bytes=${start}-${end - 1}` },
  });

  if (!res.ok && res.status !== 206) throw new Error(`HTTP ${res.status}`);

  return new Uint8Array(await res.arrayBuffer());
}

// --- 图片字节 → ObjectURL -------------------------------------------------

function sniffImageMime(bytes: Uint8Array, fallback: string): string {
  if (bytes[0] === 0xff && bytes[1] === 0xd8) return "image/jpeg";
  if (bytes[0] === 0x89 && bytes[1] === 0x50) return "image/png";
  if (bytes[0] === 0x47 && bytes[1] === 0x49 && bytes[2] === 0x46)
    return "image/gif";
  if (bytes[0] === 0x42 && bytes[1] === 0x4d) return "image/bmp";
  if (
    bytes[0] === 0x52 &&
    bytes[1] === 0x49 &&
    bytes[2] === 0x46 &&
    bytes[8] === 0x57 &&
    bytes[9] === 0x45
  )
    return "image/webp";

  return fallback;
}

function bytesToObjectUrl(bytes: Uint8Array, mime: string): string | null {
  if (bytes.length < 64) return null;
  const blob = new Blob([bytes.slice().buffer as ArrayBuffer], {
    type: sniffImageMime(bytes, mime),
  });

  return URL.createObjectURL(blob);
}

// --- ID3v2 (mp3 / aac) ----------------------------------------------------

function decodeText(bytes: Uint8Array, enc: number): string {
  try {
    if (enc === 1 || enc === 2) {
      const label = enc === 2 ? "utf-16be" : "utf-16";

      return new TextDecoder(label).decode(bytes).replace(/\0+$/, "");
    }
    if (enc === 3)
      return new TextDecoder("utf-8").decode(bytes).replace(/\0+$/, "");
    // Latin-1 逐字节映射，TextDecoder('latin1') 在部分实现是 windows-1252，可接受
    let out = "";

    for (const b of bytes) if (b !== 0) out += String.fromCharCode(b);

    return out;
  } catch {
    return "";
  }
}

/** 解析 ID3v2 头部得到 APIC/PIC 图片；数据不足（needMore）时返回 null+needMore。 */
function parseId3(buf: Uint8Array): { url: string | null; needMore: boolean } {
  if (buf.length < 10 || buf[0] !== 0x49 || buf[1] !== 0x44 || buf[2] !== 0x33)
    return { url: null, needMore: false };
  const major = buf[3];
  // syncsafe 整数
  const tagSize =
    ((buf[6] & 0x7f) << 21) |
    ((buf[7] & 0x7f) << 14) |
    ((buf[8] & 0x7f) << 7) |
    (buf[9] & 0x7f);
  const end = Math.min(10 + tagSize, buf.length);
  let p = 10;

  // v2.4 可能带 footer/extended header，简单跳过 extended header
  if (major >= 3 && buf[5] & 0x40 && p + 4 <= end) {
    const ext =
      major >= 4
        ? ((buf[p] & 0x7f) << 21) |
          ((buf[p + 1] & 0x7f) << 14) |
          ((buf[p + 2] & 0x7f) << 7) |
          (buf[p + 3] & 0x7f)
        : (buf[p] << 24) | (buf[p + 1] << 16) | (buf[p + 2] << 8) | buf[p + 3];

    p += ext + (major >= 4 ? 0 : 4);
  }

  while (p + (major <= 2 ? 6 : 10) <= end) {
    if (buf[p] === 0) break; // padding
    let id: string;
    let frameSize: number;
    let headerLen: number;

    if (major <= 2) {
      id = String.fromCharCode(buf[p], buf[p + 1], buf[p + 2]);
      frameSize = (buf[p + 3] << 16) | (buf[p + 4] << 8) | buf[p + 5];
      headerLen = 6;
    } else {
      id = String.fromCharCode(buf[p], buf[p + 1], buf[p + 2], buf[p + 3]);
      if (major === 4) {
        frameSize =
          ((buf[p + 4] & 0x7f) << 21) |
          ((buf[p + 5] & 0x7f) << 14) |
          ((buf[p + 6] & 0x7f) << 7) |
          (buf[p + 7] & 0x7f);
      } else {
        frameSize =
          (buf[p + 4] << 24) |
          (buf[p + 5] << 16) |
          (buf[p + 6] << 8) |
          buf[p + 7];
      }
      headerLen = 10;
    }
    if (frameSize <= 0 || p + headerLen + frameSize > end) {
      // 帧超出已取范围：整帧还没拉回来
      if (p + headerLen + frameSize > 10 + tagSize)
        return { url: null, needMore: false };

      return { url: null, needMore: true };
    }

    if (id === "APIC" || id === "PIC") {
      const body = buf.subarray(p + headerLen, p + headerLen + frameSize);
      const enc = body[0];
      let q = 1;
      let mime = "image/jpeg";

      if (id === "APIC") {
        let mimeEnd = q;

        while (mimeEnd < body.length && body[mimeEnd] !== 0) mimeEnd++;
        mime = decodeText(body.subarray(q, mimeEnd), 0) || mime;
        if (!mime.includes("/")) mime = `image/${mime.toLowerCase()}`;
        q = mimeEnd + 1;
      } else {
        q = 4; // v2.2 PIC：3 字符格式（JPG/PNG）
      }
      q += 1; // picture type
      // 描述按编码读到终止符（utf16 双 0）
      if (enc === 1 || enc === 2) {
        while (q + 1 < body.length && !(body[q] === 0 && body[q + 1] === 0))
          q += 2;
        q += 2;
      } else {
        while (q < body.length && body[q] !== 0) q++;
        q += 1;
      }

      const url = bytesToObjectUrl(body.subarray(q), mime);

      if (url) return { url, needMore: false };
    }
    p += headerLen + frameSize;
  }

  return { url: null, needMore: false };
}

// --- FLAC -----------------------------------------------------------------

/** 解析 FLAC METADATA_BLOCK_PICTURE。 */
function parseFlacPicture(bytes: Uint8Array): string | null {
  if (bytes.length < 32) return null;
  const view = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);
  let p = 0;
  const readU32 = () => {
    const v = view.getUint32(p);

    p += 4;

    return v;
  };

  readU32(); // picture type
  const mimeLen = readU32();
  const mime = new TextDecoder().decode(bytes.subarray(p, p + mimeLen));

  p += mimeLen;
  const descLen = readU32();

  p += descLen + 16; // 描述 + 宽/高/色深/色彩数
  const dataLen = readU32();

  if (dataLen <= 0 || p + dataLen > bytes.length) return null;

  return bytesToObjectUrl(bytes.subarray(p, p + dataLen), mime || "image/jpeg");
}

function parseFlac(buf: Uint8Array): string | null {
  if (
    buf.length < 8 ||
    buf[0] !== 0x66 ||
    buf[1] !== 0x4c ||
    buf[2] !== 0x61 ||
    buf[3] !== 0x43
  )
    return null;
  let p = 4;

  while (p + 4 <= buf.length) {
    const header = buf[p];
    const type = header & 0x7f;
    const size = (buf[p + 1] << 16) | (buf[p + 2] << 8) | buf[p + 3];

    if (type === 6) {
      if (p + 4 + size > buf.length) return null; // 图片块被截断，不再重拉（少见）

      return parseFlacPicture(buf.subarray(p + 4, p + 4 + size));
    }
    if (header & 0x80) break; // last block
    p += 4 + size;
  }

  return null;
}

// --- OGG（Vorbis 注释里的 Base64 图片块） ----------------------------------

function parseOgg(buf: Uint8Array): string | null {
  const marker = "METADATA_BLOCK_PICTURE=";
  const head = new TextDecoder().decode(
    buf.subarray(0, Math.min(buf.length, 256 * 1024)),
  );
  const at = head.indexOf(marker);

  if (at < 0) return null;
  let end = at + marker.length;

  while (end < head.length && /[A-Za-z0-9+/=]/.test(head[end])) end++;
  try {
    const bin = atob(head.slice(at + marker.length, end));
    const bytes = new Uint8Array(bin.length);

    for (let i = 0; i < bin.length; i++) bytes[i] = bin.charCodeAt(i);

    return parseFlacPicture(bytes);
  } catch {
    return null;
  }
}

// --- MP4 (m4a)：扫 covr 原子 ----------------------------------------------

function parseMp4Covr(bytes: Uint8Array): string | null {
  const text = new TextDecoder("latin1").decode(bytes);
  const at = text.indexOf("covr");

  if (at < 0 || at + 8 > bytes.length) return null;
  // covr 原子：size(4) 'covr' data 原子：size(4) 'data' ver/flags(4) size(4) payload
  let p = at + 4;
  const view = new DataView(bytes.buffer, bytes.byteOffset, bytes.byteLength);

  while (p + 16 <= bytes.length) {
    const size = view.getUint32(p);

    if (size < 16 || p + size > bytes.length) return null;
    if (bytes[p + 4] === 0x64 && bytes[p + 5] === 0x61) {
      // 'da...' → data
      const mime = view.getUint32(p + 8) === 14 ? "image/png" : "image/jpeg";
      const dataLen = size - 16;

      if (dataLen < 64) return null;

      return bytesToObjectUrl(bytes.subarray(p + 16, p + 16 + dataLen), mime);
    }
    p += size;
  }

  return null;
}

// --- 外挂图片兜底 -----------------------------------------------------------

const SIDEWORK_NAMES = [
  "cover",
  "folder",
  "front",
  "album",
  "artwork",
  "thumb",
];
const SIDEWORK_EXTS = [".jpg", ".jpeg", ".png", ".webp"];

async function trySidecarCover(filePath: string): Promise<string | null> {
  const dir = filePath.replace(/[\\/][^\\/]+$/, "");
  const base = `${dir}/`;

  for (const name of SIDEWORK_NAMES) {
    for (const ext of SIDEWORK_EXTS) {
      const url = `/localfile?path=${encodeURIComponent(base + name + ext)}`;

      try {
        const res = await fetch(url, { method: "HEAD" });

        if (res.ok) return url;
      } catch {
        /* 忽略，继续探测 */
      }
    }
  }

  return null;
}

// --- 主入口 ----------------------------------------------------------------

async function extract(filePath: string): Promise<string | null> {
  const lower = filePath.toLowerCase();
  let buf: Uint8Array;

  try {
    buf = await fetchBytes(filePath, 0, HEAD_BYTES);
  } catch {
    return null;
  }

  // ID3 标签可能超出首次取回的范围：按头部声明的标签大小精确补拉
  if (
    buf.length >= 10 &&
    buf[0] === 0x49 &&
    buf[1] === 0x44 &&
    buf[2] === 0x33
  ) {
    const tagSize =
      ((buf[6] & 0x7f) << 21) |
      ((buf[7] & 0x7f) << 14) |
      ((buf[8] & 0x7f) << 7) |
      (buf[9] & 0x7f);
    let parsed = parseId3(buf);

    if (!parsed.url && parsed.needMore && 10 + tagSize <= 8 * 1024 * 1024) {
      try {
        const full = await fetchBytes(filePath, 0, 10 + tagSize);

        parsed = parseId3(full);
      } catch {
        /* ignore */
      }
    }
    if (parsed.url) return parsed.url;
  } else if (lower.endsWith(".flac")) {
    const url = parseFlac(buf);

    if (url) return url;
  } else if (lower.endsWith(".ogg") || lower.endsWith(".opus")) {
    const url = parseOgg(buf);

    if (url) return url;
  } else if (
    lower.endsWith(".m4a") ||
    lower.endsWith(".mp4") ||
    lower.endsWith(".aac")
  ) {
    let url = parseMp4Covr(buf);

    if (!url) {
      // iTunes 系工具常把 moov 写在文件尾部：兜底扫最后 1MB
      try {
        const probe = await fetch(resolveTrackUrl(filePath), {
          method: "HEAD",
        });
        const total = Number(
          probe.headers.get("Content-Range")?.split("/")[1] ??
            probe.headers.get("Content-Length") ??
            0,
        );

        if (total > TAIL_BYTES) {
          const tail = await fetchBytes(filePath, total - TAIL_BYTES, total);

          url = parseMp4Covr(tail);
        }
      } catch {
        /* ignore */
      }
    }
    if (url) return url;
  }

  return trySidecarCover(filePath);
}

/** 取曲目封面 ObjectURL（内嵌优先，外挂兜底）；无封面返回 null。结果带缓存。 */
export function getCoverUrl(filePath: string): Promise<string | null> {
  if (!filePath) return Promise.resolve(null);
  const hit = cache.get(filePath);

  if (hit) return Promise.resolve(hit);
  const running = inflight.get(filePath);

  if (running) return running;
  const task = extract(filePath)
    .catch(() => null)
    .then((url) => {
      inflight.delete(filePath);
      cachePut(filePath, url);

      return url;
    });

  inflight.set(filePath, task);

  return task;
}
