// api_download_resources.go 资源搜索（Modrinth / CurseForge）的 Wails 绑定。
//
// 为什么挂在已有的 DownloadAPI 上：资源搜索的终点就是"把文件放进当前实例的
// 内容目录"，与 DownloadAPI 原有的内容下载是同一件事；每新开一个 API 结构体
// 就要多改一处 main.go 的 Bind 列表（P3-7 已按"能不加就不加"收敛过一轮）。
//
// CurseForge API Key 只有编译期内置一条来源（builtin_keys.go），不提供用户
// 自行配置：内置 Key 是官方渠道统一分配的，用户侧没有任何合法获取途径，
// 暴露填写入口只会诱导用户把随手申请的 Key 交进来共享配额被限流。
package bindings

import (
	"context"

	"nekolauncher/internal/download"
	"nekolauncher/internal/models"
)

// GetResourceSources 资源站清单：域名、镜像地址与"是否已配置 Key"全部由
// Go 侧给出，前端只按 Available / NeedsAPIKey 渲染引导。
func (a *DownloadAPI) GetResourceSources() []models.ResourceSourceInfo {
	return download.ListResourceSources(effectiveCurseForgeAPIKey())
}

// SearchResources 搜索资源。
// 支持按项目类型（mod / modpack / shader / resourcepack）、MC 版本与加载器过滤；
// CurseForge 未配置 API Key 时返回 NeedsAPIKey + 中文引导而不是错误。
func (a *DownloadAPI) SearchResources(request models.ResourceSearchRequest) (models.ResourceSearchResult, error) {
	return download.SearchResources(callCtx(a.ctx), request, effectiveCurseForgeAPIKey())
}

// ListResourceVersions 列出项目版本，并标记哪些匹配当前实例的 MC 版本 / 加载器
// （匹配项排在前面，不匹配的带 MatchNote 说明原因，仍可选择——用户常常要装旧版本）。
func (a *DownloadAPI) ListResourceVersions(request models.ResourceVersionRequest) (models.ResourceVersionList, error) {
	return download.ListResourceVersions(callCtx(a.ctx), request, effectiveCurseForgeAPIKey())
}

// DownloadResourceVersion 下载选定版本到实例内容目录（mods / resourcepacks / shaderpacks）。
// 文件地址由后端按版本 ID 反查，下载走既有内容下载通道（断点续传 / 限速 / 源回退），
// 并包成内容任务：右下角下载中心可见、可取消。
func (a *DownloadAPI) DownloadResourceVersion(request models.ResourceDownloadRequest) (models.ResourceDownloadResult, error) {
	var result models.ResourceDownloadResult
	err := download.RunContentDownload(callCtx(a.ctx), "content", request.VersionID,
		func(taskCtx context.Context, report download.ProgressBytes, setDetail func(string)) error {
			var runErr error
			result, runErr = download.DownloadResourceVersion(taskCtx, request, effectiveCurseForgeAPIKey(), report)

			return runErr
		})
	if err != nil {
		return models.ResourceDownloadResult{}, err
	}
	return result, nil
}
