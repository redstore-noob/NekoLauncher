package bindings

import (
	"encoding/json"
	"fmt"
	"testing"
)

func TestProbeWEScene(t *testing.T) {
	dir, file, err := wallpaperEngineSelectedProject()
	if err != nil || dir == "" {
		// 该测试探测的是真实安装的 Wallpaper Engine，属环境依赖，
		// 没装 WE（如 Linux CI 容器）时跳过而非失败。
		t.Skip("未找到 Wallpaper Engine 安装，跳过：", err)
	}
	scene := weBuildScenePayload(dir, dir, file)
	if scene == nil {
		t.Fatal("scene payload nil")
	}
	b, _ := json.MarshalIndent(scene, "", " ")
	s := string(b)
	fmt.Println("payload bytes:", len(s))
	fmt.Println(s[:min(3000, len(s))])
}
