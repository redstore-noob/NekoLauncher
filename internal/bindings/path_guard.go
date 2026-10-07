package bindings

// 读类绑定的路径收口。
//
// 背景：有几种"读"本身不写盘，却能把用户机器上的目录结构翻出来——最典型的是
// ReadSaves：它按设计只接收某个存档目录，但绑定层不校验时，任何调用方（含插件 API）
// 传一个任意可读目录就能列出其全部子目录名与路径。内容读不到，目录结构本身就是隐私。
//
// 收口口径：只认"已知游戏根目录"之内的路径——当前实例快照给出的几个根，加上用户
// 在设置里额外扫描的游戏目录（多启动器 / 独立实例都在这里）。根目录之外的路径一律
// 当作"读不到"处理。

import (
	"errors"
	"path/filepath"
	"strings"
	"sync"

	"nekolauncher/internal/config"
	"nekolauncher/internal/instance"
	"nekolauncher/internal/logs"
)

// gameRootDirectories 已知的游戏根目录：当前实例快照的三个根 + 用户额外扫描的目录。
// 存档 / 内容目录都应当是它们的子目录。
func gameRootDirectories() []string {
	snapshot := instance.CurrentSnapshot()
	roots := []string{
		snapshot.MinecraftDirectory,
		snapshot.GameDirectory,
		snapshot.SourcePath,
	}

	return append(roots, config.GetFolders()...)
}

// insideAnyRoot 判定 path 是否落在 roots 中的某一个之内（含根本身）。
// 空路径、空根一律不算命中——宁可不给结果，也不把"没配置"当成"全都允许"。
func insideAnyRoot(roots []string, path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	for _, root := range roots {
		if strings.TrimSpace(root) == "" {
			continue
		}
		if isSameOrInside(root, path) {
			return true
		}
	}

	return false
}

// insideKnownGameRoot 判定 path 是否落在已知游戏根目录内。
func insideKnownGameRoot(path string) bool {
	return insideAnyRoot(gameRootDirectories(), path)
}

// readableRootDirectories SystemAPI 读类绑定（ReadTextFile / ListDirectory）
// 的收口根：已知游戏根目录 + 音乐库目录（歌词 .lrc 与封面读取要用）。
// 这两个绑定此前不设限——插件可直呼它们读到 accounts.yaml 与
// account.secret.key（后者是非 Windows 平台的 AES 密钥，读到即解密全部
// 账号凭据），等于把账号出口的全部脱敏努力旁路掉。
func readableRootDirectories() []string {
	roots := gameRootDirectories()
	// 音乐库目录（歌词 .lrc 与封面读取要用）；与 MusicLibrary 同一配置键
	if folder := config.GetValue("musicFolder"); strings.TrimSpace(folder) != "" {
		roots = append(roots, folder)
	}
	return roots
}

// resolvedPath 解析符号链接/junction 后的真实路径；路径不存在时
// （EvalSymlinks 会失败）回落原值，调用方的判定按 fail-closed 处理。
// 根目录内部的链接可能指向根外（mod 包/junction 场景常见），字符串级
// 前缀比对会放行，必须按解析后的真实路径判定。
func resolvedPath(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	return path
}

// resolvedInsideStorage 解析后的目标路径是否落在启动器存储目录
// （accounts.yaml / 密钥 / 日志的家）内。两条路径都做符号链接解析，
// 防止字面差异（尾随点/空格替身、链接绕行）骗过字符串比对。
func resolvedInsideStorage(path string) bool {
	storage := config.DefaultStorageDirectory()
	if strings.TrimSpace(storage) == "" {
		return false
	}
	return isSameOrInside(resolvedPath(storage), resolvedPath(path))
}

// rootContainsStorage 判定拟新增的读根是否包含启动器存储目录。
// 含存储目录的根（如整个用户主目录）会让"存储目录拒绝"形同虚设，
// 在写入侧就该拒绝。
func rootContainsStorage(root string) bool {
	storage := config.DefaultStorageDirectory()
	if strings.TrimSpace(storage) == "" {
		return false
	}
	return isSameOrInside(resolvedPath(root), resolvedPath(storage))
}

// dialogApprovedDirectories 用户经目录选择对话框亲自选中的目录（包级，
// SystemAPI 记录、ConfigAPI/MusicAPI 校验）。设置游戏根/音乐目录的
// 守卫依据它判定"用户知情同意了这个区域"——插件与宿主同 WebView，
// 无法区分调用方，对话框批准是唯一可信的用户同意信号。
var dialogApprovedDirectories struct {
	mu   sync.Mutex
	dirs []string
}

// approveDialogDirectory 记录一次目录选择对话框的结果。
func approveDialogDirectory(directory string) {
	directory = filepath.Clean(strings.TrimSpace(directory))
	if directory == "" {
		return
	}
	dialogApprovedDirectories.mu.Lock()
	dialogApprovedDirectories.dirs = append(dialogApprovedDirectories.dirs, directory)
	dialogApprovedDirectories.mu.Unlock()
}

// isDialogApprovedDirectory path 是否等于（或位于）某个对话框批准目录内。
// 子目录放行是为导入扫描器准备的：用户批准了探测根，扫描出的实例目录
// 是其子目录。比对按符号链接解析后的真实路径。
func isDialogApprovedDirectory(path string) bool {
	dialogApprovedDirectories.mu.Lock()
	dirs := append([]string(nil), dialogApprovedDirectories.dirs...)
	dialogApprovedDirectories.mu.Unlock()
	for _, dir := range dirs {
		if isSameOrInside(resolvedPath(dir), resolvedPath(path)) {
			return true
		}
	}
	return false
}

// guardSettableRoot 校验"读收口根"的新设值：只允许两类路径——
//  1. 已注册的游戏目录（在已选根之间切换）；
//  2. 用户刚在目录对话框里选中的目录（含其子目录，供导入扫描注册用）。
//
// 此前 AddProfileFolder/SaveGameDirectory/SetMusicFolder 可被插件静默
// 调用注册任意目录，把读收口扩大到全盘。
func guardSettableRoot(path string) error {
	cleaned := filepath.Clean(strings.TrimSpace(path))
	if cleaned == "" {
		return errors.New("路径为空")
	}
	for _, folder := range config.GetFolders() {
		if isSameOrInside(folder, cleaned) && isSameOrInside(cleaned, folder) {
			return nil
		}
	}
	if isDialogApprovedDirectory(cleaned) {
		return nil
	}
	logs.Write("WARN", "已拒绝设置未经用户选择的读根目录："+cleaned)
	return errors.New("拒绝设置：请通过目录选择对话框选择该目录")
}

// guardReadablePath 校验读类绑定的目标路径：必须在收口根之内，
// 且按符号链接解析后的真实路径绝不落在启动器存储目录内——
// 即使用户（或冒充用户的插件）把游戏根指到了存储目录里也一样拒绝。
func guardReadablePath(path string) error {
	if insideAnyRoot(readableRootDirectories(), path) && !resolvedInsideStorage(path) {
		return nil
	}
	logs.Write("WARN", "已拒绝界面层读取收口目录之外的路径："+path)
	return errors.New("拒绝读取：该路径不在已知的游戏或音乐目录内")
}
