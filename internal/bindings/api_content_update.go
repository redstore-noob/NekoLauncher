package bindings

// X-4：实例内容更新检测的绑定。
//
// 挂在已有的 ContentAPI 上（不新增 API 结构体、不动 main.go 的 Bind 列表）：
//   - CheckInstanceContentUpdates：一次拿到整个实例的检测结果（Mod/资源包/光影 + 整合包清单比对）；
//   - DownloadContentUpdate：把新版本另存到用户选定的路径；
//   - ApplyContentUpdate：用新版本替换原文件（**先备份再替换**，失败回滚）。
//
// 目录与实例信息在这里解析后传进 download 包：download 不能直接 import instance
// （instance 依赖 download，会成环），所以实例信息经 download.InstanceGameInfoHook
// 注入（见 wiring.go 的 wireInstance）。这里的绑定方法只做参数解析与事件转发。

import (
	"fmt"
	"strings"

	"nekolauncher/internal/config"
	"nekolauncher/internal/download"
)

// CheckInstanceContentUpdates 检查实例内容的可更新情况。
//
// 参数与实例页现状保持一致：
//   - sourcePath：实例来源目录（根目录或外部实例目录）；
//   - minecraftDirectory：Minecraft 根目录；
//   - versionID：实例 ID。
//
// 返回值里 ContentDirectory 是实际检查的内容目录（隔离实例为 versions/<id>），
// Files 是逐文件的检测结果，Modpack 是整合包清单比对结果。
//
// 网络失败不会返回 error，而是把对应条目标成 checkFailed 并给出中文说明
// （实例页必须能照常渲染，不能因为 Modrinth 不可用就白屏）。
func (a *ContentAPI) CheckInstanceContentUpdates(
	sourcePath, minecraftDirectory, versionID string,
) (*download.ContentUpdateCheckResult, error) {
	ctx := callCtx(a.ctx)

	gameVersion, loaderName := resolveInstanceGameInfo(minecraftDirectory, sourcePath, versionID)
	contentDirectory := download.ResolveContentDirectoryForInstance(minecraftDirectory, sourcePath, versionID)
	if strings.TrimSpace(contentDirectory) == "" {
		// 未接线时 ResolveContentDirectoryForInstance 会退化为 minecraftDirectory 本身；
		// 只有连它都为空才是真的没法检查（例如实例尚未选中）。
		return nil, fmt.Errorf("无法确定实例的内容目录，请先选中一个实例。")
	}

	result, err := download.CheckInstanceContentUpdates(
		ctx, contentDirectory, versionID, gameVersion, loaderName,
		func(phase, message string) {
			emit(a.ctx, "content:updateProgress", map[string]string{
				"phase":   phase,
				"message": message,
			})
		})
	if err != nil {
		return nil, err
	}
	// 实例上下文补进结果，前端不用再自己拼
	result.GameVersion = gameVersion
	result.LoaderName = loaderName
	return result, nil
}

// DownloadContentUpdate 把某个可更新条目下载到 targetPath（另存，不改动原文件）。
// expectedSHA1 为检测结果里的文件哈希；为空则不校验哈希（只校验可读性）。
func (a *ContentAPI) DownloadContentUpdate(
	downloadURL, targetPath, expectedSHA1 string,
) (*download.ContentUpdateApplyResult, error) {
	if strings.TrimSpace(targetPath) == "" {
		return nil, fmt.Errorf("请先选择保存位置。")
	}
	return download.DownloadContentUpdate(callCtx(a.ctx), downloadURL, targetPath, expectedSHA1,
		func(downloaded, total int64) {
			emit(a.ctx, "content:updateDownload", map[string]any{
				"downloaded": downloaded,
				"total":      total,
			})
		})
}

// ApplyContentUpdate 用新版本替换 targetPath。
//
// 顺序固定：下载到临时文件 → 校验哈希 → 原文件改名备份（<文件名>.bak-<时间戳>）
// → 新文件落位。任何一步失败都回滚，**绝不静默覆盖**；返回结果里带备份路径，
// 前端必须展示给用户。
func (a *ContentAPI) ApplyContentUpdate(
	downloadURL, targetPath, expectedSHA1 string,
) (*download.ContentUpdateApplyResult, error) {
	if strings.TrimSpace(targetPath) == "" {
		return nil, fmt.Errorf("请先选择要替换的文件。")
	}
	return download.ApplyContentUpdate(callCtx(a.ctx), downloadURL, targetPath, expectedSHA1,
		func(downloaded, total int64) {
			emit(a.ctx, "content:updateDownload", map[string]any{
				"downloaded": downloaded,
				"total":      total,
			})
		})
}

// GetContentVersionOptions 列出已安装内容文件在资源站上的全部版本
// （升级与降级共用：选择任意版本 → ApplyContentUpdate 备份并替换）。
// 文件无法识别归属时返回带 Notice 的空版本列表，而不是报错——
// 前端要能展示"为什么不支持"。CurseForge 识别复用设置页保存的 API Key。
func (a *ContentAPI) GetContentVersionOptions(
	sourcePath, minecraftDirectory, versionID, filePath string,
) (*download.ContentVersionOptions, error) {
	gameVersion, loaderName := resolveInstanceGameInfo(minecraftDirectory, sourcePath, versionID)
	return download.GetContentVersionOptions(
		callCtx(a.ctx), filePath, gameVersion, loaderName,
		strings.TrimSpace(config.GetValue(curseForgeAPIKeyConfigKey)))
}

// resolveInstanceGameInfo 解析实例的基础 MC 版本与加载器显示名（未接线时空串）。
// 解析失败返回空串：调用方据此跳过兼容性过滤（宁可不判，也不编造）。
func resolveInstanceGameInfo(minecraftDirectory, sourcePath, versionID string) (string, string) {
	if download.InstanceGameInfoHook == nil {
		return "", ""
	}
	if strings.TrimSpace(minecraftDirectory) == "" || strings.TrimSpace(versionID) == "" {
		return "", ""
	}
	gameVersion, loaderName := download.InstanceGameInfoHook(minecraftDirectory, sourcePath, versionID)
	gameVersion = strings.TrimSpace(gameVersion)
	switch gameVersion {
	case "未识别", "未知", "未提供":
		gameVersion = ""
	}
	return gameVersion, strings.TrimSpace(loaderName)
}
