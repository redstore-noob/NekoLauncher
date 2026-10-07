package bindings

// InstanceAPI 扩展：删除实例。与 api_instance.go 分离存放避免冲突。

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"nekolauncher/internal/config"
	"nekolauncher/internal/instance"
)

// errEmptyPath 路径为空。
var errEmptyPath = errors.New("path is empty")

// errInvalidInstancePath 路径校验失败（不在 versions/ 下或含非法片段）。
var errInvalidInstancePath = errors.New("instance path is outside the game versions directory")

// DeleteInstance 删除实例：即删除 <gameDirectory>/versions/<instanceID> 目录。
// internal/instance、internal/launch 无现成的实例删除入口（launch.TryDeleteDirectory
// 是无校验的尽力删除，不适合直接暴露），故在此实现并先做路径校验防误删：
// instanceID 不得含路径分隔符，且最终路径必须严格位于 versions/ 目录内。
func (a *InstanceAPI) DeleteInstance(instanceID, gameDirectory string) error {
	if instanceID == "" || gameDirectory == "" {
		return errEmptyPath
	}
	if strings.ContainsAny(instanceID, "/\\") || instanceID == "." || instanceID == ".." {
		return errInvalidInstancePath
	}
	versionsDir := filepath.Join(gameDirectory, "versions")
	target := filepath.Join(versionsDir, instanceID)

	// 双重校验：target 与 versionsDir 都取绝对+Clean 后，target 必须在 versionsDir 内。
	absVersions, err := filepath.Abs(versionsDir)
	if err != nil {
		return err
	}
	absTarget, err := filepath.Abs(target)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(absVersions, absTarget)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return errInvalidInstancePath
	}
	var info os.FileInfo
	if stat, err := os.Stat(absTarget); err == nil {
		info = stat
	} else {
		// 注意：不要在这里因为 Stat 报 NotExist 就放弃。Windows 会把目录名
		// 尾部的空格/点在常规路径 API 里剥掉（如 "1.20.1 "），导致 Stat 误报
		// 不存在、资源管理器也删不掉——这正是清不掉的残留。改用 \\?\ 路径
		// 做存在性检查，且删除保持幂等：本就不存在即视为成功。
		extInfo, extErr := os.Stat(extendedPath(absTarget))
		if extErr != nil {
			if os.IsNotExist(extErr) {
				return nil
			}
			return extErr
		}
		info = extInfo
	}
	if !info.IsDir() {
		return errInvalidInstancePath
	}
	return removeAllForce(absTarget)
}

// removeAllForce 删除目录树，尽力保证删干净。Windows 上常见的残留原因逐个处理：
//   - 只读属性：其它启动器导入的整合包常带只读文件/目录，os.RemoveAll 会失败；
//     删除前先递归清掉只读位（不是失败后才清）。
//   - 路径超过 MAX_PATH（260）：.minecraft 目录嵌套很深，用 \\?\ 前缀绕过。
//   - 瞬时占用：杀毒/索引/资源管理器短暂握住句柄，用带退避的重试吸收。
//
// 重试耗尽后若目录仍存在，返回带首个残留路径的错误，便于用户定位占用者。
func removeAllForce(root string) error {
	extRoot := extendedPath(root)

	// 预处理：清只读。第一次 RemoveAll 之前就做，避免「删了一半卡在只读文件」。
	clearReadOnly(extRoot)

	var lastErr error
	for attempt := 0; attempt < removeMaxAttempts; attempt++ {
		if err := os.RemoveAll(extRoot); err == nil {
			// RemoveAll 返回 nil 时目录已不存在
			return nil
		} else {
			lastErr = err
		}
		// 失败后再次清只读（可能有新暴露出的层级），等一小会儿再试
		clearReadOnly(extRoot)
		time.Sleep(time.Duration(1<<uint(attempt)) * 100 * time.Millisecond)
	}

	// 仍失败：找出第一个删不掉的条目，附在错误里
	if leftover, findErr := firstExisting(extRoot); findErr == nil && leftover != "" {
		return fmt.Errorf("%w: %s", lastErr, leftover)
	}
	return lastErr
}

const removeMaxAttempts = 5

// extendedPath 返回绕过 Windows MAX_PATH 限制的路径形式。
// 普通盘符路径加 \\?\ 前缀；UNC 路径转为 \\?\UNC\...；非 Windows 原样返回。
func extendedPath(path string) string {
	if runtime.GOOS != "windows" {
		return path
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	abs = filepath.Clean(abs)
	if strings.HasPrefix(abs, `\\?\`) {
		return abs
	}
	if strings.HasPrefix(abs, `\\`) {
		return `\\?\UNC\` + strings.TrimPrefix(abs, `\\`)
	}
	return `\\?\` + abs
}

// clearReadOnly 递归移除 root 下所有文件与目录的只读位，忽略个别失败。
func clearReadOnly(root string) {
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if !d.Type().IsRegular() && !d.IsDir() {
			return nil
		}
		if info, statErr := d.Info(); statErr == nil && info.Mode()&0o222 == 0 {
			_ = os.Chmod(path, info.Mode()|0o222)
		}
		return nil
	})
}

// firstExisting 返回 root 下仍然存在的第一个条目路径；root 本身已不在则返回空串。
func firstExisting(root string) (string, error) {
	info, err := os.Stat(root)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return root, nil
	}
	if !info.IsDir() {
		return root, nil
	}
	found := ""
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil && found == "" {
			found = path
			return filepath.SkipAll
		}
		return nil
	})
	if found == "" {
		// 目录本身无法删除（被当作句柄占用等）
		return root, nil
	}
	return found, nil
}

// ScanImportableInstances 扫描 Prism/MultiMC/CurseForge/Modrinth/ATLauncher 等
// 其它启动器的实例，返回可一键注册为游戏目录的列表（不复制文件）。
func (a *InstanceAPI) ScanImportableInstances() []instance.ImportableInstance {
	return instance.ScanImportableInstances(config.GetFolders())
}

// RegisterImportedInstance 注册（不复制文件）自动探测到的外部实例目录：
// 加入游戏目录列表并切换为当前目录。路径必须在 Go 侧验明确实是探测根下的
// 外部实例（instance.IsImportCandidateInstance）——AddProfileFolder 已收紧为
// "对话框批准或已注册目录"，导入列表的路径来自自动探测而非对话框，
// 走这条专用通道才能既不破坏导入功能又不放开任意注册。
func (a *InstanceAPI) RegisterImportedInstance(path string) error {
	if !instance.IsImportCandidateInstance(path) {
		return errors.New("该路径不是探测到的外部启动器实例目录")
	}
	if !config.AddFolder(path) {
		return errors.New("注册失败：路径无效或已在目录列表中")
	}
	if !config.SaveGameDirectory(path) {
		return errors.New("切换当前游戏目录失败")
	}
	return nil
}
