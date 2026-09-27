package download

import (
	"context"
	"sync"
	"time"
)

// 全局下载限速。
//
// 语义：用户填的是**总带宽上限**（KB/s），所有并发下载共享一个配额——
// 每条连接各自限速在老版本 Minecraft 那种"几百个小文件"的场景下等于没限。
//
// 实现是"按秒记账"的令牌桶：一个 1 秒窗口内累计放行 rate 字节，额度用完就睡到
// 窗口结束。相比逐字节的令牌桶，好处是没有额外 goroutine、没有突发抖动，
// 且读块大小会被压到不超过单窗口额度（见 PermitReadSize），低速档也能限准。
type speedLimiter struct {
	mu    sync.Mutex
	rate  int64 // 字节/秒；0 = 不限速
	start time.Time
	used  int64
}

var downloadLimiter = &speedLimiter{}

// speedLimitMinimumKbps 限速下限（1 KB/s）。再低没有实际意义，反而会把
// 每窗口的读块压到几十字节，白白放大系统调用次数。
const speedLimitMinimumKbps = 1

// speedLimitMaximumKbps 限速上限（1 GB/s，等价于不限速）。
const speedLimitMaximumKbps = 1024 * 1024

// setSpeedLimitKbps 设置全局限速（KB/s，0 = 不限速）。
func (l *speedLimiter) setSpeedLimitKbps(kbps int) {
	if kbps < 0 {
		kbps = 0
	}
	if kbps > 0 && kbps < speedLimitMinimumKbps {
		kbps = speedLimitMinimumKbps
	}
	if kbps > speedLimitMaximumKbps {
		kbps = speedLimitMaximumKbps
	}

	l.mu.Lock()
	l.rate = int64(kbps) * 1024
	l.start = time.Time{}
	l.used = 0
	l.mu.Unlock()
}

// speedLimitKbps 当前限速值（KB/s，0 = 不限速）。
func (l *speedLimiter) speedLimitKbps() int {
	l.mu.Lock()
	defer l.mu.Unlock()

	return int(l.rate / 1024)
}

// permitReadSize 本次最多读多少字节：
//   - 不限速时用调用方的缓冲区大小；
//   - 限速时不超过"一个窗口的额度"，否则低速档会被单次大读突破
//     （128KB 的读块在 32KB/s 下等于一秒钟就放行了 4 倍额度）。
func (l *speedLimiter) permitReadSize(bufferSize int) int {
	l.mu.Lock()
	rate := l.rate
	l.mu.Unlock()

	if rate <= 0 || rate >= int64(bufferSize) {
		return bufferSize
	}
	if rate < 8*1024 {
		// 极低额度：至少留 8KB，避免退化成几百字节的小读
		return 8 * 1024
	}

	return int(rate)
}

// wait 申请 n 字节的配额；额度不足时睡到下一个窗口，并尊重 ctx 取消。
// 限速关闭时是空操作（调用方无需分支）。
func (l *speedLimiter) wait(ctx context.Context, n int64) error {
	l.mu.Lock()
	rate := l.rate
	if rate <= 0 {
		l.mu.Unlock()

		return nil
	}

	now := time.Now()
	if l.start.IsZero() || now.Sub(l.start) >= time.Second {
		l.start = now
		l.used = 0
	}

	if l.used+n <= rate {
		l.used += n
		l.mu.Unlock()

		return nil
	}

	// 本窗口额度不够：把这块记到下一个窗口，并睡到窗口边界
	deadline := l.start.Add(time.Second)
	l.start = deadline
	l.used = n
	l.mu.Unlock()

	delay := time.Until(deadline)
	if delay <= 0 {
		return nil
	}

	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// SetDownloadSpeedLimitKbps 设置全局限速（KB/s，0 = 不限速）。
func SetDownloadSpeedLimitKbps(kbps int) { downloadLimiter.setSpeedLimitKbps(kbps) }

// DownloadSpeedLimitKbps 当前全局限速（KB/s，0 = 不限速）。
func DownloadSpeedLimitKbps() int { return downloadLimiter.speedLimitKbps() }
