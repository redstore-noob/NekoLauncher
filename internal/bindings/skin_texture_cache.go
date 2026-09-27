package bindings

// 完整皮肤贴图（3D 预览用）及其磁盘缓存与后台刷新调度。

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"nekolauncher/internal/auth"
	"nekolauncher/internal/config"
	"nekolauncher/internal/logs"
	"nekolauncher/internal/tools"
)

// ---- 完整皮肤贴图（3D 预览） ----

// SkinTexture 账号当前外观的完整贴图（供 3D 皮肤预览等需要整张贴图的场景）。
type SkinTexture struct {
	// SkinUri 皮肤贴图（64×64 / 64×32 PNG）data URI，可直接喂给 WebGL 渲染器。
	SkinUri string `json:"skinUri"`
	// Model 皮肤模型："classic"（宽手臂）或 "slim"（窄手臂）。
	Model string `json:"model"`
	// CapeUri 披风贴图 data URI；无披风或解析失败为空串。
	CapeUri string `json:"capeUri"`
	// DisplayName 角色显示名。
	DisplayName string `json:"displayName"`
	// UpdatedAt 贴图写入缓存的时间（Unix 秒）；0 表示未缓存。
	UpdatedAt int64 `json:"updatedAt"`
	// Cached 本次结果是否直接来自本地缓存（未访问远端服务）。
	Cached bool `json:"cached"`
}

// skinTextureCacheTTL 皮肤缓存有效期：超过后下次访问会重新拉取（即一天自动刷新一次）。
const skinTextureCacheTTL = 24 * time.Hour

// skinCacheRefreshInterval 后台自动刷新当前账号皮肤缓存的周期。
const skinCacheRefreshInterval = 24 * time.Hour

// skinTextureCacheEntry 皮肤贴图磁盘缓存条目。
type skinTextureCacheEntry struct {
	SkinUri     string `json:"skinUri"`
	Model       string `json:"model"`
	CapeUri     string `json:"capeUri"`
	DisplayName string `json:"displayName"`
	UpdatedAt   int64  `json:"updatedAt"`
}

// skinTextureCacheDirectory 存储目录/appearance-cache/skin-textures。
func skinTextureCacheDirectory() string {
	storage := strings.TrimSpace(config.StorageDirectory())
	if storage == "" {
		return ""
	}
	return filepath.Join(storage, "appearance-cache", "skin-textures")
}

// skinTextureCachePath 账号皮肤缓存文件路径；存储目录不可用时返回空串。
func skinTextureCachePath(accountKey string) string {
	dir := skinTextureCacheDirectory()
	if dir == "" || strings.TrimSpace(accountKey) == "" {
		return ""
	}
	return filepath.Join(dir, sanitizeCacheKey(accountKey)+".json")
}

// sanitizeCacheKey 把账号稳定键转换为安全的文件名（去除 Windows 非法字符）。
func sanitizeCacheKey(key string) string {
	replacer := strings.NewReplacer(
		"\\", "_", "/", "_", ":", "_", "*", "_", "?", "_",
		"\"", "_", "<", "_", ">", "_", "|", "_", " ", "_",
	)
	trimmed := strings.Trim(replacer.Replace(key), "._")
	if trimmed == "" {
		return "account"
	}
	return trimmed
}

// readSkinTextureCache 读取账号皮肤磁盘缓存；不存在或损坏时返回 false。
func readSkinTextureCache(accountKey string) (*SkinTexture, bool) {
	path := skinTextureCachePath(accountKey)
	if path == "" {
		return nil, false
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	var entry skinTextureCacheEntry
	if err := json.Unmarshal(raw, &entry); err != nil {
		return nil, false
	}
	if strings.TrimSpace(entry.SkinUri) == "" {
		return nil, false
	}
	return &SkinTexture{
		SkinUri:     entry.SkinUri,
		Model:       entry.Model,
		CapeUri:     entry.CapeUri,
		DisplayName: entry.DisplayName,
		UpdatedAt:   entry.UpdatedAt,
		Cached:      true,
	}, true
}

// writeSkinTextureCache 原子写入账号皮肤磁盘缓存（失败静默：缓存非关键路径）。
func writeSkinTextureCache(accountKey string, tex *SkinTexture) {
	path := skinTextureCachePath(accountKey)
	if path == "" || tex == nil || strings.TrimSpace(tex.SkinUri) == "" {
		return
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	entry := skinTextureCacheEntry{
		SkinUri:     tex.SkinUri,
		Model:       tex.Model,
		CapeUri:     tex.CapeUri,
		DisplayName: tex.DisplayName,
		UpdatedAt:   tex.UpdatedAt,
	}
	raw, err := json.Marshal(entry)
	if err != nil {
		return
	}
	tmp, err := os.CreateTemp(dir, "skin-*.tmp")
	if err != nil {
		return
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		_ = os.Remove(tmpName)
		return
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(tmpName)
	}
}

// invalidateSkinTextureCache 皮肤变更后删除缓存，使下次读取重新拉取。
func invalidateSkinTextureCache(accountKey string) {
	if path := skinTextureCachePath(accountKey); path != "" {
		_ = os.Remove(path)
	}
}

// skinTextureCacheFresh 缓存是否仍在有效期内。
func skinTextureCacheFresh(tex *SkinTexture) bool {
	if tex == nil || tex.UpdatedAt <= 0 {
		return false
	}
	return time.Since(time.Unix(tex.UpdatedAt, 0)) < skinTextureCacheTTL
}

// GetSkinTexture 返回账号当前皮肤（含模型）与披风的完整贴图。正版/皮肤站账号走本地
// 磁盘缓存（有效期一天，命中即不访问远端），离线账号始终按本地皮肤解析。贴图一律
// 包装为 data URI：绕开 WebView 对远程贴图的跨域限制，也统一三种账号的解析路径。
func (a *AccountAPI) GetSkinTexture(accountID string) (*SkinTexture, error) {
	account := auth.Shared.FindByStableKey(accountID)
	if account == nil {
		return nil, fmt.Errorf("账号不存在：%s", accountID)
	}
	// 离线账号的皮肤源在本地，无需远端缓存
	if strings.EqualFold(account.Type, "offline") {
		return a.fetchSkinTexture(account)
	}

	accountKey := auth.Shared.GetStableKey(account)
	if cached, ok := readSkinTextureCache(accountKey); ok && skinTextureCacheFresh(cached) {
		return cached, nil
	}

	tex, err := a.fetchSkinTexture(account)
	if err != nil {
		// 远端拉取失败（如 429 限流）时回落到旧缓存，避免界面直接报错
		if cached, ok := readSkinTextureCache(accountKey); ok {
			return cached, nil
		}
		return nil, err
	}
	writeSkinTextureCache(accountKey, tex)
	return tex, nil
}

// RefreshSkinTexture 强制刷新账号皮肤并更新缓存（供前端「刷新」按钮调用）。
func (a *AccountAPI) RefreshSkinTexture(accountID string) (*SkinTexture, error) {
	account := auth.Shared.FindByStableKey(accountID)
	if account == nil {
		return nil, fmt.Errorf("账号不存在：%s", accountID)
	}
	tex, err := a.fetchSkinTexture(account)
	if err != nil {
		return nil, err
	}
	if !strings.EqualFold(account.Type, "offline") {
		writeSkinTextureCache(auth.Shared.GetStableKey(account), tex)
	}
	return tex, nil
}

// fetchSkinTexture 从账号类型对应的数据源拉取完整皮肤贴图（不读写磁盘缓存）。
func (a *AccountAPI) fetchSkinTexture(account *auth.LaunchAccount) (*SkinTexture, error) {
	tex := &SkinTexture{
		Model:       "classic",
		DisplayName: account.DisplayName,
		UpdatedAt:   time.Now().Unix(),
	}

	switch account.Type {
	case "offline":
		// OfflineSkinId：内置目录 Id，或自定义皮肤 PNG 的本地路径（扩展能力）。
		id := strings.TrimSpace(account.OfflineSkinId)
		if id == "" {
			id = "steve"
		}
		if isLocalPngPath(id) {
			if !tools.FileExists(id) {
				return nil, errors.New("自定义皮肤文件不存在")
			}
			uri, err := imageSourceDataURI(id)
			if err != nil {
				return nil, err
			}
			tex.SkinUri = uri
			return tex, nil
		}
		choice := GetOfflineSkinChoice(id)
		resolveGate.Lock()
		source := resolveOfflineSkinSourceLocked(choice)
		resolveGate.Unlock()
		uri, err := imageSourceDataURI(source)
		if err != nil {
			return nil, err
		}
		tex.SkinUri = uri
		tex.Model = normalizeModel(choice.Model)
		return tex, nil

	case "authlib":
		if account.Authlib == nil {
			return nil, errors.New("皮肤站账号缺少凭据")
		}
		textures, err := authlibTextures(callCtx(a.ctx), *account.Authlib)
		if err != nil {
			return nil, err
		}
		if textures == nil || textures.SkinURL == "" {
			return nil, errors.New("该账号没有可用皮肤贴图")
		}
		uri, err := imageSourceDataURI(textures.SkinURL)
		if err != nil {
			return nil, err
		}
		tex.SkinUri = uri
		tex.Model = textures.Model
		if textures.CapeURL != "" {
			if capeURI, capeErr := imageSourceDataURI(textures.CapeURL); capeErr == nil {
				tex.CapeUri = capeURI
			}
		}
		return tex, nil

	case "microsoft":
		if account.Microsoft == nil {
			return nil, errors.New("正版账号缺少凭据")
		}
		profile, err := a.fetchMojangProfile(account, false)
		if err != nil {
			return nil, err
		}
		if profile.ActiveSkinURL == "" {
			return nil, errors.New("该账号没有可用皮肤贴图")
		}
		uri, err := imageSourceDataURI(profile.ActiveSkinURL)
		if err != nil {
			return nil, err
		}
		tex.SkinUri = uri
		tex.Model = normalizeModel(profile.ActiveSkinVariant)
		if profile.ActiveCapeURL != "" {
			if capeURI, capeErr := imageSourceDataURI(profile.ActiveCapeURL); capeErr == nil {
				tex.CapeUri = capeURI
			}
		}
		return tex, nil

	default:
		return nil, fmt.Errorf("该账号类型不支持皮肤解析：%s", account.Type)
	}
}

// runSkinCacheRefreshScheduler 后台每天自动刷新一次当前账号的皮肤缓存：启动后
// 延迟片刻先刷新一次，之后每 24 小时刷新一次。远端失败仅记日志，不影响界面。
func (a *AccountAPI) runSkinCacheRefreshScheduler() {
	time.Sleep(30 * time.Second)
	a.refreshSelectedSkinCache()
	ticker := time.NewTicker(skinCacheRefreshInterval)
	defer ticker.Stop()
	for range ticker.C {
		a.refreshSelectedSkinCache()
	}
}

// refreshSelectedSkinCache 当前账号皮肤缓存过期（或缺失）时强制刷新并通知前端；
// 未过期则跳过，保证一天最多自动刷新一次。
func (a *AccountAPI) refreshSelectedSkinCache() {
	account := auth.Shared.Selected()
	if account == nil || strings.EqualFold(account.Type, "offline") {
		return
	}
	accountKey := auth.Shared.GetStableKey(account)
	if cached, ok := readSkinTextureCache(accountKey); ok && skinTextureCacheFresh(cached) {
		return
	}
	if _, err := a.RefreshSkinTexture(accountKey); err != nil {
		logs.Write("WARN", fmt.Sprintf("皮肤缓存自动刷新失败（%s）：%v", account.DisplayName, err))
		return
	}
	emit(a.ctx, "skin:textureChanged", accountKey)
}
