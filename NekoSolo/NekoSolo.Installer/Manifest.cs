/*
 * NekoSolo 载荷清单与首启标记的数据模型。
 * 字段名与 NekoLauncher internal/solo 的 Go 结构保持一致（camelCase JSON）。
 */
using System.Runtime.Serialization;

namespace NekoSolo.Installer
{
    [DataContract]
    internal sealed class SoloManifest
    {
        [DataMember(Name = "format")]
        public int Format { get; set; }

        [DataMember(Name = "packId")]
        public string PackId { get; set; }

        [DataMember(Name = "packName")]
        public string PackName { get; set; }

        [DataMember(Name = "packVersion")]
        public string PackVersion { get; set; }

        [DataMember(Name = "author")]
        public string Author { get; set; }

        [DataMember(Name = "description")]
        public string Description { get; set; }

        [DataMember(Name = "mcVersion")]
        public string McVersion { get; set; }

        [DataMember(Name = "loaderName")]
        public string LoaderName { get; set; }

        [DataMember(Name = "loaderVersion")]
        public string LoaderVersion { get; set; }

        [DataMember(Name = "versionId")]
        public string VersionId { get; set; }

        [DataMember(Name = "simpleMode")]
        public bool SimpleMode { get; set; }

        [DataMember(Name = "hasJava")]
        public bool HasJava { get; set; }

        [DataMember(Name = "iconPath")]
        public string IconPath { get; set; }

        [DataMember(Name = "updateLink")]
        public string UpdateLink { get; set; }

        // ---- v2 在线安装包（尾标 NKSOLO2）内嵌远程清单的额外字段 ----

        /// <summary>载荷 zip 的下载地址（如 GitHub Releases 资产直链）；v1 为 null。</summary>
        [DataMember(Name = "payloadUrl")]
        public string PayloadUrl { get; set; }

        /// <summary>载荷 zip 的字节数（下载完整性校验之一）。</summary>
        [DataMember(Name = "payloadSize")]
        public long PayloadSize { get; set; }

        /// <summary>载荷 zip 的 CRC32（IEEE，与 Go hash/crc32 一致）。</summary>
        [DataMember(Name = "payloadCrc32")]
        public uint PayloadCrc32 { get; set; }

        /// <summary>v2 在线安装包：载荷不在 exe 内，安装时需要联网下载。</summary>
        public bool IsRemote
        {
            get { return !string.IsNullOrEmpty(PayloadUrl); }
        }
    }

    /// <summary>modrinth.index.json 的最小解析视图：只取 files[].fileSize
    /// 供安装器欢迎页概览（mod 数量与下载体积），其余字段忽略。</summary>
    [DataContract]
    internal sealed class ModrinthIndex
    {
        [DataMember(Name = "files")]
        public ModrinthIndexFile[] Files { get; set; }
    }

    [DataContract]
    internal sealed class ModrinthIndexFile
    {
        [DataMember(Name = "fileSize")]
        public long FileSize { get; set; }
    }

    /// <summary>安装完成写入 NekoLauncher-data/neko-solo.json 的标记；
    /// 启动器首启消费一次（applied 置 true 后不再覆盖用户配置）。</summary>
    [DataContract]
    internal sealed class SoloMarker
    {
        [DataMember(Name = "format")]
        public int Format { get; set; }

        [DataMember(Name = "applied")]
        public bool Applied { get; set; }

        [DataMember(Name = "packId")]
        public string PackId { get; set; }

        [DataMember(Name = "packName")]
        public string PackName { get; set; }

        [DataMember(Name = "packVersion")]
        public string PackVersion { get; set; }

        [DataMember(Name = "author")]
        public string Author { get; set; }

        [DataMember(Name = "description")]
        public string Description { get; set; }

        [DataMember(Name = "mcVersion")]
        public string McVersion { get; set; }

        [DataMember(Name = "loaderName")]
        public string LoaderName { get; set; }

        [DataMember(Name = "loaderVersion")]
        public string LoaderVersion { get; set; }

        [DataMember(Name = "versionId")]
        public string VersionId { get; set; }

        [DataMember(Name = "simpleMode")]
        public bool SimpleMode { get; set; }

        [DataMember(Name = "minecraftDirectory")]
        public string MinecraftDirectory { get; set; }

        [DataMember(Name = "javaExecutable")]
        public string JavaExecutable { get; set; }

        /// <summary>v3 格式：安装器落盘的待装 mrpack 绝对路径。启动器首启据此
        /// 联网补全 mod / Minecraft 本体 / Java，成功后清空该字段。</summary>
        [DataMember(Name = "pendingPayload", EmitDefaultValue = false)]
        public string PendingPayload { get; set; }

        [DataMember(Name = "updateLink")]
        public string UpdateLink { get; set; }
    }
}
