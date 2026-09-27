package download

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"nekolauncher/internal/tools"
)

// ModDownloadService Mod 文件下载服务。从 Modrinth CDN 下载 mod JAR / mrpack /
// 资源包等到指定路径。内置断点续传与自动重试：网络抖动中断后按 HTTP Range
// 从临时文件断点继续，最多重试 3 次；全部失败才向调用方返回错误。
// 对应 C# ModDownloadService。

// modDownloadAttemptTimeout 单次尝试的最大时长（含读取流）；超时视为瞬时失败，重试时断点续传。
const modDownloadAttemptTimeout = 10 * time.Minute

// modDownloadMaxAttempts 瞬时失败后的总尝试次数（首次 + 重试）。
const modDownloadMaxAttempts = 4

// ProgressBytes 进度回调（已下载字节数，总字节数）。对应 C# IProgress<(long, long)>。
// 回调在下载 goroutine 上触发；Wails 侧可转发 EventsEmit（见 PORTING_NOTES.md）。
type ProgressBytes func(downloaded, total int64)

// DownloadFileToPath 下载文件到指定路径。
// 先写入临时文件（<目标路径>.nya-download），全部完成后再原子移动到目标路径；
// 瞬时网络失败自动断点续传重试；最终失败或用户取消时清理临时文件。
func DownloadFileToPath(
	ctx context.Context,
	downloadURL, targetPath string,
	progress ProgressBytes,
) error {
	if strings.TrimSpace(downloadURL) == "" {
		return fmt.Errorf("downloadURL 不能为空")
	}
	if strings.TrimSpace(targetPath) == "" {
		return fmt.Errorf("targetPath 不能为空")
	}

	if directory := filepath.Dir(targetPath); strings.TrimSpace(directory) != "" {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			return err
		}
	}

	temporaryPath := targetPath + ".nya-download"
	for attempt := 1; ; attempt++ {
		err := downloadAttempt(ctx, downloadURL, temporaryPath, progress)
		if err == nil {
			// 下载成功后才替换目标文件（原子移动；Windows 上先删旧目标）
			tools.RemoveFileIfExists(targetPath)
			if moveErr := os.Rename(temporaryPath, targetPath); moveErr != nil {
				return moveErr
			}
			return nil
		}
		if attempt >= modDownloadMaxAttempts || !isTransientFailure(err, ctx) {
			tools.RemoveFileIfExists(temporaryPath)
			return err
		}
		// 网络抖动 / 超时：退避后从断点续传重试（临时文件保留）
		backoff := time.Duration(attempt*2) * time.Second
		if backoff > 6*time.Second {
			backoff = 6 * time.Second
		}
		select {
		case <-time.After(backoff):
		case <-ctx.Done():
			tools.RemoveFileIfExists(temporaryPath)
			return ctx.Err()
		}
	}
}

// downloadAttempt 单次下载尝试：若临时文件已有部分内容则用 Range 断点续传。
func downloadAttempt(
	ctx context.Context,
	downloadURL, temporaryPath string,
	progress ProgressBytes,
) error {
	attemptCtx, cancel := context.WithTimeout(ctx, modDownloadAttemptTimeout)
	defer cancel()

	resumeFrom := int64(0)
	if info, err := os.Stat(temporaryPath); err == nil {
		resumeFrom = info.Size()
	}

	req, err := http.NewRequestWithContext(attemptCtx, "GET", downloadURL, nil)
	if err != nil {
		return err
	}
	if resumeFrom > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", resumeFrom))
	}
	// 与 C# HttpCompletionOption.ResponseHeadersRead 一致：尽快拿到响应头后流式读取
	resp, err := longDownloadClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// 416：本地临时文件可能已完整（上次成功但移动前被打断）。
	// 但没有 If-Range 校验时，远端内容可能已更换且比临时文件短——
	// 只有临时文件长度与服务器报告的总长度一致才视为完成，否则删除断点重下
	if resp.StatusCode == http.StatusRequestedRangeNotSatisfiable {
		if remoteTotal := parseContentRangeTotal(resp.Header.Get("Content-Range")); remoteTotal >= 0 && resumeFrom == remoteTotal {
			return nil
		}
		// 断点与远端不一致：删档后报 rangeMismatchError，
		// DownloadFileToPath 会按瞬时失败处理，从零重新下载一次
		tools.RemoveFileIfExists(temporaryPath)
		return &rangeMismatchError{}
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &httpStatusError{StatusCode: resp.StatusCode}
	}

	var totalBytes int64
	var downloadedBase int64
	var destination *os.File
	if resp.StatusCode == http.StatusPartialContent {
		// 断点续传：Content-Range 携带完整长度与本次响应的起始偏移。
		// 远端文件在两次尝试之间被换掉时 CDN 会按新文件回这个区间，直接追加
		// 会拼出"前半段旧文件 + 后半段新文件"的损坏文件，还会被当成下载成功。
		contentRange := resp.Header.Get("Content-Range")
		if start := parseContentRangeStart(contentRange); start >= 0 && start != resumeFrom {
			tools.RemoveFileIfExists(temporaryPath)

			return &rangeMismatchError{}
		}
		totalBytes = parseContentRangeTotal(contentRange)
		downloadedBase = resumeFrom
		destination, err = os.OpenFile(temporaryPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	} else {
		// 服务器不支持 Range（返回 200 全量）：从头覆盖
		totalBytes = resp.ContentLength
		downloadedBase = 0
		destination, err = os.OpenFile(temporaryPath, os.O_TRUNC|os.O_CREATE|os.O_WRONLY, 0o644)
	}
	if err != nil {
		return err
	}
	defer destination.Close()

	buffer := make([]byte, 128*1024)
	downloaded := downloadedBase
	for {
		// 全局暂停门：暂停期间连接保持、速度归零，恢复后原连接继续
		if err := WaitPauseGate(attemptCtx); err != nil {
			return err
		}
		// 全局限速：先申请配额，再按配额大小读（低速档下 128KB 的读块会超发）
		chunk := buffer[:downloadLimiter.permitReadSize(len(buffer))]
		if err := downloadLimiter.wait(attemptCtx, int64(len(chunk))); err != nil {
			return err
		}
		read, err := resp.Body.Read(chunk)
		if read > 0 {
			if _, writeErr := destination.Write(buffer[:read]); writeErr != nil {
				return writeErr
			}
			downloaded += int64(read)
			if progress != nil {
				progress(downloaded, totalBytes)
			}
		}
		if err == io.EOF {
			// 收尾校验：连接干净结束但长度对不上（服务器截断 / 中间被换文件）
			// 同样算失败，绝不能把残缺文件改名成正式文件。
			if totalBytes > 0 && downloaded != totalBytes {
				tools.RemoveFileIfExists(temporaryPath)

				return &rangeMismatchError{}
			}

			return nil
		}
		if err != nil {
			return err
		}
	}
	// 中断时由 defer Close 自动冲刷已写入部分，
	// 保证临时文件始终是有效前缀，重试可从断点继续。
}

// parseContentRangeStart 从 "bytes 123-456/789" 中解析本次响应的起始偏移；无信息返回 -1。
func parseContentRangeStart(header string) int64 {
	idx := strings.Index(header, " ")
	if idx < 0 {
		return -1
	}
	rangePart := header[idx+1:]
	if dash := strings.Index(rangePart, "-"); dash > 0 {
		rangePart = rangePart[:dash]
	}
	var start int64
	if _, err := fmt.Sscanf(rangePart, "%d", &start); err != nil {
		return -1
	}

	return start
}

// parseContentRangeTotal 从 "bytes 123-456/789" 中解析完整长度；无信息返回 -1。
func parseContentRangeTotal(header string) int64 {
	idx := strings.LastIndex(header, "/")
	if idx < 0 || idx == len(header)-1 {
		return -1
	}
	if strings.HasSuffix(header[idx+1:], "*") {
		return -1
	}
	var total int64
	if _, err := fmt.Sscanf(header[idx+1:], "%d", &total); err != nil {
		return -1
	}
	return total
}

// isTransientFailure 判断错误是否为可重试的瞬时网络失败。
// 用户主动取消（外部 ctx 已触发）不算瞬时失败，直接返回让上层显示"已取消"；
// 4xx 客户端错误（404 文件不存在、403 被拒绝等）重试也不会成功，直接失败，
// 仅保留 408/429 这两个可重试的客户端错误。
func isTransientFailure(err error, ctx context.Context) bool {
	if ctx.Err() != nil {
		return false
	}
	if statusErr := (*httpStatusError)(nil); errors.As(err, &statusErr) {
		// 仅 408（Request Timeout）/ 429（Too Many Requests）可重试
		return statusErr.StatusCode == http.StatusRequestTimeout ||
			statusErr.StatusCode == http.StatusTooManyRequests
	}
	var rangeMismatch *rangeMismatchError
	if errors.As(err, &rangeMismatch) {
		// 断点已删档重置，下一次尝试从头完整下载
		return true
	}
	// attemptCtx 超时（DeadlineExceeded 且外部 ctx 未触发）= 单次尝试超时
	return errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, io.EOF) ||
		errors.Is(err, io.ErrUnexpectedEOF) ||
		isNetError(err)
}

// isNetError 网络层错误（连接重置 / DNS / 握手失败等）视为瞬时失败。
func isNetError(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr)
}
