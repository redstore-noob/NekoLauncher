/*
 * 安装引擎：把载荷 zip 按 top-level 布局解压到目标目录，写 neko-solo.json 标记。
 * 布局约定（与 NekoLauncher internal/solo 导出一致）：
 *
 *   files/…                → 安装根目录（NekoLauncher.exe、portable.flag）
 *   minecraft/…            → <根>/NekoLauncher-data/minecraft/…（版本 + 整合包内容）
 *   jre/…                  → <根>/NekoLauncher-data/runtime/jre/…（捆绑 Java）
 *   manifest.json / icon.png → 不落盘（元数据与安装器界面用）
 *
 * 三种安装模式（DetectInstallMode）：
 *   Fresh      目标没有启动器本体 → 全量安装（本体 + Java + 整合包 + portable.flag）
 *   AddPack    有启动器但不是本安装包管理的（无 neko-solo.json）→ 只装整合包实例，
 *              绝不覆盖启动器本体、不写 portable.flag（不改变启动器的便携/配置形态）
 *   UpdatePack 本安装包曾装过（有 neko-solo.json）→ 更新整合包，保留存档与设置
 *
 * AddPack / UpdatePack 共同规则：不覆盖 files/（启动器本体）；已有捆绑 JRE 时
 * 不覆盖；配置文件（launcher.yaml / accounts.yaml）从不触碰——首启行为由启动器
 * 内部的标记消费逻辑（applied + 只在未配置时接管）保证。
 */
using System;
using System.IO;
using System.IO.Compression;
using System.Runtime.Serialization.Json;

namespace NekoSolo.Installer
{
    internal enum InstallMode
    {
        Fresh,
        AddPack,
        UpdatePack,
    }

    internal sealed class InstallProgress
    {
        public string CurrentFile { get; set; }
        public double Percent { get; set; }
    }

    internal static class InstallerEngine
    {
        public const string DataDirectoryName = "NekoLauncher-data";
        public const string MarkerFileName = "neko-solo.json";
        public const string LauncherFileName = "NekoLauncher.exe";
        public const string PortableFlagName = "portable.flag";

        /// <summary>v3 待装 mrpack 的落盘文件名（数据目录 pending-pack/ 下）。</summary>
        public const string PendingPackFileName = "pack.mrpack";

        /// <summary>标准 Modrinth 整合包的索引条目名（v3 载荷判定依据）。</summary>
        public const string ModrinthIndexEntry = "modrinth.index.json";

        public static InstallMode DetectInstallMode(string root)
        {
            if (!File.Exists(Path.Combine(root, LauncherFileName)))
                return InstallMode.Fresh;
            if (File.Exists(Path.Combine(root, DataDirectoryName, MarkerFileName)))
                return InstallMode.UpdatePack;
            return InstallMode.AddPack;
        }

        /// <summary>
        /// 标记文件的落点：便携布局放数据目录（启动器首启会读到）；
        /// 独立安装的非便携启动器读的是 %USERPROFILE%\NekoLauncher，标记写到那里。
        /// </summary>
        public static string ResolveMarkerDirectory(string root, InstallMode mode)
        {
            if (mode == InstallMode.Fresh || IsPortableLayout(root))
                return Path.Combine(root, DataDirectoryName);
            string userProfile = Environment.GetFolderPath(Environment.SpecialFolder.UserProfile);
            return Path.Combine(userProfile, "NekoLauncher");
        }

        public static bool IsPortableLayout(string root)
        {
            return File.Exists(Path.Combine(root, PortableFlagName))
                || Directory.Exists(Path.Combine(root, DataDirectoryName));
        }

        public static string ExtractIconToTemp(IPayloadPackage payload)
        {
            if (string.IsNullOrEmpty(payload.Manifest.IconPath)) return null;
            using (var zip = payload.OpenZip())
            {
                foreach (var entry in zip.Entries)
                {
                    if (!string.Equals(entry.FullName, payload.Manifest.IconPath, StringComparison.OrdinalIgnoreCase))
                        continue;
                    string temp = Path.Combine(Path.GetTempPath(), "nekosolo-" + Guid.NewGuid().ToString("N") + ".png");
                    using (var source = entry.Open())
                    using (var target = File.Create(temp))
                    {
                        source.CopyTo(target);
                    }
                    return temp;
                }
            }
            return null;
        }

        public static void Install(IPayloadPackage payload, string root, Action<InstallProgress> progress)
        {
            var manifest = payload.Manifest;
            InstallMode mode = DetectInstallMode(root);
            string dataDirectory = Path.Combine(root, DataDirectoryName);
            string markerDirectory = ResolveMarkerDirectory(root, mode);
            string markerPath = Path.Combine(markerDirectory, MarkerFileName);

            // v2 在线安装包：先下载载荷 zip（进度复用同一回调），后续流程与 v1 完全一致
            if (payload is RemotePayload remote)
                remote.EnsureDownloaded(progress);

            if (IsModrinthPayload(payload))
            {
                InstallV3(payload, manifest, mode, root, dataDirectory, markerPath, progress);
                return;
            }
            InstallLegacy(payload, manifest, mode, root, dataDirectory, progress);
        }

        /// <summary>载荷是否为 v3 Modrinth 整合包结构（含 modrinth.index.json）。</summary>
        public static bool IsModrinthPayload(IPayloadPackage payload)
        {
            if (payload is SoloPayload embedded) return embedded.IsV3;
            try
            {
                using (var zip = payload.OpenZip())
                {
                    foreach (var entry in zip.Entries)
                    {
                        if (string.Equals(entry.FullName, ModrinthIndexEntry, StringComparison.OrdinalIgnoreCase))
                            return true;
                    }
                }
            }
            catch
            {
                // 打不开按旧格式处理，由后续流程报具体错误
            }
            return false;
        }

        /// <summary>
        /// v3 安装：载荷是标准 Modrinth 整合包 + 启动器条目。只解压启动器本体，
        /// 整包转存为待装 mrpack——mod / Minecraft 本体 / Java 全部由启动器
        /// 首启联网补全（见 NekoLauncher 的 solo 标记消费逻辑）。
        /// </summary>
        private static void InstallV3(
            IPayloadPackage payload, SoloManifest manifest, InstallMode mode,
            string root, string dataDirectory, string markerPath, Action<InstallProgress> progress)
        {
            // 1. 解压启动器本体（仅全新安装；已有安装绝不覆盖）
            if (mode == InstallMode.Fresh)
            {
                using (var zip = payload.OpenZip())
                {
                    foreach (var entry in zip.Entries)
                    {
                        string name = entry.FullName.Replace('\\', '/');
                        if (!name.StartsWith("files/", StringComparison.OrdinalIgnoreCase)) continue;
                        string relative = name.Substring("files/".Length);
                        if (string.IsNullOrEmpty(relative) || relative.EndsWith("/", StringComparison.Ordinal)) continue;

                        reportProgress(progress, name, 0, 100);
                        string destination = SafeCombine(root, relative);
                        Directory.CreateDirectory(Path.GetDirectoryName(destination));
                        if (File.Exists(destination)) File.SetAttributes(destination, FileAttributes.Normal);
                        using (var source = entry.Open())
                        using (var target = File.Create(destination))
                        {
                            source.CopyTo(target);
                        }
                    }
                }
                string portableFlag = Path.Combine(root, PortableFlagName);
                if (!File.Exists(portableFlag)) File.WriteAllBytes(portableFlag, new byte[0]);
            }

            // 2. 整包转存为待装 mrpack（启动器首启装它：解压 overrides + 联网下载 mods）
            reportProgress(progress, "正在保存整合包内容", 40, 100);
            string pendingDirectory = Path.Combine(dataDirectory, "pending-pack");
            string pendingPath = Path.Combine(pendingDirectory, PendingPackFileName);
            Directory.CreateDirectory(pendingDirectory);
            payload.CopyPayloadTo(pendingPath);
            reportProgress(progress, "正在写入安装标记", 90, 100);

            // 3. 写首启标记
            WriteSoloMarker(manifest, mode, markerPath, dataDirectory, javaExecutable: null, pendingPayload: pendingPath);

            // 4. 全新安装：创建桌面快捷方式（失败不影响安装结果）
            if (mode == InstallMode.Fresh)
                TryCreateDesktopShortcut(manifest, root);

            reportProgress(progress, null, 100, 100);
        }

        /// <summary>
        /// 创建桌面快捷方式：全新安装后在桌面生成指向 NekoLauncher.exe 的快捷方式，
        /// 名称用整合包名（文件名安全化）。用 WScript.Shell COM（无需额外依赖），
        /// 任何失败（权限、COM 不可用等）都不影响安装成功。
        /// </summary>
        private static void TryCreateDesktopShortcut(SoloManifest manifest, string root)
        {
            try
            {
                string launcherPath = Path.Combine(root, "NekoLauncher.exe");
                if (!File.Exists(launcherPath)) return;

                string desktop = Environment.GetFolderPath(Environment.SpecialFolder.DesktopDirectory);
                if (string.IsNullOrEmpty(desktop)) return;

                // 快捷方式名用整合包名，剔除非法文件名字符
                string name = (manifest.PackName ?? "").Trim();
                if (name.Length == 0) name = "NekoLauncher";
                foreach (char invalid in Path.GetInvalidFileNameChars())
                    name = name.Replace(invalid, '_');
                string shortcutPath = Path.Combine(desktop, name + ".lnk");
                if (File.Exists(shortcutPath)) return; // 不覆盖用户已有同名快捷方式

                Type shellType = Type.GetTypeFromProgID("WScript.Shell");
                if (shellType == null) return;
                dynamic shell = Activator.CreateInstance(shellType);
                dynamic shortcut = shell.CreateShortcut(shortcutPath);
                shortcut.TargetPath = launcherPath;
                shortcut.WorkingDirectory = root;
                shortcut.Description = "启动 " + name + "（NekoLauncher）";
                shortcut.Save();
            }
            catch (Exception)
            {
                // 快捷方式创建失败不影响安装成功
            }
        }

        /// <summary>
        /// v1/v2 旧格式安装：载荷自带版本目录与（可选）捆绑 JRE，按 top-level 布局解压。
        /// 仅为兼容旧安装包保留。
        /// </summary>
        private static void InstallLegacy(
            IPayloadPackage payload, SoloManifest manifest, InstallMode mode,
            string root, string dataDirectory, Action<InstallProgress> progress)
        {
            // 已有捆绑 JRE 时不覆盖（v1 已知取舍：多包共用同一 JRE，见 FORMAT.md）
            string javaExe = Path.Combine(dataDirectory, "runtime", "jre", "bin", "java.exe");
            bool keepExistingJava = manifest.HasJava && File.Exists(javaExe);

            long totalBytes = 0;
            long writtenBytes = 0;
            using (var zip = payload.OpenZip())
            {
                foreach (var entry in zip.Entries) totalBytes += entry.Length;
                foreach (var entry in zip.Entries)
                {
                    string name = entry.FullName.Replace('\\', '/');
                    if (string.IsNullOrEmpty(name) || name.EndsWith("/", StringComparison.Ordinal)) continue;
                    if (name.Equals("manifest.json", StringComparison.OrdinalIgnoreCase)) continue;
                    if (!string.IsNullOrEmpty(manifest.IconPath) &&
                        name.Equals(manifest.IconPath, StringComparison.OrdinalIgnoreCase)) continue;

                    string relative;
                    string destinationRoot;
                    if (name.StartsWith("files/", StringComparison.OrdinalIgnoreCase))
                    {
                        // 已有启动器：绝不覆盖本体（含 portable.flag，避免改变其配置形态）
                        if (mode != InstallMode.Fresh) continue;
                        destinationRoot = root;
                        relative = name.Substring("files/".Length);
                    }
                    else if (name.StartsWith("minecraft/", StringComparison.OrdinalIgnoreCase))
                    {
                        destinationRoot = Path.Combine(dataDirectory, "minecraft");
                        relative = name.Substring("minecraft/".Length);
                        // 已有安装：保留玩家数据（存档与选项文件不覆盖）
                        if (mode != InstallMode.Fresh &&
                            (("/" + relative).Contains("/saves/") ||
                             relative.Equals("options.txt", StringComparison.OrdinalIgnoreCase)))
                            continue;
                    }
                    else if (name.StartsWith("jre/", StringComparison.OrdinalIgnoreCase))
                    {
                        if (keepExistingJava) continue;
                        destinationRoot = Path.Combine(dataDirectory, "runtime", "jre");
                        relative = name.Substring("jre/".Length);
                    }
                    else
                    {
                        continue; // 未知顶层条目：跳过，向前兼容
                    }

                    reportProgress(progress, name, writtenBytes, totalBytes);
                    string destination = SafeCombine(destinationRoot, relative);
                    Directory.CreateDirectory(Path.GetDirectoryName(destination));
                    if (File.Exists(destination)) File.SetAttributes(destination, FileAttributes.Normal);
                    using (var source = entry.Open())
                    using (var target = File.Create(destination))
                    {
                        var buffer = new byte[81920];
                        int read;
                        while ((read = source.Read(buffer, 0, buffer.Length)) > 0)
                        {
                            target.Write(buffer, 0, read);
                            writtenBytes += read;
                            reportProgress(progress, name, writtenBytes, totalBytes);
                        }
                    }
                }
            }

            // portable.flag 只在全新安装时写：已有安装不改变其便携/配置形态
            if (mode == InstallMode.Fresh)
            {
                string portableFlag = Path.Combine(root, PortableFlagName);
                if (!File.Exists(portableFlag)) File.WriteAllBytes(portableFlag, new byte[0]);
            }

            // 写首启标记：绝对路径在此刻落定（applied 语义见 WriteSoloMarker 注释）。
            string legacyMarkerPath = Path.Combine(ResolveMarkerDirectory(root, mode), MarkerFileName);
            WriteSoloMarker(manifest, mode, legacyMarkerPath, dataDirectory,
                manifest.HasJava && File.Exists(javaExe) ? javaExe : null, pendingPayload: null);
            reportProgress(progress, null, totalBytes, totalBytes);
        }

        /// <summary>
        /// 写首启标记：绝对路径在此刻落定。
        /// applied=false → 启动器首启应用本包（选中实例、补目录；配置只在未设置时接管）。
        /// 唯一例外：同包同版本的"修复重装"沿用原 applied，避免打扰用户当前选择。
        /// </summary>
        private static void WriteSoloMarker(
            SoloManifest manifest, InstallMode mode, string markerPath, string dataDirectory,
            string javaExecutable, string pendingPayload)
        {
            bool applied = false;
            var previousMarker = ReadMarker(markerPath);
            if (previousMarker != null &&
                string.Equals(previousMarker.PackId, manifest.PackId, StringComparison.OrdinalIgnoreCase) &&
                string.Equals(previousMarker.VersionId, manifest.VersionId, StringComparison.Ordinal))
            {
                applied = previousMarker.Applied;
                // 修复重装沿用旧待装包：不打扰已补全完成的安装
                if (applied && string.IsNullOrEmpty(previousMarker.PendingPayload))
                    pendingPayload = null;
            }

            var marker = new SoloMarker
            {
                Format = 1,
                Applied = applied,
                PackId = manifest.PackId,
                PackName = manifest.PackName,
                PackVersion = manifest.PackVersion,
                Author = manifest.Author,
                Description = manifest.Description,
                McVersion = manifest.McVersion,
                LoaderName = manifest.LoaderName,
                LoaderVersion = manifest.LoaderVersion,
                VersionId = manifest.VersionId,
                SimpleMode = manifest.SimpleMode,
                MinecraftDirectory = Path.Combine(dataDirectory, "minecraft"),
                JavaExecutable = javaExecutable,
                PendingPayload = pendingPayload,
                UpdateLink = manifest.UpdateLink,
            };
            WriteMarker(markerPath, marker);
        }

        /// <summary>
        /// 从安装包载荷中提取标准 Modrinth 整合包（.mrpack）：只保留
        /// modrinth.index.json 与 overrides/，过滤掉启动器条目与元数据，
        /// 产物可被任意支持 Modrinth 格式的启动器导入。
        /// </summary>
        public static void ExtractModpack(IPayloadPackage payload, string outputPath)
        {
            using (var source = payload.OpenZip())
            using (var output = File.Create(outputPath))
            using (var archive = new ZipArchive(output, ZipArchiveMode.Create))
            {
                foreach (var entry in source.Entries)
                {
                    string name = entry.FullName.Replace('\\', '/');
                    if (name.EndsWith("/", StringComparison.Ordinal)) continue;
                    if (name.Equals("manifest.json", StringComparison.OrdinalIgnoreCase)) continue;
                    if (name.StartsWith("files/", StringComparison.OrdinalIgnoreCase)) continue;
                    var target = archive.CreateEntry(name, CompressionLevel.Optimal);
                    using (var from = entry.Open())
                    using (var to = target.Open())
                    {
                        from.CopyTo(to);
                    }
                }
            }
        }

        private static void reportProgress(Action<InstallProgress> progress, string file, long written, long total)
        {
            if (progress == null) return;
            progress(new InstallProgress
            {
                CurrentFile = file,
                Percent = total <= 0 ? 0 : Math.Min(100.0, written * 100.0 / total),
            });
        }

        /// <summary>拼接并约束目标路径不得逃出 destinationRoot（防 zip 内恶意路径穿越）。</summary>
        private static string SafeCombine(string root, string relative)
        {
            relative = relative.Replace('/', Path.DirectorySeparatorChar);
            var candidate = Path.GetFullPath(Path.Combine(root, relative));
            var fullRoot = Path.GetFullPath(root);
            if (!candidate.StartsWith(fullRoot.EndsWith(Path.DirectorySeparatorChar.ToString())
                    ? fullRoot
                    : fullRoot + Path.DirectorySeparatorChar, StringComparison.OrdinalIgnoreCase))
                throw new InvalidDataException("载荷包含越界路径：" + relative);
            return candidate;
        }

        private static SoloMarker ReadMarker(string markerPath)
        {
            try
            {
                var serializer = new DataContractJsonSerializer(typeof(SoloMarker));
                using (var stream = File.OpenRead(markerPath))
                {
                    return (SoloMarker)serializer.ReadObject(stream);
                }
            }
            catch
            {
                return null; // 标记缺失或损坏按无标记处理
            }
        }

        private static void WriteMarker(string markerPath, SoloMarker marker)
        {
            Directory.CreateDirectory(Path.GetDirectoryName(markerPath));
            var serializer = new DataContractJsonSerializer(typeof(SoloMarker));
            string temp = markerPath + ".tmp";
            using (var stream = File.Create(temp))
            {
                serializer.WriteObject(stream, marker);
            }
            // .NET Framework 的 File.Move 不允许目标存在：更新重装必须先删旧标记
            if (File.Exists(markerPath)) File.Delete(markerPath);
            File.Move(temp, markerPath);
        }
    }
}
