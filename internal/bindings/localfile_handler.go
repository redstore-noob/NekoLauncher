package bindings

// 本地音频/视频流 Handler：挂在 Wails assetserver.Options.Handler（内嵌资源未命中时的
// 回退处理器），提供 /localfile?path=... 路由，把本地音频/图片/视频文件流式返回给 WebView。
// 背景：WebView 内 <audio>/<video>/new Audio 无法直接访问 file:// 盘符路径，需经应用内
// HTTP 路由中转。安全模型：这是本地单用户桌面应用，无远程访问面；仍做两层校验——
// 仅接受存在文件的绝对路径，且扩展名在白名单内，避免被当作任意文件读取通道。

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// transcodedWallpaperName Windows 转码壁纸的固定文件名（无扩展名）。
const transcodedWallpaperName = "TranscodedWallpaper"

// localFileRoute 本地文件流路由（背景图/音频/视频经此外发给 WebView）。
const localFileRoute = "/localfile"

// audioExtWhitelist 允许流式返回的音频扩展名。
// 必须与 music.SupportedExtensions 对齐（曲库扫描出来的格式都要能播），
// 且都是 WebView（Chromium/WebKit）能解码的容器：WMA 不在其中，故曲库也不收它。
var audioExtWhitelist = map[string]bool{
	".mp3": true, ".ogg": true, ".wav": true, ".flac": true, ".m4a": true,
	".aac": true, ".opus": true,
}

// imageExtWhitelist 允许流式返回的图片扩展名（皮肤/头像贴图与启动器背景图经 /localfile 展示）。
var imageExtWhitelist = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".webp": true, ".gif": true, ".bmp": true,
}

// videoExtWhitelist 允许流式返回的视频扩展名（Wallpaper Engine 视频壁纸原文件经
// /localfile 直接在背景播放）；仅放行 WebView 可解码的常见容器。
var videoExtWhitelist = map[string]bool{
	".mp4": true, ".webm": true, ".ogv": true, ".m4v": true, ".mov": true,
}

// contentTypeByExt 音频/图片/视频 Content-Type（http.ServeContent 可凭扩展名猜测，这里显式覆盖）。
var contentTypeByExt = map[string]string{
	".mp3": "audio/mpeg", ".ogg": "audio/ogg", ".wav": "audio/wav",
	".flac": "audio/flac", ".m4a": "audio/mp4",
	".aac": "audio/aac", ".opus": "audio/ogg",
	".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg",
	".webp": "image/webp", ".gif": "image/gif", ".bmp": "image/bmp",
	".mp4": "video/mp4", ".webm": "video/webm", ".ogv": "video/ogg",
	".m4v": "video/mp4", ".mov": "video/quicktime",
}

// NewLocalFileHandler 返回 assetserver 回退处理器：
// 命中 /localfile 时按查询参数 path 流式返回本地音频文件，其余请求 404。
// 前端用法：new Audio('/localfile?path=' + encodeURIComponent(p))
func NewLocalFileHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != localFileRoute {
			http.NotFound(w, r)
			return
		}
		p := r.URL.Query().Get("path")
		if p == "" || !filepath.IsAbs(p) {
			http.Error(w, "invalid path", http.StatusBadRequest)
			return
		}
		ext := strings.ToLower(filepath.Ext(p))
		if ext == "" {
			// 无扩展名仅放行 Windows 转码壁纸这一个精确文件名（商店主题壁纸常被
			// 转码为 %AppData%\Microsoft\Windows\Themes\TranscodedWallpaper），
			// 其余按扩展名白名单拒绝，避免 /localfile 变成任意文件读取通道。
			if !strings.EqualFold(filepath.Base(p), transcodedWallpaperName) {
				http.Error(w, "extension not allowed", http.StatusForbidden)
				return
			}
		} else if !audioExtWhitelist[ext] && !imageExtWhitelist[ext] && !videoExtWhitelist[ext] {
			http.Error(w, "extension not allowed", http.StatusForbidden)
			return
		}
		if info, err := os.Stat(p); err != nil || info.IsDir() {
			http.NotFound(w, r)
			return
		}
		// 无扩展名时不设 Content-Type，交给 ServeFile 按文件头嗅探（image/jpeg 等）。
		if ct, ok := contentTypeByExt[ext]; ok {
			w.Header().Set("Content-Type", ct)
		}
		// ServeFile 支持 Range（拖动进度条）与流式传输。
		http.ServeFile(w, r, p)
	})
}
