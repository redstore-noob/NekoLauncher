package bindings

// AI 请求本地代理路由：挂在 Wails assetserver 回退处理器上。
//
// 背景：AI 提供商的 API Key 此前由前端从加密存储（ReadSecret）读回明文，
// 再放进 fetch 的 Authorization/x-api-key 头——key 一旦进入 JS 上下文，
// 同 WebView 的任意插件 JS 都能读到。这个路由把注入点收回到 Go 侧：
// 前端 fetch /ai-proxy 并以 X-AI-Target 头声明目标 URL，Go 从加密存储
// 取 key 注入后转发并流式回传响应（SSE 逐块 flush）。key 明文从此
// 不再经过 WebView；插件至多能"借用"代理发请求，拿不走 key 本身。

import (
	"fmt"
	"net/http"
	"strings"
)

// aiProxyRoute 代理路由；前端以相对路径 fetch，与 WebView 同源。
const aiProxyRoute = "/ai-proxy"

// aiProxyTargetHeader 目标 URL 请求头（完整 https(s) 地址，含查询串）。
const aiProxyTargetHeader = "X-AI-Target"

// aiProxyKeyModeHeader key 注入方式："bearer"（Authorization: Bearer）或
// "x-api-key"（Anthropic 风格）；其余值拒绝。
const aiProxyKeyModeHeader = "X-AI-Key-Mode"

// aiProxyClient 出站客户端：不设整体超时（SSE 流可能持续数分钟），
// 取消经请求上下文传播（前端 AbortSignal → 连接断开 → ctx 取消）。
var aiProxyClient = &http.Client{}

// NewAIProxyHandler 构造 /ai-proxy 处理器。
func NewAIProxyHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		target := strings.TrimSpace(r.Header.Get(aiProxyTargetHeader))
		if target == "" || !(strings.HasPrefix(target, "https://") || strings.HasPrefix(target, "http://")) {
			http.Error(w, "缺少有效的 X-AI-Target 目标地址", http.StatusBadRequest)
			return
		}
		keyMode := strings.ToLower(strings.TrimSpace(r.Header.Get(aiProxyKeyModeHeader)))
		if keyMode != "bearer" && keyMode != "x-api-key" {
			http.Error(w, "X-AI-Key-Mode 必须是 bearer 或 x-api-key", http.StatusBadRequest)
			return
		}
		key := readSecretValue(aiSecretStorageKey)
		// key 可为空：本地推理端点（llama.cpp / Ollama 等）本来就不鉴权，
		// 直接转发即可；云端供应商会以自己的 401 回应。

		outbound, err := http.NewRequestWithContext(r.Context(), r.Method, target, r.Body)
		if err != nil {
			http.Error(w, fmt.Sprintf("构造出站请求失败：%v", err), http.StatusBadRequest)
			return
		}
		// 其余请求头（Content-Type / Accept / anthropic-version 等）原样透传
		for name, values := range r.Header {
			if strings.EqualFold(name, aiProxyTargetHeader) || strings.EqualFold(name, aiProxyKeyModeHeader) {
				continue
			}
			for _, value := range values {
				outbound.Header.Add(name, value)
			}
		}
		if key != "" {
			if keyMode == "bearer" {
				outbound.Header.Set("Authorization", "Bearer "+key)
			} else {
				outbound.Header.Set("x-api-key", key)
			}
		}

		response, err := aiProxyClient.Do(outbound)
		if err != nil {
			http.Error(w, fmt.Sprintf("AI 请求转发失败：%v", err), http.StatusBadGateway)
			return
		}
		defer response.Body.Close()

		for name, values := range response.Header {
			for _, value := range values {
				w.Header().Add(name, value)
			}
		}
		w.WriteHeader(response.StatusCode)
		flusher, _ := w.(http.Flusher)
		buffer := make([]byte, 32*1024)
		for {
			read, readErr := response.Body.Read(buffer)
			if read > 0 {
				if _, writeErr := w.Write(buffer[:read]); writeErr != nil {
					return
				}
				if flusher != nil {
					flusher.Flush()
				}
			}
			if readErr != nil {
				return
			}
		}
	})
}
