package download

import (
	"context"
	"fmt"
	"sync"
)

// 高速小文件下载器：Mods 依赖、整合包声明文件这类"数量多、单个小"的下载，
// 逐个串行下载会被每文件的 TLS 握手 / 重试 / 慢启动拖垮。这里用信号量把
// N 个文件并行起来（并发数 = 设置里的"并行下载线程数"，同一池子也限速、
// 共享暂停门），单文件内部的重试 / 断点续传 / 主备源切换仍由
// DownloadFileToPath 承担，本层不做字节级的多线程分片。

// SmallFileJob 单个零碎文件的下载作业。
type SmallFileJob struct {
	// Name 展示名（下载中心里可见，如 "sodium.jar" 或依赖路径）。
	Name string
	URL  string
	// TargetPath 最终落盘路径（含临时文件与原子重命名逻辑在 DownloadFileToPath 内）。
	TargetPath string
	// ExpectedSHA1 非空时校验下载结果（大小写不敏感）；不一致则删除下载物并记为失败。
	// 整合包清单（mrpack）为每个声明文件都带 sha1，校验能挡住残缺下载与
	// 被替换的内容——损坏的 mod jar 换上去等于把游戏弄坏。
	ExpectedSHA1 string
	// ExpectedSHA512 同上，但用更强的 SHA-512。两者都声明时都要通过；
	// 只声明 sha512 的包此前完全得不到校验，所以不能只看 SHA-1。
	ExpectedSHA512 string
}

// DownloadSmallFiles 并行下载一组小文件。返回与 jobs 等长的错误切片
// （成功位为 nil）；ctx 取消时未开始的作业直接记为 ctx.Err()。
// onFileProgress(index, downloaded, total) 在下载 goroutine 上回调，
// 调用方自行聚合（注意并发：同一时刻最多 workers 个文件在回调）。
func DownloadSmallFiles(
	ctx context.Context,
	jobs []SmallFileJob,
	workers int,
	onFileProgress func(index int, downloaded, total int64),
) []error {
	errs := make([]error, len(jobs))
	if len(jobs) == 0 {
		return errs
	}
	if workers < 1 {
		workers = 1
	}

	sem := make(chan struct{}, workers)
	var wg sync.WaitGroup
	for i := range jobs {
		// 先看整体取消：排队中的作业没必要再等信号量
		if ctx.Err() != nil {
			errs[i] = ctx.Err()

			continue
		}
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				errs[index] = ctx.Err()

				return
			}
			job := jobs[index]
			err := DownloadFileToPath(ctx, job.URL, job.TargetPath, func(downloaded, total int64) {
				if onFileProgress != nil {
					onFileProgress(index, downloaded, total)
				}
			})
			if err == nil {
				// 校验放在下载成功后：原子重命名已完成，此时的文件就是要被
				// 游戏加载的那一份，不一致直接删掉（verifyDeclaredHashes 负责）。
				err = verifyDeclaredHashes(job.TargetPath, job.ExpectedSHA1, job.ExpectedSHA512)
			}
			if err != nil {
				errs[index] = fmt.Errorf("%s：%w", job.Name, err)
			}
		}(i)
	}
	wg.Wait()

	return errs
}
