//go:build !windows

package singleinstance

// 非 Windows 平台不做单实例限制。
//
// 原因：Wails 在 Linux/macOS 上的窗口后端是 cgo 实现，而这台项目的主要
// 分发目标（NekoSolo 安装包、README 里的三平台支持）里 Windows 才是
// 多开问题真正出现的地方；在其它平台贸然加锁会引入新的启动失败面，
// 又没有对应的验证环境。
//
// 这里保持与 Windows 版一致的返回语义：调用方代码无需分平台。
func acquire() Result {
	return Result{Cleanup: func() {}}
}
