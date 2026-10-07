package bindings

// AccountAPI 扩展：内嵌浏览器 OAuth 登录。
// 主窗口经 WindowExecJS 导航到微软登录页（不弹系统浏览器、不开新窗口），
// 授权后由本地回环服务器 302 跳回启动器页面；SPA 重载后通过
// GetMicrosoftLoginState 轮询 + auth:microsoftProgress 事件接力显示进度。

import (
	"context"
	"errors"
	"strings"
	"time"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"nekolauncher/internal/auth"
)

// microsoftLoginBarID 注入到微软登录页的导航条元素 ID（幂等注入的守卫）。
const microsoftLoginBarID = "neko-msauth-bar"

// errBrowserLoginAlreadyActive 已有一轮内嵌登录在进行。
var errBrowserLoginAlreadyActive = errors.New("已有内嵌登录正在进行，请等待完成或先取消")

// MicrosoftBrowserLoginState 内嵌浏览器登录进度状态。
// 同时作为 auth:microsoftProgress 事件的载荷，前端一次解析处处可用。
type MicrosoftBrowserLoginState struct {
	Active    bool   `json:"Active"`
	Done      bool   `json:"Done"`
	Failed    bool   `json:"Failed"`
	Percent   int    `json:"Percent"`
	Message   string `json:"Message"`
	ErrorText string `json:"ErrorText"`
	Username  string `json:"Username"`
}

// browserLoginStepProgress 登录链路步骤 → 进度百分比与提示文案。
var browserLoginStepProgress = map[int]struct {
	Percent int
	Message string
}{
	auth.LoginStepAuthorized:     {40, "已通过微软登录页授权，正在跳回启动器…"},
	auth.LoginStepMicrosoftToken: {58, "已获取 Microsoft 令牌"},
	auth.LoginStepXboxLive:       {70, "Xbox Live 认证完成"},
	auth.LoginStepXsts:           {80, "XSTS 授权完成"},
	auth.LoginStepMinecraft:      {90, "Minecraft 令牌获取完成"},
	auth.LoginStepDone:           {100, "登录完成"},
}

// LoginMicrosoftBrowser 启动内嵌浏览器登录（异步：立即返回，结果经
// auth:microsoftProgress 事件与 GetMicrosoftLoginState 推送）。
// returnTo 传前端 window.location.origin，授权结束后跳回该源。
// 账号在 Go 侧直接入库（与设备码流程"前端负责入库"的约定不同——SPA
// 跳转往返后已丢失原调用上下文，只能由后端持久化）。
func (a *AccountAPI) LoginMicrosoftBrowser(returnTo string) error {
	returnBase, err := auth.NormalizeReturnBase(returnTo)
	if err != nil {
		return err
	}

	// 先查 Active 再 begin：顺序反了的话，"已有登录进行中"的拒绝路径
	// 会先杀掉正在进行的那次登录（其错误路径还会把窗口推到 ?msoauth=error
	// 再重载一次 SPA）——用户看到"已有登录进行中"，实际什么都没在跑。
	a.browserMu.Lock()
	if a.browserState.Active {
		a.browserMu.Unlock()
		return errBrowserLoginAlreadyActive
	}
	a.browserMu.Unlock()

	ctx, done := a.beginMicrosoftLogin()
	a.browserMu.Lock()
	a.browserReturnTo = returnBase
	a.browserState = MicrosoftBrowserLoginState{Active: true, Percent: 5, Message: "正在准备内嵌登录…"}
	a.browserMu.Unlock()
	a.emitBrowserState()

	go func() {
		defer done()
		account, err := a.browserAuth.StartBrowserLogin(
			ctx, returnBase,
			func(loginURL string) {
				a.updateBrowserState(func(state *MicrosoftBrowserLoginState) {
					state.Percent = 12
					state.Message = "已打开微软登录页，等待授权…"
				})
				navigateEmbeddedWindow(callCtx(a.ctx), loginURL)
				// 主窗口已在微软页面（此时 SPA 不存在），开始周期性注入
				// 顶部导航条：页面跨域跳转会把注入条冲掉，周期重注补回。
				go a.runMicrosoftLoginBar(ctx, returnBase)
			},
			func(step int) {
				meta, ok := browserLoginStepProgress[step]
				if !ok {
					return
				}
				a.updateBrowserState(func(state *MicrosoftBrowserLoginState) {
					state.Percent = meta.Percent
					state.Message = meta.Message
				})
			})
		if err != nil {
			a.updateBrowserState(func(state *MicrosoftBrowserLoginState) {
				state.Active = false
				state.Failed = true
				state.Percent = 100
				// 用户经注入条"返回启动器"主动取消时，错误文案不该吓人
				if ctx.Err() != nil {
					state.ErrorText = "已取消内嵌登录"
				} else {
					state.ErrorText = err.Error()
				}
			})
			a.emitBrowserState()
			// 失败/取消时窗口多半还停在微软登录页，带错误标记跳回启动器；
			// 已在回跳后的失败（XBL/档案阶段）会重载 SPA，由浮层读状态报错。
			navigateEmbeddedWindow(callCtx(a.ctx), returnBase+"?msoauth=error")
			return
		}
		a.persistBrowserLoginResult(account)
		a.updateBrowserState(func(state *MicrosoftBrowserLoginState) {
			state.Active = false
			state.Done = true
			state.Percent = 100
			state.Username = account.Username
		})
		a.emitBrowserState()
		emit(callCtx(a.ctx), "auth:microsoftLoggedIn", account.Username)
	}()
	return nil
}

// GetMicrosoftLoginState 读取当前内嵌登录状态（SPA 重载后接力显示进度用）。
func (a *AccountAPI) GetMicrosoftLoginState() MicrosoftBrowserLoginState {
	a.browserMu.Lock()
	defer a.browserMu.Unlock()
	return a.browserState
}

// DismissMicrosoftLoginState 清除已结束（完成/失败）的状态展示，防止下次进入页面重复弹报。
func (a *AccountAPI) DismissMicrosoftLoginState() {
	a.browserMu.Lock()
	a.browserState = MicrosoftBrowserLoginState{}
	a.browserMu.Unlock()
	a.emitBrowserState()
}

// updateBrowserState 按当前状态应用一次变更并推送事件。
func (a *AccountAPI) updateBrowserState(apply func(state *MicrosoftBrowserLoginState)) {
	a.browserMu.Lock()
	apply(&a.browserState)
	snapshot := a.browserState
	a.browserMu.Unlock()
	emit(callCtx(a.ctx), "auth:microsoftProgress", snapshot)
}

// emitBrowserState 推送当前状态快照。
func (a *AccountAPI) emitBrowserState() {
	a.browserMu.Lock()
	snapshot := a.browserState
	a.browserMu.Unlock()
	emit(callCtx(a.ctx), "auth:microsoftProgress", snapshot)
}

// persistBrowserLoginResult 内嵌登录结果入库：同一微软档案（UUID）已存在则
// 更新凭据并置顶（视为重新登录），否则新增并自动选中。
func (a *AccountAPI) persistBrowserLoginResult(account auth.MicrosoftAccount) {
	for _, entry := range auth.Shared.Current() {
		if entry.Type == "microsoft" {
			// 经存储在 gate 读锁内取凭据拷贝，避免与 Update* 的原地写竞争
			if snapshot := auth.Shared.MicrosoftSnapshot(entry); snapshot != nil &&
				strings.EqualFold(strings.TrimSpace(snapshot.Uuid), strings.TrimSpace(account.Uuid)) {
				_ = auth.Shared.UpdateMicrosoftAccount(entry, &account)
				auth.Shared.MoveToTop(entry)
				return
			}
		}
	}
	entry := auth.NewLaunchAccount("microsoft", account.Username)
	entry.Microsoft = &account
	auth.Shared.Add(entry)
}

// navigateEmbeddedWindow 把主窗口 WebView 导航到目标地址（内嵌登录跳转 /
// 回跳都走这里）。MarshalJSONString 保证 JS 字符串字面量安全转义。
func navigateEmbeddedWindow(ctx context.Context, target string) {
	if ctx == nil {
		return
	}
	wailsruntime.WindowExecJS(ctx, "window.location.replace("+auth.MarshalJSONString(target)+");")
}

// runMicrosoftLoginBar 登录期间周期性把顶部导航条注入当前页面。
// 微软登录链路会跨域多次跳转（login.live.com → login.microsoftonline.com…），
// 每次整页导航都会丢失注入的 DOM，这里靠周期重注补回（脚本自带幂等守卫）。
// 登录 ctx 结束（完成/取消/失败）时停止注入并尝试移除残留的导航条。
func (a *AccountAPI) runMicrosoftLoginBar(ctx context.Context, returnBase string) {
	if ctx == nil {
		return
	}
	script := buildMicrosoftLoginBarScript(returnBase)
	inject := func() {
		if injectCtx := callCtx(a.ctx); injectCtx != nil {
			wailsruntime.WindowExecJS(injectCtx, script)
		}
	}

	inject()
	ticker := time.NewTicker(1500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			if cleanupCtx := callCtx(a.ctx); cleanupCtx != nil {
				wailsruntime.WindowExecJS(cleanupCtx,
					"var b=document.getElementById('"+microsoftLoginBarID+"');b&&b.remove();")
			}

			return
		case <-ticker.C:
			inject()
		}
	}
}

// buildMicrosoftLoginBarScript 生成注入脚本：在页面顶部固定一条导航栏，
// 右侧"返回启动器"按钮跳回 returnBase 并带 msoauth=cancel 标记（SPA 重载后
// 由 MicrosoftLoginProgress 检测并调用 CancelMicrosoftLogin）。
// 样式全部走 CSSOM 属性赋值（不走 style 属性字符串），不受页面 CSP 的
// style-src 限制；脚本幂等，重复执行不会叠出多条。
func buildMicrosoftLoginBarScript(returnBase string) string {
	backTarget := returnBase + "?msoauth=cancel"

	return "(function(){" +
		"if(window.top!==window)return;" +
		"if(document.getElementById('" + microsoftLoginBarID + "'))return;" +
		"function st(e,p){for(var k in p)e.style[k]=p[k];}" +
		"var bar=document.createElement('div');bar.id='" + microsoftLoginBarID + "';" +
		"st(bar,{position:'fixed',top:'0',left:'0',right:'0',zIndex:'2147483647',display:'flex',alignItems:'center',gap:'12px',padding:'8px 14px',background:'#111827',color:'#f9fafb',fontFamily:'system-ui,sans-serif',fontSize:'13px',lineHeight:'1.4',boxShadow:'0 2px 8px rgba(0,0,0,.35)'});" +
		"var label=document.createElement('span');label.textContent=" + auth.MarshalJSONString("微软账号登录中：完成授权后将自动返回启动器") + ";" +
		"st(label,{flex:'1 1 auto',whiteSpace:'nowrap',overflow:'hidden',textOverflow:'ellipsis'});" +
		"var btn=document.createElement('button');btn.textContent=" + auth.MarshalJSONString("← 返回启动器") + ";" +
		"st(btn,{flex:'0 0 auto',padding:'4px 14px',borderRadius:'9999px',border:'1px solid #4b5563',cursor:'pointer',background:'#374151',color:'#f9fafb',fontSize:'12px'});" +
		"btn.onclick=function(){window.location.replace(" + auth.MarshalJSONString(backTarget) + ");};" +
		"bar.appendChild(label);bar.appendChild(btn);" +
		"(document.body||document.documentElement).appendChild(bar);" +
		"})();"
}
