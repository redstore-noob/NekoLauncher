// 服务器创建：下载核心 jar（Vanilla/Paper/Fabric 直接下载，NeoForge 走
// 安装器），写入 eula.txt 与初始 server.properties。
//
// 下载源：
//   - Vanilla   piston-meta.mojang.com（版本清单 → server.jar）
//   - Paper     api.papermc.io v2（版本 → 最新 build → application 下载名）
//   - Fabric    meta.fabricmc.net v2（最新 loader + installer → server/jar）
//   - NeoForge  maven.neoforged.net（按 MC 版本前缀过滤 → installer --installServer）
package mcserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"nekolauncher/internal/logs"
)

var httpClient = &http.Client{Timeout: 10 * time.Minute}

// CreateServer 创建服务器：目录、核心 jar、eula.txt、初始配置。
// AcceptEULA 必须为 true（前端弹窗确认）。
func CreateServer(ctx context.Context, opts CreateOptions) (string, error) {
	if !opts.AcceptEULA {
		return "", errors.New("未同意 Minecraft EULA，无法创建服务器")
	}
	if strings.TrimSpace(opts.Name) == "" || strings.TrimSpace(opts.MCVersion) == "" {
		return "", errors.New("参数无效")
	}
	switch opts.Core {
	case CoreVanilla, CorePaper, CoreNeoForge, CoreFabric:
	default:
		return "", fmt.Errorf("不支持的服务器核心：%s", opts.Core)
	}
	if opts.Port <= 0 {
		opts.Port = 25565
	}
	if opts.MaxPlayers <= 0 {
		opts.MaxPlayers = 20
	}

	name := sanitizeServerName(opts.Name)
	if name == "" {
		name = "server"
	}
	id := uniqueServerDirectory(name)
	dir := filepath.Join(ServerRootDirectory(), id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}

	cfg := &ServerConfig{
		ID:          id,
		Name:        opts.Name,
		Core:        opts.Core,
		CoreVersion: strings.TrimSpace(opts.CoreVersion),
		MCVersion:   strings.TrimSpace(opts.MCVersion),
		Port:        opts.Port,
		MaxPlayers:  opts.MaxPlayers,
		JavaPath:    strings.TrimSpace(opts.JavaPath),
	}

	var err error
	switch opts.Core {
	case CoreVanilla:
		err = installVanilla(ctx, dir, cfg)
	case CorePaper:
		err = installPaper(ctx, dir, cfg)
	case CoreFabric:
		err = installFabric(ctx, dir, cfg)
	case CoreNeoForge:
		err = installNeoForge(ctx, dir, cfg)
	}
	if err != nil {
		_ = os.RemoveAll(dir)

		return "", err
	}

	// 核心已装好之后的失败同样回滚：不回滚的话会留下一个没有 server.json 的
	// 目录——列表不可见、删不掉、同名校验还让下次创建的名字带 -1。
	if err := writeEula(dir); err != nil {
		_ = os.RemoveAll(dir)

		return "", err
	}
	if err := writeInitialProperties(dir, cfg.Port, cfg.MaxPlayers, cfg.Name); err != nil {
		_ = os.RemoveAll(dir)

		return "", err
	}
	// 顺手配好 RCON（随机密码 + 默认端口）：在线人数与停止指令优先走它，
	// 比解析 stdout 稳。写失败不影响创建——退化成 stdin 路径而已。
	if _, err := ensureRCONProperties(dir); err != nil {
		logsWriteCreate("为服务器 %s 写入 RCON 配置失败：%v（将退回 stdin 控制台路径）", id, err)
	}
	if err := saveServerConfig(dir, cfg); err != nil {
		_ = os.RemoveAll(dir)

		return "", err
	}

	return id, nil
}

// logsWriteCreate 创建流程的日志（失败不打断创建）。
func logsWriteCreate(format string, args ...any) {
	logs.Write("WARN", fmt.Sprintf(format, args...))
}

// httpGetJSON GET 并解析 JSON。
func httpGetJSON(ctx context.Context, url string, target any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d：%s", resp.StatusCode, url)
	}

	return json.NewDecoder(resp.Body).Decode(target)
}

// httpDownload GET 文件并落盘（先写 .part 再改名）。
func httpDownload(ctx context.Context, url, target string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d：%s", resp.StatusCode, url)
	}

	part := target + ".part"
	file, err := os.Create(part)
	if err != nil {
		return err
	}
	if _, err := io.Copy(file, resp.Body); err != nil {
		file.Close()
		_ = os.Remove(part)

		return err
	}
	if err := file.Close(); err != nil {
		return err
	}

	return os.Rename(part, target)
}

// installVanilla Mojang 官方 server.jar。
func installVanilla(ctx context.Context, dir string, cfg *ServerConfig) error {
	var manifest struct {
		Versions []struct {
			ID  string `json:"id"`
			URL string `json:"url"`
		} `json:"versions"`
	}
	if err := httpGetJSON(ctx,
		"https://piston-meta.mojang.com/mc/game/version_manifest_v2.json", &manifest); err != nil {
		return err
	}
	var versionURL string
	for _, v := range manifest.Versions {
		if v.ID == cfg.MCVersion {
			versionURL = v.URL

			break
		}
	}
	if versionURL == "" {
		return fmt.Errorf("未找到 Minecraft 版本：%s", cfg.MCVersion)
	}

	var meta struct {
		Downloads struct {
			Server struct {
				URL string `json:"url"`
			} `json:"server"`
		} `json:"downloads"`
	}
	if err := httpGetJSON(ctx, versionURL, &meta); err != nil {
		return err
	}
	if meta.Downloads.Server.URL == "" {
		return errors.New("版本元数据中没有 server 下载地址")
	}

	return httpDownload(ctx, meta.Downloads.Server.URL, filepath.Join(dir, "server.jar"))
}

// installPaper PaperMC fill v3 API：指定 build（空则 latest）的下载产物。
func installPaper(ctx context.Context, dir string, cfg *ServerConfig) error {
	buildRef := "latest"
	if cfg.CoreVersion != "" {
		buildRef = cfg.CoreVersion
	}
	var build struct {
		Downloads map[string]struct {
			Name string `json:"name"`
			URL  string `json:"url"`
		} `json:"downloads"`
	}
	endpoint := "https://fill.papermc.io/v3/projects/paper/versions/" +
		cfg.MCVersion + "/builds/" + buildRef
	if err := httpGetJSON(ctx, endpoint, &build); err != nil {
		return err
	}
	for _, download := range build.Downloads {
		if download.URL == "" {
			continue
		}

		return httpDownload(ctx, download.URL, filepath.Join(dir, "paper.jar"))
	}

	return errors.New("Paper 构建缺少下载地址")
}

// installFabric Fabric meta：最新 loader + 最新 installer 的 server/jar
// （自带启动器，可独立运行）。
func installFabric(ctx context.Context, dir string, cfg *ServerConfig) error {
	var loaders []struct {
		Loader struct {
			Version string `json:"version"`
		} `json:"loader"`
	}
	if err := httpGetJSON(ctx,
		"https://meta.fabricmc.net/v2/versions/loader/"+cfg.MCVersion, &loaders); err != nil {
		return err
	}
	if len(loaders) == 0 || loaders[0].Loader.Version == "" {
		return fmt.Errorf("Fabric 没有 %s 的可用加载器", cfg.MCVersion)
	}

	// 新版 meta 条目不再内嵌 installer，单独取最新安装器版本
	var installers []struct {
		Version string `json:"version"`
	}
	if err := httpGetJSON(ctx,
		"https://meta.fabricmc.net/v2/versions/installer", &installers); err != nil {
		return err
	}
	if len(installers) == 0 || installers[0].Version == "" {
		return errors.New("Fabric 没有可用的安装器版本")
	}

	// 用户指定了 loader 版本则用之（列表来自同一 API，必然存在）；
	// 否则用最新 loader。安装器版本单独取最新。
	loaderVersion := cfg.CoreVersion
	if loaderVersion == "" {
		loaderVersion = loaders[0].Loader.Version
	}
	cfg.CoreVersion = loaderVersion
	url := strings.Join([]string{
		"https://meta.fabricmc.net/v2/versions/loader",
		cfg.MCVersion,
		loaderVersion,
		installers[0].Version,
		"server/jar",
	}, "/")

	return httpDownload(ctx, url, filepath.Join(dir, "fabric-server.jar"))
}

// installNeoForge 下载 installer 并执行 --installServer（需要 Java，耗时数分钟）。
// 指定 CoreVersion 时安装该版本，否则用该 MC 版本的最新 NeoForge。
func installNeoForge(ctx context.Context, dir string, cfg *ServerConfig) error {
	if cfg.CoreVersion != "" {
		return installNeoForgeVersion(ctx, dir, cfg)
	}

	// MC "1.21.1" → NeoForge 版本前缀 "21.1."；日期式版本 "26.1" → "26.1."
	//（原来对不带 "1." 的版本直接报错，26.x 永远建不出 NeoForge 服务端）
	prefix, ok := neoforgeVersionPrefix(cfg.MCVersion)
	if !ok {
		return fmt.Errorf("NeoForge 不支持 Minecraft %s", cfg.MCVersion)
	}

	var versions struct {
		Versions []string `json:"versions"`
	}
	if err := httpGetJSON(ctx,
		"https://maven.neoforged.net/api/maven/versions/releases/net/neoforged/neoforge",
		&versions); err != nil {
		return err
	}
	var latest string
	for _, v := range versions.Versions {
		if strings.HasPrefix(v, prefix) {
			latest = v // API 返回升序，取最后一个匹配
		}
	}
	if latest == "" {
		return fmt.Errorf("NeoForge 没有 Minecraft %s 的可用版本", cfg.MCVersion)
	}
	cfg.CoreVersion = latest

	return installNeoForgeVersion(ctx, dir, cfg)
}

// installNeoForgeVersion 按 cfg.CoreVersion 执行安装。
func installNeoForgeVersion(ctx context.Context, dir string, cfg *ServerConfig) error {
	latest := cfg.CoreVersion

	jar := filepath.Join(dir, "neoforge-installer.jar")
	url := fmt.Sprintf(
		"https://maven.neoforged.net/releases/net/neoforged/neoforge/%s/neoforge-%s-installer.jar",
		latest, latest,
	)
	if err := httpDownload(ctx, url, jar); err != nil {
		return err
	}

	java, err := resolveJavaForServer(ctx, cfg, nil)
	if err != nil {
		return err
	}
	installCtx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	cmd := execCommand(installCtx, java, "-jar", "neoforge-installer.jar", "--installServer")
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("NeoForge 安装器执行失败：%w\n%s", err, tailLines(string(output), 20))
	}
	_ = os.Remove(jar)

	// 安装器生成的启动参数文件是启动入口，名为 win_args.txt（Windows）或
	// unix_args.txt（其它平台）——两个都认，否则非 Windows 上明明装好了却报错。
	argsDirectory := filepath.Join(dir, "libraries", "net", "neoforged", "neoforge", latest)
	found := false
	for _, name := range []string{"win_args.txt", "unix_args.txt"} {
		if _, err := os.Stat(filepath.Join(argsDirectory, name)); err == nil {
			found = true

			break
		}
	}
	if !found {
		return errors.New("NeoForge 安装完成但未找到启动参数文件")
	}

	return nil
}

// tailLines 取文本最后 n 行（错误信息截断用）。
func tailLines(text string, n int) string {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}

	return strings.Join(lines, "\n")
}
