package bindings

// ModpackAPI 扩展：取消进行中的整合包 / NekoSolo 导出。
// internal/modpack 与 internal/solo 的导出实现都接受 ctx（写入循环逐文件检查
// ctx.Err()），因此无需改动导出实现，仅在 bindings 层记录当前导出的 cancel。
// 模式与 AccountAPI 的 loginCancel（api_account_ext.go）一致。

import (
	"context"
	"errors"
)

// errNoExportInProgress 当前没有进行中的导出。
var errNoExportInProgress = errors.New("no export in progress")

// exportHandle 一次导出的取消句柄包装；指针身份用于 done 时判断存储的
// 句柄是否仍属于本次导出（context.CancelFunc 是函数值不可比较）。
type exportHandle struct {
	cancel context.CancelFunc
}

// beginExport 启动一次可取消的导出 ctx；若有旧导出先取消。
// 返回的 done 需在导出结束时调用以清空句柄。
func (a *ModpackAPI) beginExport() (context.Context, func()) {
	ctx, cancel := context.WithCancel(callCtx(a.ctx))
	handle := &exportHandle{cancel: cancel}
	a.exportMu.Lock()
	if a.exportCancel != nil {
		a.exportCancel.cancel()
	}
	a.exportCancel = handle
	a.exportMu.Unlock()
	done := func() {
		cancel()
		a.exportMu.Lock()
		// 仅当存储的句柄仍是本次导出时才清空：期间若已有新导出覆盖了句柄，
		// 不能把新导出的取消入口置空。
		if a.exportCancel == handle {
			a.exportCancel = nil
		}
		a.exportMu.Unlock()
	}
	return ctx, done
}

// CancelExport 取消进行中的导出（整合包打包或 NekoSolo 安装包）。
// 无进行中的导出时返回错误，前端可忽略。
func (a *ModpackAPI) CancelExport() error {
	a.exportMu.Lock()
	handle := a.exportCancel
	a.exportMu.Unlock()
	if handle == nil {
		return errNoExportInProgress
	}
	handle.cancel()
	return nil
}
