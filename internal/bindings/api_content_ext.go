package bindings

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"nekolauncher/internal/config"
	"nekolauncher/internal/content"
	"nekolauncher/internal/instance"
	"nekolauncher/internal/launch"
)

// renameToDisabled 把文件重命名为 "<file>.disabled"（幂等）；
// 只重命名，绝不删除——用户随时能手动改回来。
func renameToDisabled(path string) error {
	if strings.HasSuffix(strings.ToLower(path), ".disabled") {
		return nil
	}
	return os.Rename(path, path+".disabled")
}

// ToggleContentEntry 启用/禁用内容条目（对应 C# ContentEntryItem 的启停语义：
// 目录内加/去 .disabled 后缀 rename）。entryPath 必须是已存在的文件；
// disable=true 追加 .disabled，disable=false 去掉 .disabled。
func (c *ContentAPI) ToggleContentEntry(entryPath string, disable bool) error {
	if strings.TrimSpace(entryPath) == "" {
		return fmt.Errorf("内容路径为空")
	}
	abs, err := filepath.Abs(entryPath)
	if err != nil {
		return fmt.Errorf("路径无效: %w", err)
	}
	if _, err := os.Stat(abs); err != nil {
		return fmt.Errorf("内容文件不存在: %w", err)
	}
	lower := strings.ToLower(abs)
	var target string
	if disable {
		if strings.HasSuffix(lower, ".disabled") {
			return nil // 已是禁用态，幂等
		}
		target = abs + ".disabled"
	} else {
		if !strings.HasSuffix(lower, ".disabled") {
			return nil // 已是启用态，幂等
		}
		target = abs[:len(abs)-len(".disabled")]
	}
	if err := os.Rename(abs, target); err != nil {
		return fmt.Errorf("重命名失败: %w", err)
	}
	return nil
}

// ---- 实例体检 ----

// RunInstanceHealthCheck 对当前选中实例做一次完整体检（Java / 内存 / mod 冲突），
// 返回 0-100 的分数与按严重度排序的待办清单。
//
// 纯只读：不修改任何文件、不启动任何进程。任何一项数据拿不到时相应检查自动跳过，
// 绝不把"不知道"当成"有问题"（否则用户会被一堆无从下手的告警淹没）。
func (a *ContentAPI) RunInstanceHealthCheck() content.InstanceHealth {
	snapshot := instance.CurrentSnapshot()
	contentDirectory := instance.GameVersionIsolationGetContentDirectory(
		snapshot, snapshot.SelectedVersionId)
	if contentDirectory == "" {
		contentDirectory = snapshot.MinecraftDirectory
	}

	input := content.HealthInput{
		ModsDirectory: content.ModsDirectoryFor(contentDirectory),
		DisabledModCount: content.CountDisabledMods(
			content.ModsDirectoryFor(contentDirectory)),
	}

	// 版本 / 加载器 / Java 要求：尽力解析，失败就留空（检测侧会跳过）
	if snapshot.SelectedVersionId != "" {
		if details, err := instance.LoadDetails(callCtx(a.ctx), snapshot,
			snapshot.SelectedVersionId); err == nil {
			gameVersion := strings.TrimSpace(details.BaseGameVersion)
			switch gameVersion {
			case "未识别", "未知", "未提供":
				gameVersion = ""
			}
			input.MinecraftVersion = gameVersion
			input.LoaderName = normalizeLoaderName(details.LoaderName)
			input.RequiredJavaMajor = content.DetectRequiredJavaMajor(details.JavaRequirement)
		}
	}

	// 当前 Java：取该实例档案里配置的可执行文件，探测其主版本
	profile := config.Get(snapshot.MinecraftDirectory, snapshot.SelectedVersionId)
	javaExecutable := strings.TrimSpace(profile.JavaExecutable)
	if javaExecutable == "" {
		javaExecutable = config.JavaExecutable()
	}
	if version := launch.TryDetectJavaMajorVersion(javaExecutable); version != nil {
		input.CurrentJavaMajor = *version
	}

	// 内存 / 物理内存
	if profile.UseIndependentMemorySettings {
		input.ConfiguredMaximumMemoryMb = profile.MaximumMemoryMb
	} else {
		input.ConfiguredMaximumMemoryMb = launch.GameMemorySettings.ManualMaximumMemoryMb()
	}
	input.SystemTotalMemoryMb = launch.GetSystemMemory().TotalMemoryMb

	return content.RunInstanceHealth(input)
}

// FixDuplicateMods 处理重复 mod：同一 mod id 只保留一个 jar，其余加 .disabled。
//
// keepNewest=true 时保留版本号最高的那个（"我装新版忘了删旧版"是最常见场景）；
// 否则保留文件名排序最后的那个。返回实际被禁用的文件路径。
//
// 只做重命名，**不删除任何文件**——用户随时可以手动改回来。
func (a *ContentAPI) FixDuplicateMods(keepNewest bool) ([]string, error) {
	snapshot := instance.CurrentSnapshot()
	contentDirectory := instance.GameVersionIsolationGetContentDirectory(
		snapshot, snapshot.SelectedVersionId)
	if contentDirectory == "" {
		contentDirectory = snapshot.MinecraftDirectory
	}
	modsDirectory := content.ModsDirectoryFor(contentDirectory)

	disabled, err := content.ResolveDuplicateMods(modsDirectory, keepNewest)
	if err != nil {
		return nil, err
	}
	fixed := make([]string, 0, len(disabled))
	for _, path := range disabled {
		if renameErr := renameToDisabled(path); renameErr != nil {
			// 单个失败不中断：其余重复项仍应被处理
			continue
		}
		fixed = append(fixed, path)
	}
	return fixed, nil
}
