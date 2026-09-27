package download

// 内容文件（Mod/资源包/光影包）的版本管理：列出单个已安装文件在资源站上的
// 全部可用版本，供用户升级或降级（降级 = 选择任意更旧的版本，与升级同一条
// 替换管线：下载 → 校验 SHA-1 → 备份原文件 → 落位，见 content_update_apply.go）。
//
// 识别口径与更新检测一致（content_update_check.go）：本地文件 SHA-1 反查
// Modrinth 得到归属项目；CurseForge 没有免鉴权的哈希反查端点，独占内容
// 识别不了，只能给出说明而不是编造版本列表。

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"nekolauncher/internal/download/modrinth"
	"nekolauncher/internal/models"
)

// ContentVersionOptions 单个内容文件的版本管理数据。
type ContentVersionOptions struct {
	// FilePath / FileName 被管理的本地文件。
	FilePath string `json:"FilePath"`
	FileName string `json:"FileName"`
	// ProjectID / ProjectName / ProjectPageURL 归属项目（Modrinth）。
	ProjectID     string `json:"ProjectID"`
	ProjectName   string `json:"ProjectName"`
	ProjectPageURL string `json:"ProjectPageURL"`
	// Source 版本列表来源（当前恒为 modrinth）。
	Source string `json:"Source"`
	// CurrentVersionID / CurrentVersionNumber 当前安装版本（哈希反查所得）。
	CurrentVersionID     string `json:"CurrentVersionID"`
	CurrentVersionNumber string `json:"CurrentVersionNumber"`
	// CurrentSHA1 本地文件的 SHA-1。
	CurrentSHA1 string `json:"CurrentSHA1"`
	// Versions 全部版本（匹配当前实例的排在前面，组内新→旧）。
	Versions []models.ResourceVersion `json:"Versions"`
	// MatchedCount 其中匹配当前实例的条数。
	MatchedCount int `json:"MatchedCount"`
	// Notice 识别失败/部分失败的中文说明；空串表示正常。
	Notice string `json:"Notice"`
}

// GetContentVersionOptions 列出已安装内容文件可切换的全部版本。
// gameVersion / loaderName 为空时不做兼容性判定（只标记，不过滤）。
func GetContentVersionOptions(
	ctx context.Context,
	filePath, gameVersion, loaderName string,
) (*ContentVersionOptions, error) {
	options := &ContentVersionOptions{
		FilePath: filePath,
		FileName: filepath.Base(filePath),
		Source:   models.ResourceSourceModrinth,
		Versions: []models.ResourceVersion{},
	}
	if strings.TrimSpace(filePath) == "" {
		return nil, fmt.Errorf("文件路径不能为空")
	}

	hash, err := computeFileSHA1(filePath)
	if err != nil {
		return nil, fmt.Errorf("计算文件哈希失败：%w", err)
	}
	options.CurrentSHA1 = hash

	matches, err := modrinth.GetVersionFilesByHashes(ctx, []string{hash})
	if err != nil {
		return nil, fmt.Errorf("向 Modrinth 查询文件归属失败：%s", readableCheckError(err))
	}
	match, found := matches[hash]
	if !found {
		options.Notice = "Modrinth 未收录这个文件（自建包、手动放入或 CurseForge 独占），暂不支持版本管理。"
		return options, nil
	}

	options.ProjectID = match.ProjectID
	options.ProjectName = match.ProjectID
	options.CurrentVersionID = match.VersionID
	options.CurrentVersionNumber = match.VersionNumber
	if name, err := modrinth.GetProjectName(ctx, match.ProjectID); err == nil && strings.TrimSpace(name) != "" {
		options.ProjectName = name
	}
	options.ProjectPageURL = fmt.Sprintf("https://modrinth.com/project/%s", match.ProjectID)

	list, err := ListResourceVersions(ctx, models.ResourceVersionRequest{
		Source:      models.ResourceSourceModrinth,
		ProjectID:   match.ProjectID,
		GameVersion: gameVersion,
		Loader:      loaderName,
	}, "")
	if err != nil {
		return nil, fmt.Errorf("获取版本列表失败：%s", readableCheckError(err))
	}
	options.Versions = list.Versions
	options.MatchedCount = list.MatchedCount
	if len(options.Versions) == 0 {
		options.Notice = "该项目在 Modrinth 上还没有任何版本文件。"
	}
	return options, nil
}
