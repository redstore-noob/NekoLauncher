/*
 * v2 在线安装包的载荷获取：exe 内不含载荷，只有远程清单（元数据 + 下载地址）。
 * RemotePayload 在安装时把载荷 zip 下载到临时文件，校验大小与 CRC32 后，
 * 以与 v1 完全相同的方式（ZipArchive）交给安装引擎解压——三种安装模式、
 * 首启标记等逻辑零改动复用。
 *
 * 下载目标以 GitHub Releases 为首选托管（资产直链稳定、无需登录），
 * 任何 https 直链同样可用。HttpClient 默认跟随 302 到 CDN。
 */
using System;
using System.IO;
using System.IO.Compression;
using System.Net;
using System.Threading;

namespace NekoSolo.Installer
{
    /// <summary>v1（内嵌）与 v2（远程下载）载荷的共同视图。</summary>
    internal interface IPayloadPackage
    {
        SoloManifest Manifest { get; }

        /// <summary>以载荷为内容打开 zip 档案；远程载荷必须先 EnsureDownloaded。</summary>
        ZipArchive OpenZip();
    }

    internal sealed class RemotePayload : IPayloadPackage, IDisposable
    {
        // 单例 HttpClient：DNS/连接池复用；GitHub Releases 资产会 302 到
        // objects.githubusercontent.com，默认重定向即可
        private static readonly System.Net.Http.HttpClient Http = new System.Net.Http.HttpClient
        {
            Timeout = TimeSpan.FromMinutes(30),
        };

        static RemotePayload()
        {
            // TLS 1.2：Windows 7/8 上 .NET Framework 4.8 默认协议可能不含它
            ServicePointManager.SecurityProtocol |= SecurityProtocolType.Tls12;
        }

        private readonly object _gate = new object();
        private string _localPath;

        public RemotePayload(SoloManifest manifest)
        {
            Manifest = manifest;
        }

        public SoloManifest Manifest { get; }

        /// <summary>载荷 zip 是否已就绪（供图标展示等安装前逻辑判断）。</summary>
        public bool Downloaded
        {
            get { return _localPath != null && File.Exists(_localPath); }
        }

        public ZipArchive OpenZip()
        {
            if (!Downloaded)
                throw new InvalidOperationException("载荷尚未下载。");
            var stream = new FileStream(_localPath, FileMode.Open, FileAccess.Read, FileShare.Read);
            return new ZipArchive(stream, ZipArchiveMode.Read, leaveOpen: false);
        }

        /// <summary>
        /// 下载载荷 zip 到临时文件（幂等：已下载则直接返回）。逐块校验长度，
        /// 完成后校验 CRC32——与 v1 的本地校验同级别，防下载损坏。
        /// </summary>
        public string EnsureDownloaded(Action<InstallProgress> progress)
        {
            lock (_gate)
            {
                if (Downloaded) return _localPath;

                var manifest = Manifest;
                string temp = Path.Combine(
                    Path.GetTempPath(), "nekosolo-payload-" + Guid.NewGuid().ToString("N") + ".zip");
                try
                {
                    using (var response = Http.GetAsync(manifest.PayloadUrl,
                        System.Net.Http.HttpCompletionOption.ResponseHeadersRead).Result)
                    {
                        response.EnsureSuccessStatusCode();
                        long expected = manifest.PayloadSize;
                        using (var source = response.Content.ReadAsStreamAsync().Result)
                        using (var target = File.Create(temp))
                        {
                            var crc32 = new Crc32();
                            var buffer = new byte[81920];
                            long written = 0;
                            int read;
                            while ((read = source.Read(buffer, 0, buffer.Length)) > 0)
                            {
                                target.Write(buffer, 0, read);
                                crc32.Update(buffer, 0, read);
                                written += read;
                                Report(progress, written);
                            }
                            if (expected > 0 && written != expected)
                                throw new IOException(string.Format(
                                    "下载不完整：得到 {0} 字节，应为 {1} 字节。", written, expected));
                            if (crc32.Value != manifest.PayloadCrc32)
                                throw new IOException("下载的整合包内容校验失败（CRC 不符），请重试安装。");
                        }
                    }
                    _localPath = temp;
                    return _localPath;
                }
                catch
                {
                    try { File.Delete(temp); } catch { }
                    throw;
                }
            }
        }

        private void Report(Action<InstallProgress> progress, long written)
        {
            if (progress == null) return;
            long total = Manifest.PayloadSize;
            progress(new InstallProgress
            {
                CurrentFile = "正在下载整合包内容",
                Percent = total <= 0 ? 0 : Math.Min(100.0, written * 100.0 / total),
            });
        }

        public void Dispose()
        {
            lock (_gate)
            {
                if (_localPath != null)
                {
                    try { File.Delete(_localPath); } catch { }
                    _localPath = null;
                }
            }
        }
    }
}
