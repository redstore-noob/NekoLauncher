/*
 * 载荷定位：NekoSolo 安装包 = 安装器模板（本程序）+ 载荷 zip + 32 字节尾标。
 * 尾标结构（小端）：[0:8] 魔数 "NKSOLO1\x01"、[8:16] 载荷偏移、[16:24] 载荷长度、
 * [24:28] 载荷 CRC32、[28:32] 保留。只读文件尾部并按需寻址读取，
 * 不把整个 exe 装进内存。
 *
 * 关键点：ZipArchive 需要可寻址流（中央目录在载荷末尾），SubStream 把
 * 底层 FileStream 的 [载荷起点, 载荷终点) 区间映射成一个可 Seek 的独立流。
 */
using System;
using System.IO;
using System.IO.Compression;
using System.Runtime.Serialization.Json;
using System.Text;

namespace NekoSolo.Installer
{
    internal sealed class SoloPayload : IDisposable
    {
        private readonly FileStream _stream;
        private readonly long _payloadStart;

        private SoloPayload(FileStream stream, long payloadStart, long length, SoloManifest manifest)
        {
            _stream = stream;
            _payloadStart = payloadStart;
            Length = length;
            Manifest = manifest;
        }

        public long Length { get; }

        public SoloManifest Manifest { get; }

        /// <summary>以载荷为内容打开 zip 档案（可寻址，中央目录按需读取）。</summary>
        public ZipArchive OpenZip()
        {
            return new ZipArchive(new SubStream(_stream, _payloadStart, Length), ZipArchiveMode.Read, leaveOpen: false);
        }

        public void Dispose()
        {
            if (_stream != null) _stream.Dispose();
        }

        public static SoloPayload Open(string exePath)
        {
            var stream = new FileStream(exePath, FileMode.Open, FileAccess.Read, FileShare.Read);
            try
            {
                if (stream.Length < 32)
                    throw new InvalidDataException("文件太小，不可能是 NekoSolo 安装包。");

                var trailer = new byte[32];
                stream.Seek(-32, SeekOrigin.End);
                ReadExact(stream, trailer, trailer.Length);

                if (trailer[0] != (byte)'N' || trailer[1] != (byte)'K' || trailer[2] != (byte)'S' ||
                    trailer[3] != (byte)'O' || trailer[4] != (byte)'L' || trailer[5] != (byte)'O' ||
                    trailer[6] != (byte)'1' || trailer[7] != 0x01)
                    throw new InvalidDataException("不是有效的 NekoSolo 安装包（尾标魔数不符）。");

                long offset = BitConverter.ToInt64(trailer, 8);
                long length = BitConverter.ToInt64(trailer, 16);
                uint crc = BitConverter.ToUInt32(trailer, 24);

                if (offset < 0 || length < 0 || offset + length > stream.Length - 32)
                    throw new InvalidDataException("NekoSolo 安装包尾标非法（载荷越界），文件可能已损坏。");

                // CRC32（IEEE）校验：网盘下载损坏是分发的头号事故
                stream.Seek(offset, SeekOrigin.Begin);
                var crc32 = new Crc32();
                var buffer = new byte[81920];
                long remaining = length;
                while (remaining > 0)
                {
                    int read = stream.Read(buffer, 0, (int)Math.Min(buffer.Length, remaining));
                    if (read <= 0) throw new IOException("读取载荷时文件意外结束。");
                    crc32.Update(buffer, 0, read);
                    remaining -= read;
                }
                if (crc32.Value != crc)
                    throw new InvalidDataException("载荷校验失败（CRC 不符），安装包可能下载不完整，请重新下载。");

                var manifest = ReadManifest(stream, offset, length);
                return new SoloPayload(stream, offset, length, manifest);
            }
            catch
            {
                stream.Dispose();
                throw;
            }
        }

        private static SoloManifest ReadManifest(FileStream stream, long offset, long length)
        {
            // 整个载荷区间映射为可寻址流：ZipArchive 从末尾找中央目录，按需读取条目，
            // 只把 manifest.json（几百字节）解压进内存
            using (var zip = new ZipArchive(new SubStream(stream, offset, length), ZipArchiveMode.Read, leaveOpen: true))
            {
                ZipArchiveEntry entry = null;
                foreach (var candidate in zip.Entries)
                {
                    if (string.Equals(candidate.FullName, "manifest.json", StringComparison.OrdinalIgnoreCase))
                    {
                        entry = candidate;
                        break;
                    }
                }
                if (entry == null)
                    throw new InvalidDataException("载荷缺少 manifest.json，不是有效的 NekoSolo 整合包。");

                string json;
                using (var entryStream = entry.Open())
                using (var reader = new StreamReader(entryStream, Encoding.UTF8))
                {
                    json = reader.ReadToEnd();
                }
                var manifest = ParseManifest(json);
                if (manifest.Format != 1)
                    throw new InvalidDataException(
                        "不支持的 NekoSolo 载荷格式：" + manifest.Format + "。请获取更新版本的安装器。");
                if (string.IsNullOrWhiteSpace(manifest.PackName) || string.IsNullOrWhiteSpace(manifest.VersionId))
                    throw new InvalidDataException("载荷 manifest.json 缺少整合包名称或版本信息。");
                return manifest;
            }
        }

        internal static SoloManifest ParseManifest(string json)
        {
            var serializer = new DataContractJsonSerializer(typeof(SoloManifest));
            using (var memory = new MemoryStream(Encoding.UTF8.GetBytes(json)))
            {
                return (SoloManifest)serializer.ReadObject(memory);
            }
        }

        private static void ReadExact(FileStream stream, byte[] buffer, int count)
        {
            int total = 0;
            while (total < count)
            {
                int read = stream.Read(buffer, total, count - total);
                if (read <= 0) throw new IOException("读取安装包时文件意外结束。");
                total += read;
            }
        }
    }

    /// <summary>
    /// 把底层流的 [start, start+length) 区间映射为独立可寻址流（供 ZipArchive 使用）。
    /// 不拥有底层流：Dispose 不关闭 inner。
    /// </summary>
    internal sealed class SubStream : Stream
    {
        private readonly Stream _inner;
        private readonly long _start;
        private readonly long _length;
        private long _position;

        public SubStream(Stream inner, long start, long length)
        {
            _inner = inner;
            _start = start;
            _length = length;
        }

        public override bool CanRead { get { return true; } }
        public override bool CanSeek { get { return true; } }
        public override bool CanWrite { get { return false; } }
        public override long Length { get { return _length; } }

        public override long Position
        {
            get { return _position; }
            set { Seek(value, SeekOrigin.Begin); }
        }

        public override int Read(byte[] buffer, int offset, int count)
        {
            long remaining = _length - _position;
            if (remaining <= 0) return 0;
            if (count > remaining) count = (int)remaining;
            if (_inner.Position != _start + _position)
                _inner.Seek(_start + _position, SeekOrigin.Begin);
            int read = _inner.Read(buffer, offset, count);
            if (read > 0) _position += read;
            return read;
        }

        public override long Seek(long offset, SeekOrigin origin)
        {
            long target;
            if (origin == SeekOrigin.Begin) target = offset;
            else if (origin == SeekOrigin.Current) target = _position + offset;
            else target = _length + offset;
            if (target < 0) target = 0;
            if (target > _length) target = _length;
            _position = target;
            return _position;
        }

        public override void Flush() { }
        public override void SetLength(long value) { throw new NotSupportedException(); }
        public override void Write(byte[] buffer, int offset, int count) { throw new NotSupportedException(); }
    }

    /// <summary>标准 CRC32（IEEE 802.3，多项式 0xEDB88320），与 Go 的 hash/crc32 一致。</summary>
    internal sealed class Crc32
    {
        private static readonly uint[] Table = BuildTable();
        private uint _value = 0xFFFFFFFFu;

        public uint Value { get { return _value ^ 0xFFFFFFFFu; } }

        public void Update(byte[] buffer, int offset, int count)
        {
            for (int i = offset; i < offset + count; i++)
                _value = Table[(_value ^ buffer[i]) & 0xFF] ^ (_value >> 8);
        }

        private static uint[] BuildTable()
        {
            var table = new uint[256];
            for (uint i = 0; i < 256; i++)
            {
                uint entry = i;
                for (int bit = 0; bit < 8; bit++)
                    entry = (entry & 1) != 0 ? (entry >> 1) ^ 0xEDB88320u : entry >> 1;
                table[i] = entry;
            }
            return table;
        }
    }
}
