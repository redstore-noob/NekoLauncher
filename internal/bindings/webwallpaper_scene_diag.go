package bindings

// 场景渲染诊断端点(临时排障用):POST /wescene/diag 把前端渲染器的
// 启动结果落到存储目录日志,定位生产包里场景壁纸"卡住"的问题。
// 挂在场景路由处理器上;诊断关闭后删除本文件即可。

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"nekolauncher/internal/config"
)

var sceneDiagOnce sync.Once

// NewSceneDiagHandler 返回诊断端点处理器。
func NewSceneDiagHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(io.LimitReader(r.Body, 4096))
		var payload struct {
			Event     string `json:"event"`
			Layers    int    `json:"layers"`
			Texts     int    `json:"texts"`
			Particles int    `json:"particles"`
			T         string `json:"t"`
			Text      string `json:"text"`
			OpaquePixels int `json:"opaquePixels"`
			CanvasW  int `json:"canvasW"`
			ScreenX  int `json:"screenX"`
			ScreenY  int `json:"screenY"`
			MeshVisible bool `json:"meshVisible"`
			MaterialOpacity float64 `json:"materialOpacity"`
			RenderOrder int `json:"renderOrder"`
			GroupPos string `json:"groupPos"`
			MeshScale string `json:"meshScale"`
		}
		_ = json.Unmarshal(body, &payload)
		line := time.Now().Format("15:04:05") + " " + payload.Event +
			" layers=" + jsonInt(payload.Layers) + " texts=" + jsonInt(payload.Texts) +
			" particles=" + jsonInt(payload.Particles) +
			" text=" + payload.Text + " opaque=" + jsonInt(payload.OpaquePixels) +
			" canvasW=" + jsonInt(payload.CanvasW) + " screen=(" + jsonInt(payload.ScreenX) + "," + jsonInt(payload.ScreenY) + ")" +
			" visible=" + jsonBool(payload.MeshVisible) + " opacity=" + jsonFloat(payload.MaterialOpacity) + "\n"
		sceneDiagOnce.Do(func() { _ = os.MkdirAll(filepath.Join(config.StorageDirectory(), "cache", "wallpaper-engine"), 0o755) })
		logPath := filepath.Join(config.StorageDirectory(), "cache", "wallpaper-engine", "scene-diag.log")
		f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		if err == nil {
			_, _ = f.WriteString(line)
			f.Close()
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

func jsonBool(v bool) string {
	if v { return "true" }
	return "false"
}

func jsonFloat(v float64) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func jsonInt(v int) string {
	b, _ := json.Marshal(v)
	return string(b)
}
