package instance

// 实例复制：把 versions/<id> 整体拷贝为 versions/<新id>，修补副本版本 JSON
// 的 id 字段，并克隆实例档案（内存/窗口/JVM 参数/隔离/图标偏好等）。
// 与重命名（renameservice.go）共用校验与 JSON 工具。
//
// 与重命名的关键差异：不修补 inheritsFrom / jar 引用——原版本仍在，
// 兄弟版本与副本的继承关系都应保持原样指向原父版本；
// 需要重命名的只有副本自己的 id 与 json/jar 文件名。

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"nekolauncher/internal/config"
	"nekolauncher/internal/tools"
)

// CopyVersion 复制版本目录与实例档案，返回新版本 ID。
// 外部导入的实例（versions 下没有对应目录）不支持复制。
func CopyVersion(ctx context.Context, minecraftDirectory, sourceVersionID, requestedNewID string) (string, error) {
	newVersionID, err := validateVersionID(requestedNewID)
	if err != nil {
		return "", err
	}
	if strings.EqualFold(strings.TrimSpace(sourceVersionID), newVersionID) {
		return "", errors.New("新版本名称与原版本相同。")
	}

	versionsDirectory := filepath.Join(mustAbs(minecraftDirectory), "versions")
	sourceDirectory, err := resolveContainedDirectory(versionsDirectory, sourceVersionID)
	if err != nil {
		return "", err
	}
	targetDirectory, err := resolveContainedDirectory(versionsDirectory, newVersionID)
	if err != nil {
		return "", err
	}
	if info, err := os.Stat(sourceDirectory); err != nil || !info.IsDir() {
		return "", fmt.Errorf("原版本文件夹不存在（外部导入的实例暂不支持复制）：%s", sourceDirectory)
	}
	if pathExists(targetDirectory) {
		return "", fmt.Errorf("版本名称“%s”已存在。", newVersionID)
	}
	sourceJSONPath := filepath.Join(sourceDirectory, sourceVersionID+".json")
	if !tools.FileExists(sourceJSONPath) {
		return "", fmt.Errorf("原版本 JSON 不存在：%s", sourceJSONPath)
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}

	// 目录整体拷贝；失败即清理半成品（复制是纯新增操作，不碰原目录，无需回滚原状）
	if err := copyTree(ctx, sourceDirectory, targetDirectory); err != nil {
		_ = os.RemoveAll(targetDirectory)
		return "", err
	}
	if err := patchCopiedVersionJSON(ctx, targetDirectory, sourceVersionID, newVersionID); err != nil {
		_ = os.RemoveAll(targetDirectory)
		return "", err
	}
	cloneVersionProfile(minecraftDirectory, sourceVersionID, newVersionID)
	return newVersionID, nil
}

// patchCopiedVersionJSON 修补副本版本 JSON：id 改为新版本 ID，文件名同步改名为
// <新ID>.json / <新ID>.jar。inheritsFrom / jar 字段刻意不动（见文件头注释）。
func patchCopiedVersionJSON(ctx context.Context, targetDirectory, sourceVersionID, newVersionID string) error {
	jsonPath := filepath.Join(targetDirectory, sourceVersionID+".json")
	root, err := tryParseJSONObject(jsonPath)
	if err != nil || root == nil {
		return errors.New("无法读取所选版本 JSON。")
	}
	root["id"] = newVersionID
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := writeJSONAtomically(jsonPath, root); err != nil {
		return err
	}
	if err := os.Rename(jsonPath, filepath.Join(targetDirectory, newVersionID+".json")); err != nil {
		return err
	}
	sourceJarPath := filepath.Join(targetDirectory, sourceVersionID+".jar")
	if tools.FileExists(sourceJarPath) {
		if err := os.Rename(sourceJarPath, filepath.Join(targetDirectory, newVersionID+".jar")); err != nil {
			return err
		}
	}
	return nil
}

// cloneVersionProfile 克隆实例档案：副本继承原实例的全部启动设置。
// 保存校验失败（理论上默认值不会失败）时静默跳过——副本以默认设置启动，
// 不值得因此报错回滚一个已经复制完成的实例。
func cloneVersionProfile(minecraftDirectory, sourceVersionID, newVersionID string) {
	profile := config.Get(minecraftDirectory, sourceVersionID)
	profile.VersionId = newVersionID
	_ = config.Save(profile)
}

// copyTree 递归复制目录树；单个文件失败即中止（调用方整体清理半成品）。
func copyTree(ctx context.Context, sourceDirectory, targetDirectory string) error {
	return filepath.WalkDir(sourceDirectory, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		relative, err := filepath.Rel(sourceDirectory, path)
		if err != nil {
			return err
		}
		destination := filepath.Join(targetDirectory, relative)
		if entry.IsDir() {
			return os.MkdirAll(destination, 0o755)
		}
		if !entry.Type().IsRegular() {
			// 符号链接等非常规条目跳过：复制链接目标可能越出实例目录
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		return copyFileContents(path, destination, info)
	})
}

// copyFileContents 拷贝单个文件（保留权限位）。
func copyFileContents(sourcePath, destinationPath string, info os.FileInfo) error {
	source, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer source.Close()
	if err := os.MkdirAll(filepath.Dir(destinationPath), 0o755); err != nil {
		return err
	}
	destination, err := os.OpenFile(destinationPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, info.Mode().Perm())
	if err != nil {
		return err
	}
	defer destination.Close()
	_, err = io.Copy(destination, source)
	return err
}
