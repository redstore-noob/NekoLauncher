package bindings

// 头像裁剪、正版（Mojang）档案与皮肤上传、皮肤站（authlib）贴图解析。
// 对应 C# 的 MinecraftProfileService / AuthlibProfileTextureService。

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"nekolauncher/internal/auth"
	"nekolauncher/internal/info"
)

// ---- 头像（8×8 头部裁剪） ----

// avatarCacheKey → data URI 缓存：同一贴图源不重复下载/解码。
type avatarCacheEntry struct {
	source    string
	modTime   time.Time
	avatarURI string
}

var (
	avatarCacheMu sync.Mutex
	avatarCache   = map[string]avatarCacheEntry{}
)

// GetAvatarUrl 账号头像（当前皮肤贴图的 8×8 头部裁剪，data URI），失败返回错误，
// 前端回退到首字母占位（对应 C# AccountManagePage 的 AvatarSource 解析语义）。
// 贴图走皮肤缓存，避免每次加载账号列表都直连 Minecraft 档案服务（易触发 429）。
func (a *AccountAPI) GetAvatarUrl(accountID string) (string, error) {
	tex, err := a.GetSkinTexture(accountID)
	if err != nil {
		return "", err
	}
	if tex == nil || strings.TrimSpace(tex.SkinUri) == "" {
		return "", errors.New("该账号没有可用皮肤贴图")
	}
	return avatarDataURI(tex.SkinUri)
}

// loadSkinImage 从贴图源（data URI / http(s) URL / 本地路径）解码出贴图。
func loadSkinImage(source string) (image.Image, error) {
	if strings.HasPrefix(source, "data:") {
		comma := strings.Index(source, ",")
		if comma < 0 {
			return nil, errors.New("无效的 data URI")
		}
		raw, err := base64.StdEncoding.DecodeString(source[comma+1:])
		if err != nil {
			return nil, err
		}
		return png.Decode(bytes.NewReader(raw))
	}
	if isHTTPTextureURL(source) {
		resp, err := skinHTTPClient.Get(source)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("皮肤贴图下载返回 %d", resp.StatusCode)
		}
		return png.Decode(resp.Body)
	}
	file, err := os.Open(source)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return png.Decode(file)
}

// imageSourceDataURI 把任意贴图源统一转成 data URI（已是 data URI 的原样返回），
// 前端 WebGL/canvas 可直接使用、不受远程贴图跨域限制。
func imageSourceDataURI(source string) (string, error) {
	if strings.HasPrefix(source, "data:") {
		return source, nil
	}
	img, err := loadSkinImage(source)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return "", err
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}

// normalizeModel 皮肤模型归一化："slim"（任意大小写）→ slim，其余 → classic。
func normalizeModel(model string) string {
	if strings.EqualFold(strings.TrimSpace(model), "slim") {
		return "slim"
	}
	return "classic"
}

// avatarDataURI 从皮肤贴图源（远程 URL 或本地路径）提取 8×8 头部，返回 data URI。
func avatarDataURI(source string) (string, error) {
	var modTime time.Time
	if !strings.HasPrefix(source, "data:") {
		if info, err := os.Stat(source); err == nil {
			modTime = info.ModTime()
		}
	}

	avatarCacheMu.Lock()
	cached, ok := avatarCache[source]
	if ok && cached.modTime.Equal(modTime) {
		avatarCacheMu.Unlock()
		return cached.avatarURI, nil
	}
	avatarCacheMu.Unlock()

	skin, err := loadSkinImage(source)
	if err != nil {
		return "", err
	}

	// 8×8 头部：基础层 (8,8)-(16,16) + 双层（帽子）层 (40,8)-(48,16) 按 alpha
	// source-over 合成（64x64 与 64x32 贴图的头部/帽子区域坐标一致），放大后即经典 MC 头像
	head := image.NewNRGBA(image.Rect(0, 0, 8, 8))
	bounds := skin.Bounds()
	// 基础头部
	draw.Draw(head, head.Bounds(), skin, image.Point{X: bounds.Min.X + 8, Y: bounds.Min.Y + 8}, draw.Src)
	// 帽子层（透明像素不影响基础层）
	draw.Draw(head, head.Bounds(), skin, image.Point{X: bounds.Min.X + 40, Y: bounds.Min.Y + 8}, draw.Over)
	var buf bytes.Buffer
	if err := png.Encode(&buf, head); err != nil {
		return "", err
	}
	uri := "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())

	avatarCacheMu.Lock()
	avatarCache[source] = avatarCacheEntry{source, modTime, uri}
	avatarCacheMu.Unlock()
	return uri, nil
}

// ---- 正版（Mojang）档案与皮肤上传（对应 MinecraftProfileService） ----

// mojangProfile 正版档案解析结果。
type mojangProfile struct {
	ActiveSkinURL     string
	ActiveSkinVariant string
	ActiveCapeURL     string
	Capes             []mojangTexture
}

type mojangTexture struct {
	Id      string `json:"id"`
	State   string `json:"state"`
	URL     string `json:"url"`
	Variant string `json:"variant"`
	Alias   string `json:"alias"`
}

type mojangProfileDTO struct {
	Id    string          `json:"id"`
	Name  string          `json:"name"`
	Skins []mojangTexture `json:"skins"`
	Capes []mojangTexture `json:"capes"`
}

// normalizeTextureURL 贴图地址归一化：https 直接使用；http 仅当主机为
// textures.minecraft.net 时升级为 https（对应 C# NormalizeTextureUrl）。
func normalizeTextureURL(value string) string {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" {
		return ""
	}
	switch parsed.Scheme {
	case "https":
		return parsed.String()
	case "http":
		if strings.EqualFold(parsed.Hostname(), "textures.minecraft.net") {
			parsed.Scheme = "https"
			parsed.Host = parsed.Hostname()
			return parsed.String()
		}
	}
	return ""
}

// fetchMojangProfile 拉取正版档案；令牌过期自动刷新，401/403 强制刷新重试一次
// （对应 C# SendWithFreshTokenAsync）。
func (a *AccountAPI) fetchMojangProfile(account *auth.LaunchAccount, forceRefresh bool) (*mojangProfile, error) {
	token, err := a.freshMicrosoftToken(account, forceRefresh)
	if err != nil {
		return nil, err
	}
	profile, status, err := requestMojangProfile(token)
	if err != nil {
		return nil, err
	}
	if (status == http.StatusUnauthorized || status == http.StatusForbidden) && !forceRefresh {
		return a.fetchMojangProfile(account, true)
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("Minecraft 档案服务返回 %d", status)
	}
	return profile, nil
}

func requestMojangProfile(token string) (*mojangProfile, int, error) {
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
	active := ""
	activeVariant := ""
	for _, skin := range dto.Skins {
		if strings.EqualFold(skin.State, "ACTIVE") {
			active = normalizeTextureURL(skin.URL)
			activeVariant = skin.Variant
			break
		}
	}
	if active == "" && len(dto.Skins) > 0 {
		active = normalizeTextureURL(dto.Skins[0].URL)
		activeVariant = dto.Skins[0].Variant
	}
	activeCape := ""
	for _, cape := range dto.Capes {
		if strings.EqualFold(cape.State, "ACTIVE") {
			activeCape = normalizeTextureURL(cape.URL)
			break
		}
	}
	return &mojangProfile{
		ActiveSkinURL:     active,
		ActiveSkinVariant: activeVariant,
		ActiveCapeURL:     activeCape,
		Capes:             dto.Capes,
	}, http.StatusOK, nil
}

// freshMicrosoftToken 取可用的正版访问令牌：按账号刷新锁串行化，
// 过期（或强制）时经认证器刷新并写回存储（对应 C# EnsureFreshAccountAsync）。
func (a *AccountAPI) freshMicrosoftToken(account *auth.LaunchAccount, forceRefresh bool) (string, error) {
	if !strings.EqualFold(account.Type, "microsoft") || account.Microsoft == nil {
		return "", errors.New("当前账号不是可编辑皮肤的正版账号。")
	}
	var token string
	err := auth.Shared.WithRefreshLock(account, func() error {
		ms := account.Microsoft
		if ms == nil {
			return errors.New("当前账号不是可编辑皮肤的正版账号。")
		}
		if !forceRefresh && !ms.IsExpired() {
			token = ms.AccessToken
			return nil
		}
		refreshed, err := a.microsoft.Refresh(callCtx(a.ctx), *ms)
		if err != nil {
			// 轮换路径：旧 refresh_token 已被服务端作废，必须先把轮换后的
			// 新凭据落库，否则本次报错后下次刷新必然失败、用户被迫重新登录
			// （与启动管线 prepareMicrosoftAccount 的语义一致）。
			var rotated *auth.RotatedCredentialsError
			if errors.As(err, &rotated) {
				refreshedCopy := rotated.RefreshedAccount
				auth.Shared.UpdateMicrosoftAccount(account, &refreshedCopy)
			}
			return err
		}
		auth.Shared.UpdateMicrosoftAccount(account, &refreshed)
		token = refreshed.AccessToken
		return nil
	})
	if err != nil {
		return "", err
	}
	return token, nil
}

// UploadSkin 为正版账号上传皮肤（对应 MinecraftProfileService.UploadSkinAsync +
// MinecraftAppearanceEditor.ValidateSkinFile）：
// 校验 PNG（≤4 MiB，64×64 或 64×32）→ multipart 上传（variant: classic|slim）。
func (a *AccountAPI) UploadSkin(accountID, path, variant string) error {
	account := auth.Shared.FindByStableKey(accountID)
	if account == nil {
		return fmt.Errorf("账号不存在：%s", accountID)
	}
	if err := validateSkinFile(path); err != nil {
		return err
	}
	model := variant
	switch strings.ToLower(variant) {
	case "classic", "slim":
		model = strings.ToLower(variant)
	case "wide":
		model = "classic"
	default:
		return errors.New("皮肤模型必须是 classic（经典）或 slim（纤细）。")
	}

	return a.uploadSkinWithRetry(account, path, model, false)
}

func (a *AccountAPI) uploadSkinWithRetry(account *auth.LaunchAccount, path, model string, forceRefresh bool) error {
	token, err := a.freshMicrosoftToken(account, forceRefresh)
	if err != nil {
		return err
	}
	status, err := postMojangSkin(token, path, model)
	if err != nil {
		return err
	}
	if (status == http.StatusUnauthorized || status == http.StatusForbidden) && !forceRefresh {
		return a.uploadSkinWithRetry(account, path, model, true)
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("皮肤上传返回 %d", status)
	}
	// 皮肤已变更：清除缓存，使下次读取重新拉取
	invalidateSkinTextureCache(auth.Shared.GetStableKey(account))
	return nil
}

// postMojangSkin multipart 上传：variant 字段 + file 字段（image/png）。
// 注意：C# 与 Mojang 实际 API 均为 POST（非 PUT）。
func postMojangSkin(token, path, model string) (int, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer file.Close()

	body := &bytes.Buffer{}
	form := multipart.NewWriter(body)
	if err := form.WriteField("variant", model); err != nil {
		return 0, err
	}
	part, err := form.CreateFormFile("file", filepath.Base(path))
	if err != nil {
		return 0, err
	}
	if _, err := io.Copy(part, file); err != nil {
		return 0, err
	}
	if err := form.Close(); err != nil {
		return 0, err
	}

	req, err := http.NewRequest(http.MethodPost, minecraftSkinEndpoint, body)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", form.FormDataContentType())
	resp, err := skinHTTPClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	return resp.StatusCode, nil
}

// validateSkinFile 校验皮肤文件（对应 MinecraftAppearanceEditor.ValidateSkinFile）：
// 本地 PNG，大小 1 B–4 MiB，尺寸 64×64 或 64×32。
func validateSkinFile(path string) error {
	info, err := os.Stat(path)
	if err != nil || !strings.EqualFold(filepath.Ext(path), ".png") {
		return errors.New("皮肤文件必须是本地 PNG 图片。")
	}
	if info.Size() <= 0 || info.Size() > 4*1024*1024 {
		return errors.New("皮肤文件为空或超过 4 MiB 限制。")
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	img, err := png.Decode(file)
	if err != nil {
		return errors.New("皮肤文件不是有效的 PNG 图片。")
	}
	bounds := img.Bounds()
	width := bounds.Dx()
	height := bounds.Dy()
	if width != 64 || (height != 32 && height != 64) {
		return errors.New("Minecraft Java 皮肤尺寸必须为 64×64，或兼容旧版的 64×32。")
	}
	return nil
}

// ---- 皮肤站（authlib / Yggdrasil）皮肤解析（对应 AuthlibProfileTextureService） ----

// authlibTextureSet 皮肤站角色贴图解析结果（对应 Yggdrasil sessionserver textures 属性）。
type authlibTextureSet struct {
	// SkinURL 皮肤贴图地址（已校验为绝对 http(s) 地址；无皮肤为空）。
	SkinURL string
	// CapeURL 披风贴图地址（无披风为空）。
	CapeURL string
	// Model 皮肤模型："classic" 或 "slim"（metadata 缺省 classic）。
	Model string
}

// authlibTextures 经 sessionserver 读取角色贴图属性，从 base64 textures 值中
// 提取皮肤/披风地址与模型；无皮肤或解析失败返回零值与 nil（由调用方回退占位）。
func authlibTextures(ctx context.Context, credential auth.AuthlibCredential) (*authlibTextureSet, error) {
	requestURL := strings.TrimRight(credential.ApiRoot, "/") +
		"/sessionserver/session/minecraft/profile/" + credential.ProfileUuid
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "NekoLauncher/"+info.Version())
	resp, err := skinHTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, nil
	}

	var profile struct {
		Properties []struct {
			Name  string `json:"name"`
			Value string `json:"value"`
		} `json:"properties"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&profile); err != nil {
		return nil, nil
	}
	textureValue := ""
	for _, property := range profile.Properties {
		if property.Name == "textures" {
			textureValue = property.Value
			break
		}
	}
	if textureValue == "" {
		return nil, nil
	}
	decoded, err := base64.StdEncoding.DecodeString(textureValue)
	if err != nil {
		return nil, nil
	}
	var payload struct {
		Textures struct {
			Skin struct {
				URL      string `json:"url"`
				Metadata struct {
					Model string `json:"model"`
				} `json:"metadata"`
			} `json:"SKIN"`
			Cape struct {
				URL string `json:"url"`
			} `json:"CAPE"`
		} `json:"textures"`
	}
	if err := json.Unmarshal(decoded, &payload); err != nil {
		return nil, nil
	}
	set := &authlibTextureSet{Model: normalizeModel(payload.Textures.Skin.Metadata.Model)}
	// 仅接受绝对 http(s) 地址（与 C# 语义一致）
	if isHTTPTextureURL(payload.Textures.Skin.URL) {
		set.SkinURL = payload.Textures.Skin.URL
	}
	if isHTTPTextureURL(payload.Textures.Cape.URL) {
		set.CapeURL = payload.Textures.Cape.URL
	}
	return set, nil
}

// isHTTPTextureURL 是否为可用的绝对 http(s) 贴图地址。
func isHTTPTextureURL(value string) bool {
	return strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://")
}
