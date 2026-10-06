package main

// 场景/网页壁纸渲染验证服务(开发工具,不参与正式构建):
//
//	go run ./internal/bindings/scene_harness_main [壁纸项目目录]
//
//	1. 按指定目录(缺省为 WE 当前选中壁纸)pin 资源目标:
//	   scene 类型 → /wescene,web 类型 → /wwwallpaper;
//  2. 在 127.0.0.1:8788 起资源路由 + harness 静态页;
//  3. 浏览器打开对应地址即可脱离 Wails 验证渲染链路:
//	   http://127.0.0.1:8788/          场景壁纸(three.js harness 页)
//	   http://127.0.0.1:8788/web.html  网页壁纸(iframe 承载)

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"

	"nekolauncher/internal/bindings"
)

func main() {
	api := bindings.New()
	var wallpaper *bindings.WallpaperEngineWallpaper
	var err error
	if len(os.Args) > 1 {
		// 调试模式:直接指定壁纸项目目录(不依赖 WE 当前选择)
		wallpaper, err = bindings.BuildSceneWallpaperForHarness(os.Args[1])
	} else {
		wallpaper, err = api.System.GetWallpaperEngineWallpaper()
	}
	if err != nil {
		fmt.Println("读取 WE 壁纸失败:", err)
		os.Exit(1)
	}

	os.MkdirAll("harness", 0o755)
	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.Dir("harness")))
	// 复用应用内的回退路由:/wescene(场景)与 /wwwallpaper(网页)都在里面
	fallback := bindings.NewAssetFallbackHandler()
	mux.Handle("/wescene/", fallback)
	mux.Handle("/wwwallpaper/", fallback)

	switch {
	case wallpaper.Scene != nil:
		payloadJSON, _ := json.Marshal(wallpaper.Scene)
		os.WriteFile("harness/payload.json", payloadJSON, 0o644)
		fmt.Printf("场景载荷就绪:设计 %dx%d,对象 %d 个,属性 %d 项 → http://127.0.0.1:8788/\n",
			wallpaper.Scene.DesignWidth, wallpaper.Scene.DesignHeight,
			len(wallpaper.Scene.Objects), len(wallpaper.Scene.GeneralProperties))
	case wallpaper.Web != "":
		fmt.Printf("网页壁纸就绪:入口 %s(属性指纹 %s)→ http://127.0.0.1:8788/web.html\n",
			wallpaper.Web, wallpaper.WebConfigVersion)
	default:
		fmt.Printf("壁纸不是场景/网页类型(Type=%s),无法验证\n", wallpaper.Type)
		os.Exit(1)
	}

	fmt.Println("harness 运行在 http://127.0.0.1:8788/")
	if err := http.ListenAndServe("127.0.0.1:8788", mux); err != nil {
		fmt.Println("服务失败:", err)
		os.Exit(1)
	}
}
