/*
 * 安装器主窗口：三步向导（欢迎 → 安装 → 完成）。
 * 启动时解析自身尾标定位载荷；整合包图标解压到临时文件展示。
 * 完成页可勾选"立即启动"——Process.Start 拉起 NekoLauncher.exe，
 * 首启由启动器内部的 solo 标记接管（S 模式 / 实例选中 / 捆绑 Java）。
 */
using System;
using System.Diagnostics;
using System.IO;
using System.Windows;
using System.Windows.Controls;
using System.Windows.Media.Imaging;

namespace NekoSolo.Installer
{
    public partial class MainWindow : Window
    {
        private IPayloadPackage _payload;
        private SoloManifest _manifest;
        private bool _installing;
        private bool _finished;

        public MainWindow()
        {
            InitializeComponent();
            Loaded += OnLoaded;
        }

        private void OnLoaded(object sender, RoutedEventArgs e)
        {
            // NEKOSOLO_DEV=1：开发诊断模式，跳过载荷解析直接展示界面
            if (Environment.GetEnvironmentVariable("NEKOSOLO_DEV") == "1")
            {
                _payload = null;
            }
            else
            {
                try
                {
                    _payload = SoloPayload.Open(Process.GetCurrentProcess().MainModule.FileName);
                }
                catch (Exception ex)
                {
                    MessageBox.Show(this, ex.Message, "无法打开安装包", MessageBoxButton.OK, MessageBoxImage.Error);
                    Application.Current.Shutdown(1);
                    return;
                }
            }

            _manifest = _payload == null
                ? new SoloManifest
                {
                    Format = 1,
                    PackId = "dev-pack",
                    PackName = "DEV 模式整合包",
                    PackVersion = "0.0.0-dev",
                    Author = "作者喵",
                    McVersion = "1.20.1",
                    LoaderName = "Forge",
                    LoaderVersion = "47.2.0",
                    VersionId = "dev",
                    SimpleMode = true,
                    HasJava = true,
                }
                : _payload.Manifest;
            Title = "安装 " + _manifest.PackName + " - NekoSolo";
            PackTitle.Text = _manifest.PackName;
            PackVersionText.Text = string.IsNullOrEmpty(_manifest.PackVersion) ? "" : "v" + _manifest.PackVersion;
            PackAuthorText.Text = string.IsNullOrEmpty(_manifest.Author) ? "" : "by " + _manifest.Author;
            PackMcBadge.Text = string.IsNullOrEmpty(_manifest.McVersion)
                ? "Minecraft"
                : "Minecraft " + _manifest.McVersion;
            if (!string.IsNullOrEmpty(_manifest.LoaderName))
            {
                PackLoaderText.Text = _manifest.LoaderName +
                    (string.IsNullOrEmpty(_manifest.LoaderVersion) ? "" : " " + _manifest.LoaderVersion);
                PackLoaderBadge.Visibility = Visibility.Visible;
            }
            PackDescription.Text = string.IsNullOrEmpty(_manifest.Description)
                ? "作者没有留下描述。"
                : _manifest.Description;

            ShowIcon(_manifest);
            RefreshPackStats();
            InstallDirBox.Text = DefaultInstallDirectory(_manifest);
            InstallDirBox.TextChanged += OnInstallDirChanged;
            RefreshModeHints();
        }

        /// <summary>
        /// 从载荷读取内容概览：modrinth.index.json 声明的 mod 数与下载体积、
        /// overrides/ 随包文件数与体积。读取失败一律静默（概览是加分项）。
        /// </summary>
        private void RefreshPackStats()
        {
            if (_payload == null || _payload is RemotePayload) return; // 远程载荷未下载，无概览
            try
            {
                int modCount = 0;
                long downloadBytes = 0;
                using (var zip = _payload.OpenZip())
                {
                    int overrideCount = 0;
                    long overrideBytes = 0;
                    foreach (var entry in zip.Entries)
                    {
                        string name = entry.FullName.Replace('\\', '/');
                        if (name.EndsWith("/", StringComparison.Ordinal)) continue;
                        if (name.Equals(InstallerEngine.ModrinthIndexEntry, StringComparison.OrdinalIgnoreCase))
                        {
                            using (var stream = entry.Open())
                            using (var reader = new StreamReader(stream))
                            {
                                var serializer = new System.Runtime.Serialization.Json.DataContractJsonSerializer(typeof(ModrinthIndex));
                                if (serializer.ReadObject(reader.BaseStream) is ModrinthIndex index && index.Files != null)
                                {
                                    foreach (var file in index.Files)
                                    {
                                        modCount++;
                                        downloadBytes += file.FileSize;
                                    }
                                }
                            }
                            continue;
                        }
                        if (name.StartsWith("overrides/", StringComparison.OrdinalIgnoreCase) ||
                            name.StartsWith("client-overrides/", StringComparison.OrdinalIgnoreCase))
                        {
                            overrideCount++;
                            overrideBytes += entry.Length;
                        }
                    }
                    if (modCount == 0 && overrideCount == 0) return;
                    StatModsValue.Text = modCount.ToString();
                    StatModsSize.Text = modCount > 0 ? string.Format("约 {0:0.#} MB（首次启动时下载）", downloadBytes / 1048576.0) : "";
                    StatOverridesValue.Text = overrideCount.ToString();
                    StatOverridesSize.Text = overrideCount > 0 ? string.Format("共 {0:0.#} MB", overrideBytes / 1048576.0) : "";
                    StatsRow.Visibility = Visibility.Visible;
                }
            }
            catch
            {
                // 概览读不出来不影响安装
            }
        }

        /// <summary>安装目录变化时刷新模式判定与文案（新增整合包 / 更新 / 全新安装）。</summary>
        private void RefreshModeHints()
        {
            string root;
            try { root = Path.GetFullPath(InstallDirBox.Text.Trim()); }
            catch { return; }
            if (string.IsNullOrWhiteSpace(root)) return;

            // v2 在线安装包：提示安装时需要联网下载整合包内容
            if (_manifest.IsRemote)
            {
                string sizeText = _manifest.PayloadSize > 0
                    ? string.Format("（约 {0:0.#} MB）", _manifest.PayloadSize / 1048576.0)
                    : "";
                SoloHint.Text = "本安装包为在线安装：安装时需要联网下载整合包内容" + sizeText +
                    "，下载完成后自动校验并安装。";
            }

            switch (InstallerEngine.DetectInstallMode(root))
            {
                case InstallMode.AddPack:
                    SoloHint.Text = "检测到已有 NekoLauncher：不会覆盖启动器本体与任何配置，只安装新的整合包实例并自动选中。";
                    InstallButton.Content = "添加整合包";
                    break;
                case InstallMode.UpdatePack:
                    SoloHint.Text = "检测到本整合包已安装：将更新到本版本，存档与设置保留，启动器本体不变。";
                    InstallButton.Content = "更新整合包";
                    break;
                default:
                    SoloHint.Text = "将安装 NekoLauncher 启动器与整合包实例，安装完成后即可打开启动器。";
                    InstallButton.Content = "安装";
                    break;
            }

            // UpdatePack 的专用提示已并入 SoloHint
            UpdateHint.Visibility = Visibility.Collapsed;
        }

        private void ShowIcon(SoloManifest manifest)
        {
            if (string.IsNullOrEmpty(manifest.IconPath)) return;
            // 在线安装包：图标在载荷里，尚未下载，欢迎页用占位图标
            if (_payload is RemotePayload) return;
            try
            {
                string temp = InstallerEngine.ExtractIconToTemp(_payload);
                if (temp == null) return;
                var image = new BitmapImage();
                image.BeginInit();
                image.CacheOption = BitmapCacheOption.OnLoad;
                image.UriSource = new Uri(temp);
                image.EndInit();
                image.Freeze();
                // OnLoad 已把像素读进内存，临时文件立刻可删
                try { File.Delete(temp); } catch { }
                PackIcon.Source = image;
                PackIcon.Visibility = Visibility.Visible;
                PackIconFallback.Visibility = Visibility.Collapsed;
            }
            catch
            {
                // 图标坏了不影响安装
            }
        }

        private static string DefaultInstallDirectory(SoloManifest manifest)
        {
            string userProfile = Environment.GetFolderPath(Environment.SpecialFolder.UserProfile);
            string folder = string.IsNullOrEmpty(manifest.PackId) ? "NekoLauncher-Solo" : manifest.PackId;
            return Path.Combine(userProfile, "NekoLauncher-Solo", folder);
        }

        private void OnInstallDirChanged(object sender, TextChangedEventArgs e)
        {
            RefreshModeHints();
        }

        private void OnBrowse(object sender, RoutedEventArgs e)
        {
            using (var dialog = new System.Windows.Forms.FolderBrowserDialog())
            {
                dialog.Description = "选择安装位置";
                dialog.ShowNewFolderButton = true;
                if (dialog.ShowDialog() == System.Windows.Forms.DialogResult.OK)
                {
                    InstallDirBox.Text = dialog.SelectedPath;
                }
            }
        }

        private async void OnInstall(object sender, RoutedEventArgs e)
        {
            string root;
            try
            {
                root = Path.GetFullPath(InstallDirBox.Text.Trim());
            }
            catch
            {
                MessageBox.Show(this, "安装目录无效。", "NekoSolo", MessageBoxButton.OK, MessageBoxImage.Warning);
                return;
            }
            if (string.IsNullOrWhiteSpace(root))
            {
                MessageBox.Show(this, "请先选择安装目录。", "NekoSolo", MessageBoxButton.OK, MessageBoxImage.Warning);
                return;
            }

            InstallButton.IsEnabled = false;
            ExitButton.IsEnabled = false;
            StepWelcome.Visibility = Visibility.Collapsed;
            StepInstalling.Visibility = Visibility.Visible;
            _installing = true;
            InstallMode mode = InstallerEngine.DetectInstallMode(root);

            try
            {
                var lastFile = string.Empty;
                await System.Threading.Tasks.Task.Run(() =>
                {
                    int lastPercent = -1;
                    InstallerEngine.Install(_payload, root, progress =>
                    {
                        if (progress.CurrentFile != null) lastFile = progress.CurrentFile;
                        // 按 1% 步进节流 UI 刷新：GB 级载荷逐块 Invoke 会拖慢解压
                        int percent = (int)progress.Percent;
                        if (percent == lastPercent) return;
                        lastPercent = percent;
                        var file = lastFile;
                        Dispatcher.Invoke(() =>
                        {
                            ProgressBar.Value = percent;
                            ProgressFile.Text = file;
                        });
                    });
                });
                DoneHint.Text = mode == InstallMode.UpdatePack
                    ? "整合包已更新到本版本，存档与设置保持不变；首次启动将自动补全更新内容。"
                    : mode == InstallMode.AddPack
                        ? "整合包实例已添加到现有启动器，本体与配置保持不变；下次启动将自动选中新实例并联网补全 mod 与游戏文件。"
                        : "启动器与整合包已就绪；首次启动将自动联网下载 Minecraft、mod 与 Java 运行时（需要网络）。";
                StepInstalling.Visibility = Visibility.Collapsed;
                StepDone.Visibility = Visibility.Visible;
            }
            catch (Exception ex)
            {
                MessageBox.Show(this, "安装失败：" + ex.Message, "NekoSolo", MessageBoxButton.OK, MessageBoxImage.Error);
                StepInstalling.Visibility = Visibility.Collapsed;
                StepWelcome.Visibility = Visibility.Visible;
                _installing = false;
                InstallButton.IsEnabled = true;
                ExitButton.IsEnabled = true;
            }
        }

        private void OnOpenFolder(object sender, RoutedEventArgs e)
        {
            try
            {
                Process.Start("explorer.exe", InstallDirBox.Text.Trim());
            }
            catch
            {
                // 打不开就算了
            }
        }

        /// <summary>把安装包内嵌的 Modrinth 整合包另存为 .mrpack（其他启动器也能导入）。</summary>
        private void OnExtractModpack(object sender, RoutedEventArgs e)
        {
            if (_payload == null)
            {
                MessageBox.Show(this, "当前是开发诊断模式，没有可提取的整合包。", "NekoSolo",
                    MessageBoxButton.OK, MessageBoxImage.Information);
                return;
            }
            var dialog = new Microsoft.Win32.SaveFileDialog
            {
                Title = "提取整合包",
                Filter = "Modrinth 整合包 (*.mrpack)|*.mrpack",
                FileName = (string.IsNullOrEmpty(_manifest.PackId) ? "pack" : _manifest.PackId) + ".mrpack",
            };
            if (dialog.ShowDialog(this) != true) return;
            try
            {
                InstallerEngine.ExtractModpack(_payload, dialog.FileName);
                MessageBox.Show(this, "整合包已提取到：\n" + dialog.FileName, "NekoSolo",
                    MessageBoxButton.OK, MessageBoxImage.Information);
            }
            catch (Exception ex)
            {
                MessageBox.Show(this, "提取失败：" + ex.Message, "NekoSolo", MessageBoxButton.OK, MessageBoxImage.Error);
            }
        }

        private void OnFinish(object sender, RoutedEventArgs e)
        {
            LaunchIfNeeded();
            _finished = true;
            Close();
        }

        private void OnExit(object sender, RoutedEventArgs e)
        {
            _finished = true;
            Close();
        }

        private void LaunchIfNeeded()
        {
            if (LaunchAfterInstall.IsChecked != true) return;
            try
            {
                string launcher = Path.Combine(InstallDirBox.Text.Trim(), InstallerEngine.LauncherFileName);
                if (File.Exists(launcher)) Process.Start(new ProcessStartInfo(launcher) { UseShellExecute = true });
            }
            catch
            {
                // 启动失败不打扰用户，可从安装目录手动启动
            }
        }

        private void OnWindowClosing(object sender, System.ComponentModel.CancelEventArgs e)
        {
            if (_installing && !_finished)
            {
                MessageBox.Show(this, "正在安装，请稍候。", "NekoSolo", MessageBoxButton.OK, MessageBoxImage.Information);
                e.Cancel = true;
            }
        }
    }
}
