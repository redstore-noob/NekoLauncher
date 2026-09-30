package auth

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"

	"nekolauncher/internal/config"
)

// startFakeAuthlib 启动假皮肤站并记录全部请求；测试永不访问真实皮肤站。
func startFakeAuthlib(
	t *testing.T,
	respond func(w http.ResponseWriter, r *http.Request, body string),
) (*httptest.Server, *httpTrace) {
	t.Helper()
	trace := &httpTrace{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		trace.add(r, body)
		respond(w, r, string(body))
	}))
	t.Cleanup(server.Close)
	return server, trace
}

// TestNormalizeServerUrl 用户输入的皮肤站地址归一化：允许省略协议、带协议时去掉尾斜杠。
// 防的回归：用户只填 littleskin.cn（不带协议）时直接拿去发请求，报
// 「不支持的协议方案」，看起来像皮肤站挂了。
// 注意：无协议输入不在这里去尾斜杠（后续 trimToPath 会兜底），这里只锁协议补全行为。
func TestNormalizeServerUrl(t *testing.T) {
	cases := map[string]string{
		"littleskin.cn":            "https://littleskin.cn",
		"littleskin.cn/":           "https://littleskin.cn/",
		"https://littleskin.cn":    "https://littleskin.cn",
		"https://littleskin.cn/":   "https://littleskin.cn",
		"http://127.0.0.1:8080/":   "http://127.0.0.1:8080",
		"https://a.example/b/c/":   "https://a.example/b/c",
		"https://a.example/b/c//":  "https://a.example/b/c/",
		"  https://space.example ": "https://space.example",
	}
	for input, want := range cases {
		trimmed := strings.TrimSpace(input)
		got, err := normalizeServerUrl(trimmed)
		if err != nil {
			t.Fatalf("normalizeServerUrl(%q) 报错：%v", input, err)
		}
		if got != want {
			t.Fatalf("normalizeServerUrl(%q) = %q，期望 %q", input, got, want)
		}
	}
}

// TestTrimToPathAndHostOf 去掉查询/片段与取主机名的辅助函数。
// 防的回归：接口地址里带上 ?query 或 #fragment，拼出 /authserver/authenticate?x=1
// 之外的怪地址；错误文案里的主机名把整条 URL 打出来吓到用户。
func TestTrimToPathAndHostOf(t *testing.T) {
	pathCases := map[string]string{
		"https://a.example/api/yggdrasil":     "https://a.example/api/yggdrasil",
		"https://a.example/api/yggdrasil/":    "https://a.example/api/yggdrasil",
		"https://a.example/api?x=1":           "https://a.example/api",
		"https://a.example/api#frag":          "https://a.example/api",
		"https://a.example/api?x=1#frag":      "https://a.example/api",
		"https://a.example":                   "https://a.example",
		"https://a.example/?x=1":              "https://a.example",
		"http://127.0.0.1:8080/yggdrasil?x=1": "http://127.0.0.1:8080/yggdrasil",
	}
	for input, want := range pathCases {
		if got := trimToPath(input); got != want {
			t.Fatalf("trimToPath(%q) = %q，期望 %q", input, got, want)
		}
	}

	hostCases := map[string]string{
		"https://a.example/api/yggdrasil?x=1": "a.example",
		"http://a.example:8080/x":             "a.example:8080",
		"a.example/path":                      "a.example",
		"a.example":                           "a.example",
	}
	for input, want := range hostCases {
		if got := hostOf(input); got != want {
			t.Fatalf("hostOf(%q) = %q，期望 %q", input, got, want)
		}
	}
}

// TestResolveLocation 皮肤站的 X-Authlib-Injector-API-Location 头可能是绝对地址、
// 根相对地址或普通相对地址，三种都要解析正确。
// 防的回归：相对地址按字符串直接拼接，得到 https://a.example/api/api/yggdrasil
// 之类的地址，元数据请求 404 后提示「该地址不是有效的皮肤站」。
func TestResolveLocation(t *testing.T) {
	cases := []struct {
		base     string
		location string
		want     string
	}{
		{"https://a.example/", "https://b.example/api/yggdrasil", "https://b.example/api/yggdrasil"},
		{"https://a.example/", "http://b.example/api", "http://b.example/api"},
		{"https://a.example/", "/api/yggdrasil", "https://a.example/api/yggdrasil"},
		{"https://a.example/sub/page", "/api/yggdrasil", "https://a.example/api/yggdrasil"},
		// 相对地址按「当前路径目录」解析：实现先 trimToPath 去掉尾斜杠再回退一级，
		// 所以以 / 结尾的 base 也会再回退一级（当前行为，改动它要先想清楚兼容性）
		{"https://a.example/sub/", "api", "https://a.example/api"},
		{"https://a.example/sub/page", "api/yggdrasil", "https://a.example/sub/api/yggdrasil"},
	}
	for _, testCase := range cases {
		got, err := resolveLocation(testCase.base, testCase.location)
		if err != nil {
			t.Fatalf("resolveLocation(%q, %q) 报错：%v", testCase.base, testCase.location, err)
		}
		if got != testCase.want {
			t.Fatalf("resolveLocation(%q, %q) = %q，期望 %q",
				testCase.base, testCase.location, got, testCase.want)
		}
	}
}

// TestResolveServerFollowsApiLocationHeader 必须读取 X-Authlib-Injector-API-Location
// 头并跳转到真正的 API 根，同时解析出皮肤站名与皮肤域名。
// 防的回归：忽略该响应头直接用首页地址当 API 根——多数皮肤站首页不是
// Yggdrasil 接口，登录一律失败。
func TestResolveServerFollowsApiLocationHeader(t *testing.T) {
	cases := []struct {
		name         string
		locationPath string // 头里声明的 API 根（相对当前站点）
	}{
		{name: "绝对地址", locationPath: "absolute"},
		{name: "根相对地址", locationPath: "/api/yggdrasil"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			var serverURL string
			server, trace := startFakeAuthlib(t, func(w http.ResponseWriter, r *http.Request, _ string) {
				if r.URL.Path == "/" {
					location := testCase.locationPath
					if location == "absolute" {
						location = serverURL + "/api/yggdrasil"
					}
					w.Header().Set("X-Authlib-Injector-API-Location", location)
					w.WriteHeader(http.StatusOK)
					_, _ = w.Write([]byte("<html>home</html>"))
					return
				}
				if r.URL.Path == "/api/yggdrasil" {
					respondJSON(w, http.StatusOK,
						`{"meta":{"serverName":"小猫皮肤站","implementationName":"Blessing Skin"},`+
							`"skinDomains":["littleskin.cn",".littleskin.cn"]}`)
					return
				}
				w.WriteHeader(http.StatusNotFound)
			})
			serverURL = server.URL

			info, err := NewAuthlibAuthenticator(nil).ResolveServer(context.Background(), server.URL+"/")
			if err != nil {
				t.Fatalf("解析皮肤站失败：%v", err)
			}
			if info.ApiRoot != server.URL+"/api/yggdrasil" {
				t.Fatalf("ApiRoot = %q，期望 %q", info.ApiRoot, server.URL+"/api/yggdrasil")
			}
			if info.ServerName != "小猫皮肤站" {
				t.Fatalf("ServerName = %q", info.ServerName)
			}
			if len(info.SkinDomains) != 2 || info.SkinDomains[0] != "littleskin.cn" {
				t.Fatalf("SkinDomains = %v", info.SkinDomains)
			}
			if got := trace.paths(); len(got) != 2 || got[0] != "/" || got[1] != "/api/yggdrasil" {
				t.Fatalf("请求路径 = %v，期望先探测首页再读元数据", got)
			}
		})
	}
}

// TestResolveServerWithoutLocationHeader 没有声明 API 位置时，输入地址本身即 API 根，
// 且要去掉查询与尾斜杠。
// 防的回归：把 ?query / 尾斜杠原样带进 ApiRoot，后续拼出
// /authserver/authenticate 时地址重复或带查询参数，接口 404。
func TestResolveServerWithoutLocationHeader(t *testing.T) {
	server, _ := startFakeAuthlib(t, func(w http.ResponseWriter, r *http.Request, _ string) {
		if strings.TrimSuffix(r.URL.Path, "/") != "/api/yggdrasil" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		respondJSON(w, http.StatusOK, `{"meta":{"serverName":"自建站"}}`)
	})

	info, err := NewAuthlibAuthenticator(nil).ResolveServer(
		context.Background(), server.URL+"/api/yggdrasil/?from=launcher")
	if err != nil {
		t.Fatalf("解析皮肤站失败：%v", err)
	}
	if want := server.URL + "/api/yggdrasil"; info.ApiRoot != want {
		t.Fatalf("ApiRoot = %q，期望 %q", info.ApiRoot, want)
	}
}

// TestResolveServerRejectsEmptyInput 空地址必须立刻报错且不发请求。
// 防的回归：空地址被拼成 https:// 去发请求，用户看到的是一句
// 「无法连接皮肤站：https://（请检查地址与网络）」。
func TestResolveServerRejectsEmptyInput(t *testing.T) {
	_, trace := startFakeAuthlib(t, func(w http.ResponseWriter, _ *http.Request, _ string) {
		w.WriteHeader(http.StatusOK)
	})
	for _, input := range []string{"", "   "} {
		_, err := NewAuthlibAuthenticator(nil).ResolveServer(context.Background(), input)
		if err == nil {
			t.Fatalf("ResolveServer(%q) 应当失败", input)
		}
		if !strings.Contains(err.Error(), "请输入皮肤站地址") {
			t.Fatalf("错误 = %v，期望提示输入地址", err)
		}
	}
	if got := len(trace.paths()); got != 0 {
		t.Fatalf("空地址不应发请求，实际 %d 次", got)
	}
}

// TestRequestMetadataFailureBranches 元数据阶段的失败分支。
// 防的回归：把「不是皮肤站」（返回 HTML 的普通网站）和「皮肤站 500」混成
// 同一句网络错误，用户不知道该换地址还是稍后重试。
// 注意：发现阶段与元数据请求打的是同一个地址（没有 location 头时输入即 API 根），
// 所以这里让第一次请求成功、第二次才返回被测的失败响应。
func TestRequestMetadataFailureBranches(t *testing.T) {
	cases := []struct {
		name         string
		status       int
		body         string
		wantContains string
	}{
		{
			name:         "HTTP 500",
			status:       http.StatusInternalServerError,
			body:         "boom",
			wantContains: "读取皮肤站元数据失败（HTTP 500）",
		},
		{
			name:         "返回 HTML 而不是元数据",
			status:       http.StatusOK,
			body:         "<html>welcome</html>",
			wantContains: "不是有效的 authlib-injector 皮肤站",
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			var gate sync.Mutex
			requests := 0
			server, _ := startFakeAuthlib(t, func(w http.ResponseWriter, _ *http.Request, _ string) {
				gate.Lock()
				requests++
				current := requests
				gate.Unlock()
				if current == 1 {
					// 发现阶段：200 且不带 X-Authlib-Injector-API-Location，
					// 于是输入地址本身被当作 API 根
					respondJSON(w, http.StatusOK, "<html>home</html>")
					return
				}
				respondJSON(w, testCase.status, testCase.body)
			})

			_, err := NewAuthlibAuthenticator(nil).ResolveServer(context.Background(), server.URL)
			if err == nil {
				t.Fatal("应当失败")
			}
			if !strings.Contains(err.Error(), testCase.wantContains) {
				t.Fatalf("错误 = %v，期望包含 %q", err, testCase.wantContains)
			}
		})
	}

	t.Run("服务器不可达", func(t *testing.T) {
		server, _ := startFakeAuthlib(t, func(w http.ResponseWriter, _ *http.Request, _ string) {
			w.WriteHeader(http.StatusOK)
		})
		server.Close()

		_, err := NewAuthlibAuthenticator(nil).ResolveServer(context.Background(), server.URL)
		if err == nil {
			t.Fatal("服务器不可达时应当失败")
		}
		if !strings.Contains(err.Error(), "无法连接皮肤站") {
			t.Fatalf("错误 = %v，期望提示无法连接皮肤站", err)
		}
	})
}

// TestAuthenticateBuildsYggdrasilPayload 皮肤站密码登录的报文构造与档案过滤。
// 防的回归：agent 字段写成别的名字（Yggdrasil 服务端会拒绝）、缺 clientToken
// 导致后续 refresh 失败，以及把缺 id/name 的半条档案塞进角色列表
// （启动时会选中空角色，进游戏就是无皮肤的白号）。
func TestAuthenticateBuildsYggdrasilPayload(t *testing.T) {
	server, trace := startFakeAuthlib(t, func(w http.ResponseWriter, r *http.Request, _ string) {
		if r.URL.Path != "/api/yggdrasil/authserver/authenticate" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		respondJSON(w, http.StatusOK, `{"accessToken":"ygg-token","clientToken":"client-token",
			"availableProfiles":[{"id":"uuid-1","name":"NyaPlayer"},{"id":"uuid-2","name":""},{"name":"无 id"}]}`)
	})

	result, err := NewAuthlibAuthenticator(nil).Authenticate(context.Background(),
		server.URL+"/api/yggdrasil/", "  nya@example.com  ", "p@ssw0rd", "client-token-1")
	if err != nil {
		t.Fatalf("登录应当成功：%v", err)
	}
	if result.AccessToken != "ygg-token" {
		t.Fatalf("AccessToken = %q", result.AccessToken)
	}
	if len(result.Profiles) != 1 || result.Profiles[0].Id != "uuid-1" || result.Profiles[0].Name != "NyaPlayer" {
		t.Fatalf("角色列表 = %+v，期望只保留 id/name 完整的条目", result.Profiles)
	}

	request := trace.last(t, "/api/yggdrasil/authserver/authenticate")
	if request.Method != http.MethodPost {
		t.Fatalf("请求方法 = %s，期望 POST", request.Method)
	}
	if got := request.Header.Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q，期望 application/json", got)
	}
	payload := decodeJSON(t, request.Body)
	agent, ok := payload["agent"].(map[string]any)
	if !ok {
		t.Fatalf("agent 字段类型 = %T", payload["agent"])
	}
	if agent["name"] != "MinecraftLauncher" || agent["version"] != float64(1) {
		t.Fatalf("agent = %v，期望 MinecraftLauncher/1", agent)
	}
	if payload["username"] != "nya@example.com" {
		t.Fatalf("username = %v，期望去掉首尾空格", payload["username"])
	}
	if payload["password"] != "p@ssw0rd" {
		t.Fatalf("password = %v（密码不应被改写）", payload["password"])
	}
	if payload["clientToken"] != "client-token-1" {
		t.Fatalf("clientToken = %v，期望沿用调用方传入的值", payload["clientToken"])
	}
	if payload["requestUser"] != true {
		t.Fatalf("requestUser = %v，期望 true（拿 user 信息给皮肤服务用）", payload["requestUser"])
	}
}

// TestAuthenticateRejectsEmptyInputs 登录参数缺失时必须返回错误而不是 panic
// （绑定协程不 recover）。
// 防的回归：空地址/空密码被拿去发请求或索引空串，登录浮层点一下就把程序带走。
func TestAuthenticateRejectsEmptyInputs(t *testing.T) {
	_, trace := startFakeAuthlib(t, func(w http.ResponseWriter, _ *http.Request, _ string) {
		w.WriteHeader(http.StatusOK)
	})
	cases := []struct {
		name                        string
		apiRoot, username, password string
	}{
		{name: "空地址", apiRoot: "  ", username: "u", password: "p"},
		{name: "空用户名", apiRoot: "https://a.example", username: " ", password: "p"},
		{name: "空密码", apiRoot: "https://a.example", username: "u", password: ""},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			result, err := NewAuthlibAuthenticator(nil).Authenticate(
				context.Background(), testCase.apiRoot, testCase.username, testCase.password, "ct")
			if err == nil {
				t.Fatalf("应当失败，实际返回 %+v", result)
			}
			if !strings.Contains(err.Error(), "不能为空") {
				t.Fatalf("错误 = %v，期望提示参数不能为空", err)
			}
		})
	}
	if got := len(trace.paths()); got != 0 {
		t.Fatalf("参数缺失时不应发请求，实际 %d 次", got)
	}
}

// TestAuthenticateFailureMessages 皮肤站登录失败的文案分支。
// 防的回归：把「验证码拦截」「密码错误」「服务端 500」压成同一句，
// 用户反复重试密码却不知道皮肤站开了验证码。
func TestAuthenticateFailureMessages(t *testing.T) {
	cases := []struct {
		name         string
		status       int
		body         string
		wantContains string
		wantCaptcha  bool
	}{
		{
			name:         "用户名或密码错误",
			status:       http.StatusForbidden,
			body:         `{"error":"ForbiddenOperationException","errorMessage":"Invalid credentials. Invalid username or password."}`,
			wantContains: "用户名或密码错误",
		},
		{
			name:         "中文密码错误文案",
			status:       http.StatusForbidden,
			body:         `{"error":"ForbiddenOperationException","errorMessage":"用户不存在或密码错误"}`,
			wantContains: "用户名或密码错误",
		},
		{
			name:         "皮肤站开启验证码（中文）",
			status:       http.StatusForbidden,
			body:         `{"errorMessage":"需要填写验证码"}`,
			wantContains: "验证码",
			wantCaptcha:  true,
		},
		{
			name:         "皮肤站开启验证码（英文）",
			status:       http.StatusBadRequest,
			body:         `{"errorMessage":"Captcha required"}`,
			wantContains: "验证码",
			wantCaptcha:  true,
		},
		{
			name:         "其它错误信息原样带出",
			status:       http.StatusBadRequest,
			body:         `{"error":"IllegalArgumentException","errorMessage":"角色名已被占用"}`,
			wantContains: "角色名已被占用",
		},
		{
			name:         "无响应体只给状态码",
			status:       http.StatusInternalServerError,
			body:         `{}`,
			wantContains: "登录失败（HTTP 500）",
		},
		{
			name:         "缺少 accessToken",
			status:       http.StatusOK,
			body:         `{"clientToken":"ct"}`,
			wantContains: "皮肤站未返回有效的访问令牌",
		},
		{
			name:         "响应不是 JSON",
			status:       http.StatusOK,
			body:         "<html>502</html>",
			wantContains: "皮肤站登录响应解析失败",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			server, _ := startFakeAuthlib(t, func(w http.ResponseWriter, _ *http.Request, _ string) {
				respondJSON(w, testCase.status, testCase.body)
			})

			result, err := NewAuthlibAuthenticator(nil).Authenticate(
				context.Background(), server.URL, "u", "p", "ct")
			if err == nil {
				t.Fatalf("应当失败，实际返回 %+v", result)
			}
			var authErr *AuthlibAuthenticationError
			if !errors.As(err, &authErr) {
				t.Fatalf("错误类型 = %T，期望 *AuthlibAuthenticationError", err)
			}
			if !strings.Contains(authErr.Message, testCase.wantContains) {
				t.Fatalf("错误消息 = %q，期望包含 %q", authErr.Message, testCase.wantContains)
			}
			if authErr.CaptchaRequired != testCase.wantCaptcha {
				t.Fatalf("CaptchaRequired = %v，期望 %v", authErr.CaptchaRequired, testCase.wantCaptcha)
			}
		})
	}
}

// TestValidateOrRefreshBranches 启动前的令牌保活：validate 有效则原样返回，
// 失效则 refresh 换新令牌，refresh 也失败才提示重新登录。
// 防的回归：validate 的 403/400 被当成致命错误直接抛给用户（明明可以无感刷新），
// 以及 refresh 返回空令牌时被当成成功，拿空令牌去启动。
func TestValidateOrRefreshBranches(t *testing.T) {
	credential := &AuthlibCredential{
		Username:    "nya@example.com",
		ProfileName: "NyaPlayer",
		ProfileUuid: "uuid-1",
		AccessToken: "old-token",
		ApiRoot:     "",
	}

	t.Run("validate 通过则原样返回", func(t *testing.T) {
		server, trace := startFakeAuthlib(t, func(w http.ResponseWriter, r *http.Request, _ string) {
			switch r.URL.Path {
			case "/authserver/validate":
				w.WriteHeader(http.StatusNoContent) // Yggdrasil：204 = 有效
			case "/authserver/refresh":
				t.Error("validate 通过时不应调用 refresh")
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		})
		valid := *credential
		valid.ApiRoot = server.URL

		token, err := NewAuthlibAuthenticator(nil).ValidateOrRefresh(
			context.Background(), &valid, "ct")
		if err != nil {
			t.Fatalf("validate 通过时不应报错：%v", err)
		}
		if token != "old-token" {
			t.Fatalf("令牌 = %q，期望原样返回", token)
		}
		payload := decodeJSON(t, trace.last(t, "/authserver/validate").Body)
		if payload["accessToken"] != "old-token" || payload["clientToken"] != "ct" {
			t.Fatalf("validate 报文 = %v", payload)
		}
	})

	t.Run("validate 失效则刷新", func(t *testing.T) {
		server, trace := startFakeAuthlib(t, func(w http.ResponseWriter, r *http.Request, _ string) {
			switch r.URL.Path {
			case "/authserver/validate":
				w.WriteHeader(http.StatusForbidden)
			case "/authserver/refresh":
				respondJSON(w, http.StatusOK, `{"accessToken":"new-token","clientToken":"ct"}`)
			default:
				w.WriteHeader(http.StatusNotFound)
			}
		})
		expired := *credential
		expired.ApiRoot = server.URL + "/" // 尾斜杠不应产生 //authserver/refresh

		token, err := NewAuthlibAuthenticator(nil).ValidateOrRefresh(
			context.Background(), &expired, "ct")
		if err != nil {
			t.Fatalf("刷新应当成功：%v", err)
		}
		if token != "new-token" {
			t.Fatalf("令牌 = %q，期望 new-token", token)
		}
		refresh := decodeJSON(t, trace.last(t, "/authserver/refresh").Body)
		if refresh["accessToken"] != "old-token" || refresh["clientToken"] != "ct" ||
			refresh["requestUser"] != true {
			t.Fatalf("refresh 报文 = %v", refresh)
		}
	})

	t.Run("refresh 也失败则要求重新登录", func(t *testing.T) {
		server, _ := startFakeAuthlib(t, func(w http.ResponseWriter, r *http.Request, _ string) {
			w.WriteHeader(http.StatusForbidden)
		})
		revoked := *credential
		revoked.ApiRoot = server.URL

		token, err := NewAuthlibAuthenticator(nil).ValidateOrRefresh(
			context.Background(), &revoked, "ct")
		if err == nil {
			t.Fatalf("应当失败，实际返回令牌 %q", token)
		}
		if !strings.Contains(err.Error(), "重新登录") {
			t.Fatalf("错误 = %v，期望提示重新登录", err)
		}
	})

	t.Run("refresh 返回空令牌视为失效", func(t *testing.T) {
		server, _ := startFakeAuthlib(t, func(w http.ResponseWriter, r *http.Request, _ string) {
			if r.URL.Path == "/authserver/validate" {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			respondJSON(w, http.StatusOK, `{"clientToken":"ct"}`)
		})
		revoked := *credential
		revoked.ApiRoot = server.URL

		token, err := NewAuthlibAuthenticator(nil).ValidateOrRefresh(
			context.Background(), &revoked, "ct")
		if err == nil {
			t.Fatalf("空令牌应当视为失败，实际返回 %q", token)
		}
	})

	t.Run("validate 网络异常", func(t *testing.T) {
		server, _ := startFakeAuthlib(t, func(w http.ResponseWriter, _ *http.Request, _ string) {
			w.WriteHeader(http.StatusNoContent)
		})
		server.Close()

		broken := *credential
		broken.ApiRoot = server.URL
		_, err := NewAuthlibAuthenticator(nil).ValidateOrRefresh(context.Background(), &broken, "ct")
		if err == nil || !strings.Contains(err.Error(), "网络异常") {
			t.Fatalf("错误 = %v，期望提示网络异常", err)
		}
	})

	t.Run("凭据缺失", func(t *testing.T) {
		authenticator := NewAuthlibAuthenticator(nil)
		if _, err := authenticator.ValidateOrRefresh(context.Background(), nil, "ct"); err == nil {
			t.Fatal("credential 为 nil 时应当报错")
		}
		if _, err := authenticator.ValidateOrRefresh(context.Background(), &AuthlibCredential{}, "ct"); err == nil {
			t.Fatal("缺少 ApiRoot/AccessToken 时应当报错")
		}
	})
}

// TestGetOrCreateClientTokenPersists 同一安装内的 clientToken 必须持久化复用。
// 防的回归：每次会话生成新 clientToken——Yggdrasil 规范要求 authenticate 与
// refresh 使用一致的 clientToken，不一致时已有账号的 validate/refresh 全部失败，
// 用户每次启动都被要求重新登录。
func TestGetOrCreateClientTokenPersists(t *testing.T) {
	useTempAuthStorage(t)

	if got := config.GetValue(ClientTokenConfigKey); got != "" {
		t.Fatalf("全新存储目录里不应已有 clientToken：%q", got)
	}

	first := GetOrCreateClientToken()
	if !uuidV4Pattern.MatchString(first) {
		t.Fatalf("clientToken = %q，期望小写带连字符的 UUID v4", first)
	}
	if stored := config.GetValue(ClientTokenConfigKey); stored != first {
		t.Fatalf("clientToken 未持久化：存储值 = %q", stored)
	}
	if second := GetOrCreateClientToken(); second != first {
		t.Fatalf("第二次调用得到 %q，期望复用 %q", second, first)
	}

	// 已存在（含首尾空格）的值必须原样复用，不重新生成
	if !config.SetValue(ClientTokenConfigKey, "  fixed-token  ") {
		t.Fatal("写入 clientToken 失败")
	}
	if got := GetOrCreateClientToken(); got != "  fixed-token  " {
		t.Fatalf("已存在的 clientToken 被改写：%q", got)
	}
}

var uuidV4Pattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

// TestNewUuidFormat newUUID 必须产出 RFC 4122 v4 格式的小写 UUID。
// 防的回归：格式不合法（大写/缺连字符/版本号错）被 Yggdrasil 服务端拒绝，
// 以及随机源失效时产出重复值造成账号串号。
func TestNewUuidFormat(t *testing.T) {
	seen := map[string]bool{}
	for index := 0; index < 64; index++ {
		value := newUUID()
		if !uuidV4Pattern.MatchString(value) {
			t.Fatalf("newUUID() = %q，不符合 UUID v4 格式", value)
		}
		if seen[value] {
			t.Fatalf("newUUID() 产生重复值：%q", value)
		}
		seen[value] = true
	}
	if got := len(newUUID()); got != 36 {
		t.Fatalf("UUID 长度 = %d，期望 36", got)
	}
}
