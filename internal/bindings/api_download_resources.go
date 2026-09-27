// api_download_resources.go 资源搜索（Modrinth / CurseForge）的 Wails 绑定。
//
// 为什么挂在已有的 DownloadAPI 上：资源搜索的终点就是"把文件放进当前实例的
// 内容目录"，与 DownloadAPI 原有的内容下载是同一件事；每新开一个 API 结构体
// 就要多改一处 main.go 的 Bind 列表（P3-7 已按"能不加就不加"收敛过一轮）。
package bindings

import (
	"strings"

	"nekolauncher/internal/config"
	"nekolauncher/internal/download"
	"nekolauncher/internal/models"
)

// curseForgeAPIKeyConfigKey CurseForge API Key 的配置键。
// 走 config.GetValue / SetValue 存在 launcher.yaml 里，不新建配置文件。
// 注意：Key 是用户自己的凭据，明文保存（与其它设置一致），任何日志都不能打印它。
const curseForgeAPIKeyConfigKey = "curseforgeApiKey"

// GetCurseForgeAPIKey 读取已保存的 CurseForge API Key（未配置时返回空串）。
func (a *DownloadAPI) GetCurseForgeAPIKey() string {
	return strings.TrimSpace(config.GetValue(curseForgeAPIKeyConfigKey))
}

// SaveCurseForgeAPIKey 保存 CurseForge API Key；空串 = 清除，回到"未配置"状态。
// 返回是否写入成功（配置存储不可用时为 false，界面应提示保存失败而不是假装成功）。
func (a *DownloadAPI) SaveCurseForgeAPIKey(apiKey string) bool {
	trimmed := strings.TrimSpace(apiKey)
	if trimmed == "" {
		return config.ClearValue(curseForgeAPIKeyConfigKey)
	}
	return config.SetValue(curseForgeAPIKeyConfigKey, trimmed)
}

// GetResourceSources 资源站清单：域名、镜像地址、Key 申请地址与"是否已配置 Key"
// 全部由 Go 侧给出，前端只按 Available / NeedsAPIKey 渲染引导。
func (a *DownloadAPI) GetResourceSources() []models.ResourceSourceInfo {
	return download.ListResourceSources(a.GetCurseForgeAPIKey())
}

// SearchResources 搜索资源。
// 支持按项目类型（mod / modpack / shader / resourcepack）、MC 版本与加载器过滤；
// CurseForge 未配置 API Key 时返回 NeedsAPIKey + 中文引导而不是错误。
func (a *DownloadAPI) SearchResources(request models.ResourceSearchRequest) (models.ResourceSearchResult, error) {
	return download.SearchResources(callCtx(a.ctx), request, a.GetCurseForgeAPIKey())
}

// ListResourceVersions 列出项目版本，并标记哪些匹配当前实例的 MC 版本 / 加载器
// （匹配项排在前面，不匹配的带 MatchNote 说明原因，仍可选择——用户常常要装旧版本）。
func (a *DownloadAPI) ListResourceVersions(request models.ResourceVersionRequest) (models.ResourceVersionList, error) {
	return download.ListResourceVersions(callCtx(a.ctx), request, a.GetCurseForgeAPIKey())
}

// DownloadResourceVersion 下载选定版本到实例内容目录（mods / resourcepacks / shaderpacks）。
// 文件地址由后端按版本 ID 反查，下载走既有内容下载通道（断点续传 / 限速 / 源回退）；
// 进度经 download:contentProgress 事件推送 {downloaded, total}，与其它内容下载一致。
func (a *DownloadAPI) DownloadResourceVersion(request models.ResourceDownloadRequest) (models.ResourceDownloadResult, error) {
	return download.DownloadResourceVersion(callCtx(a.ctx), request, a.GetCurseForgeAPIKey(),
		func(downloaded, total int64) {
			emit(a.ctx, "download:contentProgress", map[string]int64{"downloaded": downloaded, "total": total})
		})
}
