package download

import (
	"bytes"
	"context"
	"io"
	"sync"
	"testing"
	"time"
)

// TestSpeedLimiterDisabled 不限速时 wait 立即返回且不影响读块大小。
func TestSpeedLimiterDisabled(t *testing.T) {
	limiter := &speedLimiter{}
	limiter.setSpeedLimitKbps(0)

	if got := limiter.speedLimitKbps(); got != 0 {
		t.Fatalf("不限速时读数 = %d", got)
	}
	if got := limiter.permitReadSize(128 * 1024); got != 128*1024 {
		t.Fatalf("不限速时读块应保持原大小，得到 %d", got)
	}

	start := time.Now()
	for index := 0; index < 100; index++ {
		if err := limiter.wait(context.Background(), 128*1024); err != nil {
			t.Fatalf("不限速时不应报错：%v", err)
		}
	}
	if elapsed := time.Since(start); elapsed > 50*time.Millisecond {
		t.Fatalf("不限速时不应等待，实际耗时 %v", elapsed)
	}
}

// TestSpeedLimiterThrottles 限速后吞吐应接近设定值（1 秒窗口）。
func TestSpeedLimiterThrottles(t *testing.T) {
	limiter := &speedLimiter{}
	limiter.setSpeedLimitKbps(64) // 64 KB/s

	chunk := limiter.permitReadSize(128 * 1024)
	if chunk > 64*1024 {
		t.Fatalf("读块应被压到单窗口额度以内，得到 %d", chunk)
	}

	// 申请 2 个窗口的量：第二次必须等到下一个窗口
	start := time.Now()
	for index := 0; index < 2; index++ {
		if err := limiter.wait(context.Background(), int64(chunk)); err != nil {
			t.Fatalf("wait 报错：%v", err)
		}
	}
	elapsed := time.Since(start)

	if elapsed < 300*time.Millisecond {
		t.Fatalf("第二个窗口应等待，实际只用了 %v", elapsed)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("等待过久（限速逻辑可能死等）：%v", elapsed)
	}
}

// TestSpeedLimiterHonoursContext wait 阻塞期间 ctx 取消要立即返回，否则取消下载会卡住。
func TestSpeedLimiterHonoursContext(t *testing.T) {
	limiter := &speedLimiter{}
	limiter.setSpeedLimitKbps(1) // 1 KB/s

	ctx, cancel := context.WithCancel(context.Background())
	// 先把本窗口额度用满
	if err := limiter.wait(ctx, 1024); err != nil {
		t.Fatalf("首次申请不该阻塞：%v", err)
	}

	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	start := time.Now()
	err := limiter.wait(ctx, 1024)
	if err == nil {
		t.Fatal("ctx 取消后应返回错误")
	}
	if elapsed := time.Since(start); elapsed > 900*time.Millisecond {
		t.Fatalf("取消后应立即返回，实际 %v", elapsed)
	}
}

// TestSpeedLimiterSharedAcrossDownloads 多路并发共享同一份配额：
// 两条各读 32KB 的流在 32KB/s 上限下必须合计被限住，而不是各自拿到 32KB/s。
func TestSpeedLimiterSharedAcrossDownloads(t *testing.T) {
	limiter := &speedLimiter{}
	limiter.setSpeedLimitKbps(32)

	var waitGroup sync.WaitGroup
	start := time.Now()
	for worker := 0; worker < 2; worker++ {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			for round := 0; round < 2; round++ {
				if err := limiter.wait(context.Background(), 32*1024); err != nil {
					t.Errorf("wait 报错：%v", err)

					return
				}
			}
		}()
	}
	waitGroup.Wait()

	// 4 × 32KB 在 32KB/s 的共享上限下约需 3 个窗口
	if elapsed := time.Since(start); elapsed < 1500*time.Millisecond {
		t.Fatalf("并发下载未被共享限速（耗时 %v）", elapsed)
	}
}

// TestSpeedLimitRangeClamps 取值边界：负数归 0、超上限归 0（等价不限速）。
func TestSpeedLimitRangeClamps(t *testing.T) {
	limiter := &speedLimiter{}

	limiter.setSpeedLimitKbps(-5)
	if got := limiter.speedLimitKbps(); got != 0 {
		t.Fatalf("负数应归 0，得到 %d", got)
	}

	limiter.setSpeedLimitKbps(speedLimitMaximumKbps + 1)
	if got := limiter.speedLimitKbps(); got == 0 || got > speedLimitMaximumKbps {
		t.Fatalf("超上限应被夹住，得到 %d", got)
	}
}

// TestThrottledReaderThroughput 端到端：限速 64KB/s 读 96KB 至少要 1 个窗口。
func TestThrottledReaderThroughput(t *testing.T) {
	limiter := &speedLimiter{}
	limiter.setSpeedLimitKbps(64)

	payload := bytes.Repeat([]byte("x"), 96*1024)
	reader := bytes.NewReader(payload)
	buffer := make([]byte, 128*1024)

	start := time.Now()
	var total int
	for {
		chunk := buffer[:limiter.permitReadSize(len(buffer))]
		if err := limiter.wait(context.Background(), int64(len(chunk))); err != nil {
			t.Fatalf("wait 报错：%v", err)
		}
		read, err := reader.Read(chunk)
		total += read
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("读取报错：%v", err)
		}
	}
	elapsed := time.Since(start)

	if total != len(payload) {
		t.Fatalf("读取字节数 = %d，期望 %d", total, len(payload))
	}
	if elapsed < 500*time.Millisecond {
		t.Fatalf("96KB 在 64KB/s 下不该这么快读完：%v", elapsed)
	}
}
