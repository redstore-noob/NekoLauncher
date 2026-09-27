package bindings

// 插件清单的图形化编辑支持：插件页的「插件制作」把清单字段做成表单，
// 新建骨架与保存编辑都走这里（只改元数据与入口模板，不动已有代码与图标文件）。
// 前端传入 JSON 表单载荷，落盘统一为 plugin.yaml（作者手编辑友好、可写注释）。

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SavePluginManifest 把编辑后的清单写回插件目录的 plugin.yaml。
// manifestJSON 必须是完整清单（含 id 等全部字段）；id 不允许改动——
// id 与插件目录一一对应，改名等于换插件，请卸载后重新安装。
func (a *PluginAPI) SavePluginManifest(sourceDirectory, manifestJSON string) error {
	source := filepath.Clean(strings.TrimSpace(sourceDirectory))
	if stat, err := os.Stat(source); err != nil || !stat.IsDir() {
		return fmt.Errorf("插件目录不可用：%s", sourceDirectory)
	}

	// 以磁盘上的既有清单为基准校验，防止把 A 插件的目录改写成 B 的 id
	current, err := readPluginManifest(source)
	if err != nil {
		return err
	}
	var manifest pluginManifest
	if err := json.Unmarshal([]byte(manifestJSON), &manifest); err != nil {
		return fmt.Errorf("%s 不是合法 JSON：%w", pluginManifestName, err)
	}
	if manifest.ID != current.ID {
		return fmt.Errorf("插件 id 不可修改（当前为 %s）", current.ID)
	}
	if strings.TrimSpace(manifest.Name) == "" {
		return errors.New("插件名称不能为空")
	}
	if strings.TrimSpace(manifest.APIVersion) == "" {
		return errors.New("apiVersion 不能为空")
	}
	if err := validatePluginID(manifest.ID); err != nil {
		return err
	}

	raw, err := writePluginManifestYAML(manifest)
	if err != nil {
		return fmt.Errorf("序列化 %s 失败：%w", pluginManifestName, err)
	}

	return os.WriteFile(filepath.Join(source, pluginManifestName), raw, 0o644)
}

// CreatePluginScaffold 从零创建插件骨架：在 parentDirectory 下新建 <id>/
// 目录，写入 plugin.yaml 与入口文件（entryContent 为前端给的入门模板），
// 返回创建的插件目录。已存在同名目录时拒绝，避免覆盖别人的插件。
func (a *PluginAPI) CreatePluginScaffold(parentDirectory, manifestJSON, entryContent string) (string, error) {
	parent := filepath.Clean(strings.TrimSpace(parentDirectory))
	if stat, err := os.Stat(parent); err != nil || !stat.IsDir() {
		return "", fmt.Errorf("创建位置不可用：%s", parentDirectory)
	}

	var manifest pluginManifest
	if err := json.Unmarshal([]byte(manifestJSON), &manifest); err != nil {
		return "", fmt.Errorf("%s 不是合法 JSON：%w", pluginManifestName, err)
	}
	if strings.TrimSpace(manifest.Name) == "" {
		return "", errors.New("插件名称不能为空")
	}
	if strings.TrimSpace(manifest.APIVersion) == "" {
		return "", errors.New("apiVersion 不能为空")
	}
	if err := validatePluginID(manifest.ID); err != nil {
		return "", err
	}

	target := filepath.Join(parent, manifest.ID)
	if _, err := os.Stat(target); err == nil {
		return "", fmt.Errorf("目录已存在：%s", target)
	}
	if err := os.MkdirAll(target, 0o755); err != nil {
		return "", fmt.Errorf("创建插件目录失败：%w", err)
	}

	raw, err := writePluginManifestYAML(manifest)
	if err != nil {
		return "", fmt.Errorf("序列化 %s 失败：%w", pluginManifestName, err)
	}
	if err := os.WriteFile(filepath.Join(target, pluginManifestName), raw, 0o644); err != nil {
		return "", fmt.Errorf("写入 %s 失败：%w", pluginManifestName, err)
	}

	entry := filepath.FromSlash(manifest.entryFile())
	entryPath := filepath.Join(target, entry)
	// 入口文件名带子目录时先建目录（罕见，但别让用户撞墙）
	if dir := filepath.Dir(entryPath); dir != target {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", fmt.Errorf("创建入口目录失败：%w", err)
		}
	}
	if err := os.WriteFile(entryPath, []byte(entryContent), 0o644); err != nil {
		return "", fmt.Errorf("写入入口文件失败：%w", err)
	}
	if err := os.MkdirAll(filepath.Join(target, "assets"), 0o755); err != nil {
		return "", fmt.Errorf("创建 assets 目录失败：%w", err)
	}

	return target, nil
}
