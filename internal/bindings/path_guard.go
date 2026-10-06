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
	"strings"

	"nekolauncher/internal/config"
	"nekolauncher/internal/instance"
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
