package bindings

// AccountAPI 扩展：取消微软登录轮询。
// internal/auth 的 Authenticate 本身接受 ctx（取消即中断轮询），
// 因此无需改动 internal/auth，仅在 bindings 层记录当前登录的 cancel 即可。

import (
	"context"
	"errors"
)

// errNoLoginInProgress 当前没有进行中的微软登录。
var errNoLoginInProgress = errors.New("no microsoft login in progress")

// microsoftLoginHandle 一次登录的取消句柄包装；指针身份用于 done 时
// 判断存储的句柄是否仍属于本次登录（context.CancelFunc 是函数值不可比较）。
type microsoftLoginHandle struct {
	cancel context.CancelFunc
}

// CancelMicrosoftLogin 取消进行中的微软设备码登录（中断后台轮询）。
// 无进行中的登录时返回错误，前端可忽略。
func (a *AccountAPI) CancelMicrosoftLogin() error {
	a.loginMu.Lock()
	handle := a.loginCancel
	a.loginMu.Unlock()
	if handle == nil {
		return errNoLoginInProgress
	}
	handle.cancel()
	return nil
}

// beginMicrosoftLogin 启动一次可取消的登录 ctx；若有旧登录先取消。
// 返回的 done 需在登录结束时调用以清空句柄。
func (a *AccountAPI) beginMicrosoftLogin() (context.Context, func()) {
	ctx, cancel := context.WithCancel(callCtx(a.ctx))
	handle := &microsoftLoginHandle{cancel: cancel}
	a.loginMu.Lock()
	if a.loginCancel != nil {
		a.loginCancel.cancel()
	}
	a.loginCancel = handle
	a.loginMu.Unlock()
	done := func() {
		cancel()
		a.loginMu.Lock()
		// 仅当存储的句柄仍是本次登录时才清空：
		// 期间若已有新登录覆盖了句柄，不能把新登录的取消入口置空。
		if a.loginCancel == handle {
			a.loginCancel = nil
		}
		a.loginMu.Unlock()
	}
	return ctx, done
}
