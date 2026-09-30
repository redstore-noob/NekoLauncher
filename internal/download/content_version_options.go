package download

// 内容文件（Mod/资源包/光影包）的版本管理：列出单个已安装文件在资源站上的
// 全部可用版本，供用户升级或降级（降级 = 选择任意更旧的版本，与升级同一条
// 替换管线：下载 → 校验 SHA-1 → 备份原文件 → 落位，见 content_update_apply.go）。
//
// 识别口径与更新检测一致（content_update_check.go）：本地文件 SHA-1 反查
// Modrinth 得到归属项目；CurseForge 没有哈希反查端点，改用 murmur2 指纹
// 精确反查（需 API Key，与设置页保存的是同一个 Key）——指纹也未命中的
// 文件（自建包、手动放入）没有任何可信元数据来源，只给出手工管理指引。

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"nekolauncher/internal/download/curseforge"
	"nekolauncher/internal/download/modrinth"
	"nekolauncher/internal/models"
)

// ContentVersionOptions 单个内容文件的版本管理数据。
type ContentVersionOptions struct {
	// FilePath / FileName 被管理的本地文件。
	FilePath string `json:"FilePath"`
	FileName string `json:"FileName"`
	// ProjectID / ProjectName / ProjectPageURL 归属项目（Modrinth 或 CurseForge）。
	ProjectID      string `json:"ProjectID"`
	ProjectName    string `json:"ProjectName"`
	ProjectPageURL string `json:"ProjectPageURL"`
	// Source 版本列表来源（modrinth / curseforge；识别失败为空）。
	Source string `json:"Source"`
	// CurrentVersionID / CurrentVersionNumber 当前安装版本（反查所得）。
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
	// NeedsAPIKey 为 true 表示配置 CurseForge API Key 后可以继续（前端引导去设置页）。
	NeedsAPIKey bool `json:"NeedsAPIKey"`
}

// GetContentVersionOptions 列出已安装内容文件可切换的全部版本。
// gameVersion / loaderName 为空时不做兼容性判定（只标记，不过滤）。
// apiKey 为 CurseForge API Key（可为空：Modrinth 命中时不参与，未命中时降级提示）。
func GetContentVersionOptions(
	ctx context.Context,
	filePath, gameVersion, loaderName, apiKey string,
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
	if match, found := matches[hash]; found {
		return options.fillModrinth(ctx, match, gameVersion, loaderName)
	}
	return options.fillFromCurseForgeOrFallback(ctx, apiKey, gameVersion, loaderName)
}

// fillModrinth Modrinth 命中：取项目名与版本列表。
func (options *ContentVersionOptions) fillModrinth(
	ctx context.Context,
	match modrinth.VersionFileMatch,
	gameVersion, loaderName string,
) (*ContentVersionOptions, error) {
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

// fillFromCurseForgeOrFallback Modrinth 未命中：先试 CurseForge 指纹反查
// （需要 API Key），指纹也未命中（自建包/手动放入）时给出手工管理指引。
func (options *ContentVersionOptions) fillFromCurseForgeOrFallback(
	ctx context.Context,
	apiKey, gameVersion, loaderName string,
) (*ContentVersionOptions, error) {
	if strings.TrimSpace(apiKey) == "" {
		options.Source = ""
		options.NeedsAPIKey = true
		options.Notice = "Modrinth 未收录这个文件（可能是 CurseForge 独占、自建包或手动放入）。" +
			"配置 CurseForge API Key 后可自动识别 CurseForge 文件并列出版本；" +
			curseforge.APIKeyHint
		return options, nil
	}

	fingerprint, err := curseforge.FingerprintFile(options.FilePath)
	if err != nil {
		return nil, fmt.Errorf("计算 CurseForge 指纹失败：%w", err)
	}
	matches, err := curseforge.Fingerprints(ctx, apiKey, []uint32{fingerprint})
	if err != nil {
		return nil, fmt.Errorf("向 CurseForge 查询文件指纹失败：%s", readableCheckError(err))
	}
	match, found := matches[fingerprint]
	if !found {
		options.Source = ""
		options.Notice = "Modrinth 与 CurseForge 都未收录这个文件（自建包或手动放入），无法自动管理版本。" +
			"你可以打开所在目录手动管理，或按文件名到资源站搜索。"
		return options, nil
	}

	projectID := strconv.FormatInt(match.ProjectID, 10)
	options.Source = models.ResourceSourceCurseForge
	options.ProjectID = projectID
	options.ProjectName = match.DisplayName
	options.CurrentVersionID = strconv.FormatInt(match.FileID, 10)
	options.CurrentVersionNumber = match.DisplayName

	// 项目详情补名称与主页链接；失败不致命（还能用文件显示名继续）
	if project, err := curseforge.GetMod(ctx, apiKey, projectID); err == nil && project != nil {
		if strings.TrimSpace(project.Name) != "" {
			options.ProjectName = project.Name
		}
		if url := strings.TrimSpace(project.Links.WebsiteURL); url != "" {
			options.ProjectPageURL = url
		}
	}

	list, err := ListResourceVersions(ctx, models.ResourceVersionRequest{
		Source:      models.ResourceSourceCurseForge,
		ProjectID:   projectID,
		GameVersion: gameVersion,
		Loader:      loaderName,
	}, apiKey)
	if err != nil {
		return nil, fmt.Errorf("获取版本列表失败：%s", readableCheckError(err))
	}
	options.Versions = list.Versions
	options.MatchedCount = list.MatchedCount
	if len(options.Versions) == 0 {
		options.Notice = "该项目在 CurseForge 上还没有可列出的版本文件（可能被分页截断或已下架）。"
	}
	return options, nil
}
