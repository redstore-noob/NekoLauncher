// NekoSolo 标记的消费：启动器首启时把安装器写好的 neko-solo.json
// 应用进 launcher.yaml（游戏目录 / 捆绑 Java / 选中实例 / S 模式 / 版本隔离），
// 然后把 Applied 置 true。此后用户在界面里怎么改都不再被覆盖。
package solo

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"nekolauncher/internal/config"
	"nekolauncher/internal/logs"
)

// simpleModeConfigKey launcher.yaml 中 S 模式的键（前端经 ConfigAPI 读写同一键）。
const simpleModeConfigKey = "simpleMode"

// pathsEquivalent 宽松的路径等价比较：清理分隔符后，Windows 忽略大小写。
func pathsEquivalent(left, right string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(filepath.Clean(left), filepath.Clean(right))
	}
	return filepath.Clean(left) == filepath.Clean(right)
}

// ApplyStartupDefaults 消费 NekoSolo 标记。幂等：Applied 已置位或标记不存在时
// 立即返回。任何一步失败都只记日志并继续（应用部分生效好过完全卡死）。
// 返回 error 仅用于调用方记日志，不代表启动失败。
func ApplyStartupDefaults() error {
	storage := config.StorageDirectory()
	marker, err := LoadMarker(storage)
	if err != nil {
		return fmt.Errorf("读取 NekoSolo 标记失败：%w", err)
	}
	if marker == nil || marker.Applied {
		return nil
	}

	// S 模式：NekoSolo 安装的目标用户是低龄玩家，默认精简界面。
	// 只在用户从未配置过该键时开启——"不覆盖配置"原则（多包安装/重装时
	// 用户若已主动关闭 S 模式，不得被翻回开启）
	if marker.SimpleMode && config.GetValue(simpleModeConfigKey) == "" {
		config.SetValue(simpleModeConfigKey, "true")
	}

	// 游戏目录：只在用户还没配置时接管（更新重装时不动用户已迁移的目录）；
	// 用户已有别的游戏目录时，把整合包根目录注册进"游戏目录列表"供切换，
	// 绝不改动当前选中的目录
	if marker.MinecraftDirectory != "" {
		if info, err := os.Stat(marker.MinecraftDirectory); err == nil && info.IsDir() {
			switch {
			case config.GameDirectory() == "":
				if !config.SaveGameDirectory(marker.MinecraftDirectory) {
					logs.Write("WARN", "NekoSolo：游戏目录写入失败："+marker.MinecraftDirectory)
				}
			case !pathsEquivalent(config.GameDirectory(), marker.MinecraftDirectory):
				if !config.AddFolder(marker.MinecraftDirectory) {
					logs.Write("WARN", "NekoSolo：游戏目录列表添加失败："+marker.MinecraftDirectory)
				}
			}
		} else {
			logs.Write("WARN", "NekoSolo：标记里的游戏目录不存在："+marker.MinecraftDirectory)
		}
	}

	// 捆绑 Java：同理只在未配置首选 Java 时注册
	if config.JavaExecutable() == "" && marker.JavaExecutable != "" {
		if info, err := os.Stat(marker.JavaExecutable); err == nil && !info.IsDir() {
			if !config.SaveJava(marker.JavaExecutable, "bundled") {
				logs.Write("WARN", "NekoSolo：捆绑 Java 写入失败："+marker.JavaExecutable)
			}
		} else {
			logs.Write("WARN", "NekoSolo：捆绑 Java 不存在："+marker.JavaExecutable)
		}
	}

	// 选中实例：首启直接停在整合包实例上（扫描的选中优先级里有 saved selection）
	if marker.VersionID != "" {
		config.SetValue("selectedGameInstance", marker.VersionID)
	}

	// 版本隔离：内容就装在 versions/<id>/ 里，显式开启隔离使判定不依赖内容探测
	//（内容为空的整合包也能得到正确的隔离布局）
	if marker.MinecraftDirectory != "" && marker.VersionID != "" {
		profile := config.Get(marker.MinecraftDirectory, marker.VersionID)
		enabled := true
		profile.IsVersionIsolationEnabled = &enabled
		if !config.Save(profile) {
			logs.Write("WARN", "NekoSolo：实例隔离设置写入失败："+marker.VersionID)
		}
	}

	marker.Applied = true
	if err := SaveMarker(storage, marker); err != nil {
		// 应用已生效但标记没能落位：下次启动会重复应用，均为幂等写，无害
		return fmt.Errorf("NekoSolo 标记回写失败：%w", err)
	}
	logs.Write("INFO", "NekoSolo：已应用安装标记（"+marker.PackName+" "+marker.PackVersion+"）")

	// v3 格式：mod / MC 本体 / Java 都不在安装包里，安装器把待装 mrpack 落盘
	// 后由这里触发首启补全（异步进行，不阻塞启动）。失败不清字段——下次启动重试。
	if strings.TrimSpace(marker.PendingPayload) != "" && PendingPayloadHook != nil {
		go func(pending Marker) {
			if err := PendingPayloadHook(pending); err != nil {
				logs.Write("WARN", "NekoSolo：整合包内容补全失败（下次启动将重试）："+err.Error())
				return
			}
			if fresh, err := LoadMarker(storage); err == nil && fresh != nil && fresh.PendingPayload != "" {
				fresh.PendingPayload = ""
				if err := SaveMarker(storage, fresh); err != nil {
					logs.Write("WARN", "NekoSolo：补全标记清理失败："+err.Error())
				}
			}
			logs.Write("INFO", "NekoSolo：整合包内容补全完成（"+pending.PackName+"）")
		}(*marker)
	}
	return nil
}
