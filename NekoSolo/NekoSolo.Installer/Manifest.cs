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

        [DataMember(Name = "updateLink")]
        public string UpdateLink { get; set; }
    }
}
