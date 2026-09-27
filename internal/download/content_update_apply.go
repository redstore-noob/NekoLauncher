package download

// content_update_apply.go 把「检查到的新版本」真正落地到磁盘。
//
// 为什么单独成文件：这是本功能里唯一会**覆盖用户文件**的操作，规则必须写死在一处：
//  1. 先下载到临时文件，校验 SHA-1（与检查阶段拿到的哈希口径一致）；
//  2. 校验通过才给原文件改名留备份（<文件名>.bak-<时间戳>，不覆盖旧备份）；
//  3. 最后一步才把新文件移到目标路径；任何一步失败都把原文件改回去。
//
// 绝不静默覆盖：返回值里带上备份路径，前端必须把它显示给用户。

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ContentUpdateApplyResult 落地结果。
type ContentUpdateApplyResult struct {
	// TargetPath 被替换的目标文件路径。
	TargetPath string
	// BackupPath 原文件的备份路径（空串 = 没有备份，例如目标原本不存在）。
	BackupPath string
	// SHA1 新文件的 SHA-1（与请求里的期望值一致）。
	SHA1 string
	// SizeBytes 新文件大小。
	SizeBytes int64
	// Message 面向用户的中文结果说明。
	Message string
}

// DownloadContentUpdate 把指定下载地址保存到 targetPath（不做任何替换/备份）。
// expectedSHA1 非空时会校验下载结果，不一致则删除下载物并报错。
// 用于「另存为新版本」这类不改动原文件的场景。
func DownloadContentUpdate(
	ctx context.Context,
	downloadURL, targetPath, expectedSHA1 string,
	progress ProgressBytes,
) (*ContentUpdateApplyResult, error) {
	if strings.TrimSpace(downloadURL) == "" {
		return nil, fmt.Errorf("下载地址不能为空")
	}
	if strings.TrimSpace(targetPath) == "" {
		return nil, fmt.Errorf("保存路径不能为空")
	}

	if err := DownloadFileToPath(ctx, downloadURL, targetPath, progress); err != nil {
		return nil, err
	}

	hash, size, err := verifyDownloadedFile(targetPath, expectedSHA1)
	if err != nil {
		return nil, err
	}
	return &ContentUpdateApplyResult{
		TargetPath: targetPath,
		SHA1:       hash,
		SizeBytes:  size,
		Message:    fmt.Sprintf("已保存到 %s（%s）。", targetPath, formatByteSize(size)),
	}, nil
}

// ApplyContentUpdate 用新版本替换 targetPath 指向的文件：
// 先备份原文件，再落地新文件；失败时回滚。
//
// 只替换文件本身，不碰同目录的其它文件（禁用态 .disabled 文件也会保留原后缀语义：
// 目标路径由调用方给出，通常是列表里那一条的 SourcePath）。
func ApplyContentUpdate(
	ctx context.Context,
	downloadURL, targetPath, expectedSHA1 string,
	progress ProgressBytes,
) (*ContentUpdateApplyResult, error) {
	if strings.TrimSpace(downloadURL) == "" {
		return nil, fmt.Errorf("下载地址不能为空")
	}
	if strings.TrimSpace(targetPath) == "" {
		return nil, fmt.Errorf("目标文件路径不能为空")
	}

	// ---- 1) 先下载到临时文件（与目标同目录，保证后续改名是同一卷内的原子操作）----
	temporaryPath := targetPath + ".nya-update"
	defer func() { _ = os.Remove(temporaryPath) }()

	if err := DownloadFileToPath(ctx, downloadURL, temporaryPath, progress); err != nil {
		return nil, err
	}
	hash, size, err := verifyDownloadedFile(temporaryPath, expectedSHA1)
	if err != nil {
		return nil, err
	}

	result := &ContentUpdateApplyResult{
		TargetPath: targetPath,
		SHA1:       hash,
		SizeBytes:  size,
	}

	// ---- 2) 原文件存在才备份；备份名带时间戳，不覆盖以前留下的备份 ----
	hadOriginal := false
	if info, statErr := os.Stat(targetPath); statErr == nil && !info.IsDir() {
		backupPath := backupPathFor(targetPath, time.Now())
		if renameErr := os.Rename(targetPath, backupPath); renameErr != nil {
			return nil, fmt.Errorf("备份原文件失败（未做任何改动）：%w", renameErr)
		}
		hadOriginal = true
		result.BackupPath = backupPath
	}

	// ---- 3) 落地新文件；失败则把备份改回去 ----
	if renameErr := os.Rename(temporaryPath, targetPath); renameErr != nil {
		if hadOriginal {
			if restoreErr := os.Rename(result.BackupPath, targetPath); restoreErr != nil {
				return nil, fmt.Errorf("替换失败（%v），且原文件回滚失败（%v）：请手动把 %s 改回 %s",
					renameErr, restoreErr, result.BackupPath, targetPath)
			}
			result.BackupPath = ""
		}
		return nil, fmt.Errorf("替换失败，原文件已恢复：%w", renameErr)
	}

	if hadOriginal {
		result.Message = fmt.Sprintf("已替换 %s（原文件备份为 %s）。",
			filepath.Base(targetPath), filepath.Base(result.BackupPath))
	} else {
		result.Message = fmt.Sprintf("已写入 %s（原本不存在同名文件，无备份）。", filepath.Base(targetPath))
	}
	return result, nil
}

// verifyDownloadedFile 校验下载结果：返回 SHA-1 与文件大小。
// expectedSHA1 非空且不一致时删除文件并报错（残缺/被篡改的包换上去等于把游戏弄坏）。
func verifyDownloadedFile(path, expectedSHA1 string) (string, int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", 0, fmt.Errorf("下载结果不可读：%w", err)
	}
	hash, err := computeFileSHA1(path)
	if err != nil {
		return "", 0, fmt.Errorf("校验下载文件失败：%w", err)
	}
	wanted := strings.ToLower(strings.TrimSpace(expectedSHA1))
	if wanted != "" && hash != wanted {
		_ = os.Remove(path)
		return "", 0, fmt.Errorf("下载文件校验失败（SHA-1 不一致，已删除下载物）：期望 %s，实际 %s", wanted, hash)
	}
	return hash, info.Size(), nil
}

// backupPathFor 备份文件名：<原名>.bak-<yyyyMMddHHmmss>；同一秒内重名时补序号。
func backupPathFor(targetPath string, now time.Time) string {
	base := fmt.Sprintf("%s.bak-%s", targetPath, now.Format("20060102150405"))
	candidate := base
	for index := 1; index < 100; index++ {
		if !fileExists(candidate) {
			return candidate
		}
		candidate = fmt.Sprintf("%s-%d", base, index)
	}
	return candidate
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
