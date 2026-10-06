// Package world 移植自 NyaLauncher.Core/World/WorldStore.cs：
// 扫描全部实例的 saves 目录，提供最近游玩的世界列表。
package world

import (
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"nekolauncher/internal/instance"
	"nekolauncher/internal/tools"
)

// WorldInfo 表示一个 Minecraft 世界的简要信息。
type WorldInfo struct {
	// Name 世界名（存档目录名）。
	Name string
	// DirectoryPath 存档目录完整路径。
	DirectoryPath string
	// OwnerVersionId 世界所属的游戏实例（版本）Id；一键启动前先切换到该实例。
	OwnerVersionId string
	// LastPlayed 上次游玩时间（level.dat NBT 的 Data.LastPlayed；读不到回落文件修改时间）。
	LastPlayed time.Time
	// IconPath 世界图标 icon.png 的路径（不存在为空串）。
	IconPath string
}

// normalizeDirectoryKey 共享目录分组的键：Windows 下忽略大小写。
func normalizeDirectoryKey(path string) string {
	if runtime.GOOS == "windows" {
		return strings.ToLower(path)
	}
	return path
}

// GetRecentWorlds 扫描 snapshot 中全部实例的存档目录。
// 每实例按版本隔离设置解析各自的游戏目录，按目录去重后只扫一次 saves；
// 世界归属优先当前选中实例（一键启动不会无谓切换实例）。
// 结果按 LastPlayed 降序取前 max 个；任何异常返回空数组。
func GetRecentWorlds(snapshot instance.GameInstanceSnapshot, max int) []WorldInfo {
	worlds := make([]WorldInfo, 0)
	if snapshot.IsLoading ||
		strings.TrimSpace(snapshot.ErrorMessage) != "" ||
		strings.TrimSpace(snapshot.MinecraftDirectory) == "" {
		return []WorldInfo{}
	}

	// 目录 → 拥有该目录的实例列表（多实例共享目录只扫一次）
	type directoryOwners struct {
		key       string
		directory string
		owners    []string
	}
	var directories []*directoryOwners
	index := map[string]int{}
	for _, versionID := range snapshot.VersionIds {
		gameDirectory := instance.GameVersionIsolationGetGameDirectory(snapshot, versionID)
		if gameDirectory == "" {
			gameDirectory = snapshot.MinecraftDirectory
		}
		if strings.TrimSpace(gameDirectory) == "" {
			continue
		}

		key := normalizeDirectoryKey(gameDirectory)
		if existing, ok := index[key]; ok {
			directories[existing].owners = append(directories[existing].owners, versionID)
			continue
		}
		index[key] = len(directories)
		directories = append(directories, &directoryOwners{
			key:       key,
			directory: gameDirectory,
			owners:    []string{versionID},
		})
	}

	for _, entry := range directories {
		savesDirectory := filepath.Join(entry.directory, "saves")
		if info, err := os.Stat(savesDirectory); err != nil || !info.IsDir() {
			continue
		}

		// 目录被多个实例共享时记到当前选中实例名下
		owner := entry.owners[0]
		if snapshot.SelectedVersionId != "" {
			for _, candidate := range entry.owners {
				if strings.EqualFold(candidate, snapshot.SelectedVersionId) {
					owner = snapshot.SelectedVersionId
					break
				}
			}
		}

		entries, err := os.ReadDir(savesDirectory)
		if err != nil {
			continue
		}
		for _, worldEntry := range entries {
			if !worldEntry.IsDir() {
				continue
			}
			// 启动器的临时目录（存档导入 .nya-import-*、回滚恢复 .nya-restore-*）
			// 与世界目录同级，别把它们当成世界列出来
			if strings.HasPrefix(worldEntry.Name(), ".") {
				continue
			}
			worldPath := filepath.Join(savesDirectory, worldEntry.Name())
			levelFile := filepath.Join(worldPath, "level.dat")
			// 优先 NBT 里的真实 LastPlayed（Unix 毫秒）：mtime 会被复制存档、
			// 备份同步、网盘拉取刷新，把"最近游玩"排序搅乱；读不到再回落 mtime。
			var lastPlayed time.Time
			if fromNBT, ok := readLevelDatLastPlayed(levelFile); ok {
				lastPlayed = fromNBT
			} else if info, err := os.Stat(levelFile); err == nil {
				lastPlayed = info.ModTime()
			} else if info, err := os.Stat(worldPath); err == nil {
				lastPlayed = info.ModTime()
			} else {
				continue
			}
			iconPath := filepath.Join(worldPath, "icon.png")
			worldIcon := ""
			if tools.FileExists(iconPath) {
				worldIcon = iconPath
			}

			worlds = append(worlds, WorldInfo{
				Name:           worldEntry.Name(),
				DirectoryPath:  worldPath,
				OwnerVersionId: owner,
				LastPlayed:     lastPlayed,
				IconPath:       worldIcon,
			})
		}
	}

	// 按 LastPlayed 降序取前 max 个；任何 IO 异常在上方被跳过/返回空结果。
	sort.SliceStable(worlds, func(i, j int) bool {
		return worlds[i].LastPlayed.After(worlds[j].LastPlayed)
	})
	if max < 0 {
		max = 0
	}
	if len(worlds) > max {
		worlds = worlds[:max]
	}
	return worlds
}
