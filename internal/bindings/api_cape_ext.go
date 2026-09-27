package bindings

// 披风 API 扩展（对应 C# NyaLauncher.Avalonia 的 MinecraftProfileService 披风部分：
// GetProfileAsync 的披风列表 + SetActiveCapeAsync 的激活/停用 + EnsureSuccessAsync 的
// 错误细节解析）。复用 api_skin_ext.go 的令牌刷新与贴图地址归一化。

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"nekolauncher/internal/auth"
)

// minecraftCapeEndpoint 披风激活/停用端点（C# ActiveCapeEndpoint）。
const minecraftCapeEndpoint = "https://api.minecraftservices.com/minecraft/profile/capes/active"

// MinecraftProfileTexture 一件皮肤/披风纹理（对应 C# MinecraftProfileTexture）。
type MinecraftProfileTexture struct {
	// Id 纹理 Id。
	Id string `json:"id"`
	// URL 纹理下载地址（已归一化为 https，可直接作为 <img src> 使用）。
	URL string `json:"url"`
	// Alias 显示别名（服务端提供时），如披风活动名称。
	Alias string `json:"alias"`
	// Variant 变体标识（披风通常为空）。
	Variant string `json:"variant"`
	// IsActive 是否为当前启用的一件。
	IsActive bool `json:"isActive"`
}

// MinecraftProfile 正版档案：角色身份 + 皮肤/披风列表（对应 C# MinecraftProfile）。
type MinecraftProfile struct {
	// Id 角色 Id。
	Id string `json:"id"`
	// Name 玩家名。
	Name string `json:"name"`
	// Skins 皮肤列表，最多一件处于激活态。
	Skins []MinecraftProfileTexture `json:"skins"`
	// Capes 披风列表，最多一件处于激活态。
	Capes []MinecraftProfileTexture `json:"capes"`
}

// GetMinecraftProfile 拉取正版档案（含皮肤/披风列表），供前端披风选择等外观编辑
// 功能使用。仅支持正版账号；令牌过期自动刷新，401/403 强制刷新后重试一次
// （对应 C# GetProfileAsync + SendWithFreshTokenAsync）。
func (a *AccountAPI) GetMinecraftProfile(accountID string) (*MinecraftProfile, error) {
	account := auth.Shared.FindByStableKey(accountID)
	if account == nil {
		return nil, fmt.Errorf("账号不存在：%s", accountID)
	}
	return a.fetchMojangProfileFull(account, false)
}

// SetActiveCape 激活或停用披风（对应 C# SetActiveCapeAsync）。
// capeId 非空时 PUT 激活；为空时 DELETE 停用当前披风。
func (a *AccountAPI) SetActiveCape(accountID, capeId string) error {
	account := auth.Shared.FindByStableKey(accountID)
	if account == nil {
		return fmt.Errorf("账号不存在：%s", accountID)
	}
	return a.setActiveCapeWithRetry(account, strings.TrimSpace(capeId), false)
}

func (a *AccountAPI) setActiveCapeWithRetry(account *auth.LaunchAccount, capeId string, forceRefresh bool) error {
	token, err := a.freshMicrosoftToken(account, forceRefresh)
	if err != nil {
		return err
	}
	status, detail, err := requestMojangCapeActive(token, capeId)
	if err != nil {
		return err
	}
	if (status == http.StatusUnauthorized || status == http.StatusForbidden) && !forceRefresh {
		return a.setActiveCapeWithRetry(account, capeId, true)
	}
	if status < 200 || status >= 300 {
		if status == http.StatusForbidden && strings.Contains(strings.ToUpper(detail), "ACCOUNT_SUSPENDED") {
			return errors.New("正版账号已被 Minecraft 服务封禁（ACCOUNT_SUSPENDED），披风不可用，需联系官方客服申诉。")
		}
		if strings.TrimSpace(detail) != "" {
			return fmt.Errorf("Minecraft 档案服务返回 %d：%s", status, detail)
		}
		return fmt.Errorf("Minecraft 档案服务返回 %d。", status)
	}
	// 披风已变更：清除皮肤缓存，使下次读取重新拉取
	invalidateSkinTextureCache(auth.Shared.GetStableKey(account))
	return nil
}

// fetchMojangProfileFull 拉取完整档案（重试语义与 fetchMojangProfile 一致）。
func (a *AccountAPI) fetchMojangProfileFull(account *auth.LaunchAccount, forceRefresh bool) (*MinecraftProfile, error) {
	token, err := a.freshMicrosoftToken(account, forceRefresh)
	if err != nil {
		return nil, err
	}
	profile, status, err := requestMojangProfileFull(token)
	if err != nil {
		return nil, err
	}
	if (status == http.StatusUnauthorized || status == http.StatusForbidden) && !forceRefresh {
		return a.fetchMojangProfileFull(account, true)
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("Minecraft 档案服务返回 %d", status)
	}
	return profile, nil
}

func requestMojangProfileFull(token string) (*MinecraftProfile, int, error) {
	req, err := http.NewRequest(http.MethodGet, minecraftProfileEndpoint, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := doMojangRequest(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, resp.StatusCode, nil
	}
	var dto mojangProfileDTO
	if err := json.NewDecoder(resp.Body).Decode(&dto); err != nil {
		return nil, resp.StatusCode, err
	}
	if strings.TrimSpace(dto.Id) == "" || strings.TrimSpace(dto.Name) == "" {
		return nil, resp.StatusCode, errors.New("Minecraft 档案响应缺少玩家身份信息。")
	}
	return &MinecraftProfile{
		Id:    dto.Id,
		Name:  dto.Name,
		Skins: convertMojangTextures(dto.Skins),
		Capes: convertMojangTextures(dto.Capes),
	}, http.StatusOK, nil
}

// convertMojangTextures 转换纹理列表：丢弃 Id 为空或地址非法的条目，
// state == "ACTIVE" 记为激活（对应 C# ConvertTextures）。
func convertMojangTextures(source []mojangTexture) []MinecraftProfileTexture {
	result := make([]MinecraftProfileTexture, 0, len(source))
	for _, texture := range source {
		if strings.TrimSpace(texture.Id) == "" {
			continue
		}
		normalized := normalizeTextureURL(texture.URL)
		if normalized == "" {
			continue
		}
		result = append(result, MinecraftProfileTexture{
			Id:       texture.Id,
			URL:      normalized,
			Alias:    texture.Alias,
			Variant:  texture.Variant,
			IsActive: strings.EqualFold(texture.State, "ACTIVE"),
		})
	}
	return result
}

// requestMojangCapeActive PUT（capeId 非空）/ DELETE（空）当前披风，
// 返回状态码与错误细节（对应 C# EnsureSuccessAsync 的 errorMessage/path 解析）。
func requestMojangCapeActive(token, capeId string) (int, string, error) {
	method := http.MethodDelete
	var body io.Reader
	if capeId != "" {
		method = http.MethodPut
		payload, err := json.Marshal(map[string]string{"capeId": capeId})
		if err != nil {
			return 0, "", err
		}
		body = bytes.NewReader(payload)
	}
	req, err := http.NewRequest(method, minecraftCapeEndpoint, body)
	if err != nil {
		return 0, "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := doMojangRequest(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return resp.StatusCode, "", nil
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return resp.StatusCode, "", nil
	}
	var errorDTO struct {
		ErrorMessage string `json:"errorMessage"`
		Path         string `json:"path"`
	}
	if json.Unmarshal(raw, &errorDTO) == nil {
		if errorDTO.ErrorMessage != "" {
			return resp.StatusCode, errorDTO.ErrorMessage, nil
		}
		if errorDTO.Path != "" {
			return resp.StatusCode, errorDTO.Path, nil
		}
	}
	return resp.StatusCode, "", nil
}
