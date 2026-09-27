using System;
using System.Diagnostics;
using System.IO;
using System.Reflection;
using System.Windows;

namespace NekoSolo.Installer
{
    public partial class App : Application
    {
        /// <summary>
        /// 依赖 DLL 的嵌入资源名前缀（对应 csproj 的 EmbedRuntimeDependencies 目标，
        /// Link 为 deps\程序集名.dll → "NekoSolo.Installer.deps.程序集名.dll"）。
        /// </summary>
        private const string DependencyResourcePrefix = "NekoSolo.Installer.deps.";

        /// <summary>允许自解压加载的依赖程序集白名单（其余解析失败走默认行为）。</summary>
        private static readonly string[] EmbeddedDependencies =
        {
            "MaterialDesignThemes.Wpf",
            "MaterialDesignColors",
            "Microsoft.Xaml.Behaviors",
        };

        /// <summary>
        /// 静态构造器先于 Main 里的任何 WPF 调用执行。
        /// 1. 强制软件渲染：虚拟机 / 基础显示适配器 / 远程会话上 WPF 的硬件
        ///    渲染管线可能初始化失败，症状是整个窗口白屏（控件都在、就是不画）。
        ///    安装器不需要 GPU 加速，SoftwareOnly 换取在一切机器上必然出画面。
        /// 2. 注册依赖 DLL 自解压：单文件安装包旁边没有依赖 DLL，MaterialDesign
        ///    探测失败时从嵌入资源恢复（Assembly.Load(byte[])）。
        /// </summary>
        static App()
        {
            System.Windows.Media.RenderOptions.ProcessRenderMode =
                System.Windows.Interop.RenderMode.SoftwareOnly;
            AppDomain.CurrentDomain.AssemblyResolve += OnAssemblyResolve;
        }

        private static Assembly OnAssemblyResolve(object sender, ResolveEventArgs args)
        {
            string simpleName;
            try
            {
                simpleName = new AssemblyName(args.Name).Name;
            }
            catch
            {
                return null;
            }

            bool whitelisted = false;
            foreach (var candidate in EmbeddedDependencies)
            {
                if (string.Equals(candidate, simpleName, StringComparison.OrdinalIgnoreCase))
                {
                    whitelisted = true;
                    break;
                }
            }
            if (!whitelisted) return null;

            string resource = DependencyResourcePrefix + simpleName + ".dll";
            Assembly executing = typeof(App).Assembly;
            using (Stream stream = executing.GetManifestResourceStream(resource))
            {
                if (stream == null)
                {
                    Trace.WriteLine("依赖嵌入资源缺失：" + resource);
                    return null;
                }
                using (var memory = new MemoryStream())
                {
                    stream.CopyTo(memory);
                    return Assembly.Load(memory.ToArray());
                }
            }
        }
    }
}
