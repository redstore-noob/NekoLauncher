package launch

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLiveLaunchPlanRealVersion 真实安装版本上的"启动前预检"：加载版本 JSON →
// 解析继承链 → 过滤依赖库 → 拼装最终命令行，并逐一确认 classpath / 客户端 jar /
// natives 归档在磁盘上真实存在。
//
// 这是在不真正拉起游戏进程的前提下，对启动管线最接近端到端的验证：
// 能拼出一条所有条目都存在的命令行，说明"点启动能不能出画面"只剩 JVM 与账号两件事。
//
// 默认跳过（CI 上不该依赖本机安装的游戏），需要时显式指定：
//
//	NEKO_LIVE_MINECRAFT="C:\Users\me\AppData\Roaming\.minecraft" \
//	NEKO_LIVE_VERSION="fabric-loader-0.19.5-1.21.1" \
//	go test ./internal/launch/ -run TestLiveLaunchPlanRealVersion -v
func TestLiveLaunchPlanRealVersion(t *testing.T) {
	minecraftDirectory := strings.TrimSpace(os.Getenv("NEKO_LIVE_MINECRAFT"))
	versionID := strings.TrimSpace(os.Getenv("NEKO_LIVE_VERSION"))
	if minecraftDirectory == "" || versionID == "" {
		t.Skip("未设置 NEKO_LIVE_MINECRAFT / NEKO_LIVE_VERSION，跳过真实版本预检")
	}

	ctx := context.Background()

	profile, err := (MinecraftVersionProfileLoader{}).Load(ctx, minecraftDirectory, versionID)
	if err != nil {
		t.Fatalf("加载版本档案失败：%v", err)
	}
	if profile.MainClass == "" {
		t.Fatal("版本档案没有主类，启动参数无从拼起")
	}

	// 依赖解析：缺库文件时这一步就会失败（这正是启动前自检的价值）
	resolved, err := (MinecraftLibraryResolver{}).Resolve(ctx, profile, minecraftDirectory, map[string]bool{})
	if err != nil {
		t.Fatalf("依赖库解析失败：%v", err)
	}

	for _, entry := range resolved.Classpath {
		if _, err := os.Stat(entry); err != nil {
			t.Errorf("classpath 条目不存在：%s（%v）", entry, err)
		}
	}
	for _, native := range resolved.Natives {
		if _, err := os.Stat(native.ArchivePath); err != nil {
			t.Errorf("natives 归档不存在：%s（%v）", native.ArchivePath, err)
		}
	}

	// 客户端 JAR：显式 jar 字段优先，其次取提供 client 下载的链节点
	jarID := profile.ClientJarVersionId
	if jarID == "" {
		jarID = versionID
	}
	clientJar := filepath.Join(minecraftDirectory, "versions", jarID, jarID+".jar")
	if _, err := os.Stat(clientJar); err != nil {
		t.Errorf("客户端 JAR 不存在：%s（%v）", clientJar, err)
	}

	assetIndex := filepath.Join(minecraftDirectory, "assets", "indexes", profile.AssetsId+".json")
	if _, err := os.Stat(assetIndex); err != nil {
		t.Errorf("资源索引不存在：%s（%v）", assetIndex, err)
	}

	options := MinecraftLaunchOptions{
		MinecraftDirectory: minecraftDirectory,
		GameDirectory:      minecraftDirectory,
		VersionId:          versionID,
		Account:            MustOfflineAccount("NekoAudit"),
		MinimumMemoryMb:    512,
		MaximumMemoryMb:    2048,
		LauncherName:       "NekoLauncher",
		LauncherVersion:    "live-precheck",
	}

	arguments, err := (MinecraftArgumentBuilder{}).Build(
		profile, options, t.TempDir(), resolved.Classpath, profile.MainClass, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("拼装启动参数失败：%v", err)
	}

	joined := strings.Join(arguments, " ")
	for _, required := range []string{
		"-cp", profile.MainClass, "--username", "NekoAudit", "--accessToken",
		"--gameDir", "--assetsDir", "-Djava.library.path",
	} {
		if !strings.Contains(joined, required) {
			t.Errorf("命令行缺少 %q：%s", required, joined)
		}
	}
	if strings.Contains(joined, "${") {
		t.Errorf("命令行里还有未替换的占位符：%s", joined)
	}
	// 离线账号没有真实令牌，但必须有个非空值，否则客户端会以"认证失败"退出
	if strings.Contains(joined, "--accessToken --") {
		t.Errorf("--accessToken 没有取到值：%s", joined)
	}

	t.Logf("版本 %s：主类 %s，classpath %d 项，natives %d 个，参数 %d 项，资源索引 %s",
		versionID, profile.MainClass, len(resolved.Classpath), len(resolved.Natives),
		len(arguments), profile.AssetsId)

	// 日志配置（log4j2）：真实版本应当声明它；配置在本机时命令行必须带上
	// -Dlog4j.configurationFile=<path>，不在本机时应当**跳过**该参数（不指空文件）。
	if profile.LoggingFileId == "" {
		// 已被旧版扁平化处理过的实例：logging 段随原版 JSON 一起消失，本地拿不回来
		// （修复路径在 download.EnsureLoggingConfigFromMetadataURL，按 clientVersion 取回）。
		if raw, err := os.ReadFile(filepath.Join(minecraftDirectory, "versions", versionID, versionID+".json")); err == nil &&
			strings.Contains(string(raw), `"clientVersion"`) {
			t.Logf("版本 %s 已被扁平化，本地没有 logging 段（属已知情况）", versionID)
		} else {
			t.Errorf("版本 %s 的 logging 段没有被解析出来", versionID)
		}
	} else {
		logConfigPath := filepath.Join(minecraftDirectory, "assets", "log_configs", profile.LoggingFileId)
		_, statErr := os.Stat(logConfigPath)
		hasArgument := strings.Contains(joined, "-Dlog4j.configurationFile="+logConfigPath)

		t.Logf("日志配置 %s：本机存在=%v，命令行下发=%v", profile.LoggingFileId, statErr == nil, hasArgument)
		if statErr == nil && !hasArgument {
			t.Errorf("日志配置已存在却没下发参数：%s", logConfigPath)
		}
		if statErr != nil && hasArgument {
			t.Errorf("日志配置不存在却下发了参数（会让 log4j 报错）：%s", logConfigPath)
		}
	}
}
