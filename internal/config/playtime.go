package config

import (
	"sort"
	"strings"
	"time"
)

// PlaytimeRecord 单个实例的游玩时长统计（主页统计卡片数据源）。
type PlaytimeRecord struct {
	MinecraftDirectory string `json:"MinecraftDirectory"`
	VersionId          string `json:"VersionId"`
	PlaytimeSeconds    int64  `json:"PlaytimeSeconds"`
	LastPlayedAt       int64  `json:"LastPlayedAt"`
}

// AddPlaytime 游戏进程退出后累加游玩时长并刷新最后游玩时间。
// 只更新时长相关字段，不触碰实例的其他启动设置；失败返回 false（无副作用路径除外）。
func AddPlaytime(minecraftDirectory, versionId string, seconds int64, exitedAt time.Time) bool {
	if strings.TrimSpace(minecraftDirectory) == "" || strings.TrimSpace(versionId) == "" || seconds <= 0 {
		return false
	}

	normalized := normalizePathOrOriginal(minecraftDirectory)
	profileGate.Lock()
	defer profileGate.Unlock()
	profiles := loadProfiles()
	// 与 GetInstanceIconOverride 同模式：只更新最后一条匹配，更新后移到末尾
	index := findProfileIndex(profiles, normalized, versionId)
	var profile GameVersionProfile
	if index >= 0 {
		profile = profiles[index]
		profiles = append(profiles[:index], profiles[index+1:]...)
	} else {
		profile = NewGameVersionProfile()
		profile.MinecraftDirectory = normalized
		profile.VersionId = versionId
	}
	profile.PlaytimeSeconds += seconds
	profile.LastPlayedAt = exitedAt.Unix()
	profiles = append(profiles, profile)
	if !SetValue(profilesKey, serializeProfiles(profiles)) {
		return false
	}

	raiseChanged()
	return true
}

// GetPlaytimeStats 返回全部有游玩记录的实例时长，按累计时长降序排列。
func GetPlaytimeStats() []PlaytimeRecord {
	profileGate.Lock()
	defer profileGate.Unlock()
	profiles := loadProfiles()
	records := make([]PlaytimeRecord, 0, len(profiles))
	for _, profile := range profiles {
		if profile.PlaytimeSeconds <= 0 && profile.LastPlayedAt <= 0 {
			continue
		}
		records = append(records, PlaytimeRecord{
			MinecraftDirectory: profile.MinecraftDirectory,
			VersionId:          profile.VersionId,
			PlaytimeSeconds:    profile.PlaytimeSeconds,
			LastPlayedAt:       profile.LastPlayedAt,
		})
	}
	sort.SliceStable(records, func(i, j int) bool {
		return records[i].PlaytimeSeconds > records[j].PlaytimeSeconds
	})
	return records
}
