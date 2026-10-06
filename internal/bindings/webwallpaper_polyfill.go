package bindings

// Wallpaper Engine 网页壁纸的 WE API 兼容层(配置解析 + 注入脚本)。
//
// WE 网页壁纸与宿主的交互契约(官方 Web 壁纸文档):
//   - 壁纸把回调对象挂到 window.wallpaperPropertyListener,WE 在启动/属性
//     变化时调 applyUserProperties({属性名: {value}})、applyGeneralProperties;
//   - window.wallpaperRegisterAudioListener(cb) 订阅 128 值频谱;
//   - window.wallpaperRequestRandomFileForProperty(name, cb) 换随机文件。
// 壁纸跑在本启动器的 iframe 里,WE 并不在场——这里把"用户属性"在 pin 壁纸时
// 解析成最终值,并在入口 HTML 的最前面注入一段 polyfill 脚本(先于壁纸自身的
// JS 执行),由它替 WE 完成上述调用。
//
// 用户属性的取值优先级(与 WE 实际行为对齐):
//  1. 依赖项目(模板)project.json general.properties[].value —— 默认值
//  2. 用户项目(预设)project.json 的 preset 对象 —— 预设作者定制的值
//     (WE 安装预设时会把 preset 的值当作用户属性当前值推给壁纸)
//  3. WE config.json wproperties —— 用户后来在 WE 界面里手动改过的值

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// weWebUserProperties 解析网页壁纸的最终用户属性。
//
// 键的大小写必须与 project.json schema 原样一致:WE 的 applyUserProperties
// 推送的属性名保持 schema 原样(如 DateFormat、showSakura),壁纸 JS
// 按同样的键读取——曾经错误地全小写化,壁纸读不到任何属性,全部回落
// 内置默认值(时间格式/粒子数量/自定义文件全错的根因)。
// preset/wproperties 里键的大小写可能与 schema 不同(WE 保存时不保证),
// 归一时以模板 schema 的原始键为准做大小写不敏感匹配。
func weWebUserProperties(renderDir, configDir, selectedFile string) map[string]any {
	properties := map[string]any{}

	// 1. 模板默认值:直接读原始 project.json(schema 键保持原样)。
	//    不能走 weProjectProperties——它为场景渲染器做了小写归一,
	//    而网页壁纸的 applyUserProperties 必须保留原始大小写。
	schemaNames := map[string]string{} // 小写 → schema 原始键
	if data, err := os.ReadFile(filepath.Join(renderDir, "project.json")); err == nil {
		var project struct {
			General struct {
				Properties map[string]struct {
					Value any `json:"value"`
				} `json:"properties"`
			} `json:"general"`
		}
		if json.Unmarshal(data, &project) == nil {
			for name, property := range project.General.Properties {
				properties[name] = property.Value
				schemaNames[strings.ToLower(name)] = name
			}
		}
	}

	// canonical 键名归一:优先映射到模板 schema 的原始大小写
	canonical := func(name string) string {
		if original, ok := schemaNames[strings.ToLower(name)]; ok {
			return original
		}
		return name // schema 里没有的键(如 preset 独有的杂项)按原样保留
	}

	// 2. 预设值:用户项目 project.json 的两处来源
	//    a) general.properties[].value —— 预设自带的属性默认(嵌套在 general 下)
	//    b) preset 对象 —— WE 保存预设时的最终取值(权威,覆盖 a)
	if configDir != "" && !strings.EqualFold(configDir, renderDir) {
		data, err := os.ReadFile(filepath.Join(configDir, "project.json"))
		if err == nil {
			var project struct {
				General struct {
					Properties map[string]struct {
						Value any `json:"value"`
					} `json:"properties"`
				} `json:"general"`
				Preset map[string]any `json:"preset"`
			}
			if json.Unmarshal(data, &project) == nil {
				for name, property := range project.General.Properties {
					if property.Value != nil {
						properties[canonical(name)] = property.Value
					}
				}
				for name, value := range project.Preset {
					if value != nil {
						properties[canonical(name)] = value
					}
				}
			}
		}
	}

	// 3. WE 记录的用户手动取值(只存与默认不同的项)
	for name, value := range weWallpaperUserValues(selectedFile) {
		if value != nil {
			properties[canonical(name)] = value
		}
	}

	if len(properties) == 0 {
		return nil
	}
	return properties
}

// weWebPropertiesJSON 把属性序列化成可安全嵌进 <script> 的 JSON。
// encoding/json 默认转义 <、>、&,不会产生 "</script>" 逃逸。
func weWebPropertiesJSON(properties map[string]any) string {
	if len(properties) == 0 {
		return "{}"
	}
	data, err := json.Marshal(properties)
	if err != nil {
		return "{}"
	}
	return string(data)
}

// weWebPropertiesVersion 属性集合的短指纹(sha1 前 16 个十六进制字符):
// 用户在 WE 里改配置后值变化,前端据此在 iframe URL 上加版本参数触发重载重应用。
// 用哈希而不是 JSON 全文——模板壁纸动辄上百个属性,全文会拼出超长 URL。
func weWebPropertiesVersion(properties map[string]any) string {
	digest := sha1.Sum([]byte(weWebPropertiesJSON(properties)))
	return hex.EncodeToString(digest[:8])
}

// weWebPolyfillScript 生成注入脚本。propertiesJSON 必须来自
// weWebPropertiesJSON(已做 HTML 转义)。
func weWebPolyfillScript(propertiesJSON string) []byte {
	script := `(function () {
  'use strict';
  if (window.__wePolyfillInstalled) return;
  window.__wePolyfillInstalled = true;

  var userProperties = ` + propertiesJSON + `;

  // ---- 属性应用 ----
  // WE 的时序是:壁纸脚本就绪后,WE 把全部当前属性一次性推给
  // applyUserProperties。壁纸挂 listener 的时机不可控(内联脚本、
  // DOMContentLoaded、load 皆有),因此用 setter 拦截赋值 + 多时机兜底。
  function applyAll() {
    var listener = window.wallpaperPropertyListener;
    if (!listener || typeof listener.applyUserProperties !== 'function') return false;
    if (window.__wePropertiesApplied) return true;
    window.__wePropertiesApplied = true;
    try {
      var payload = {};
      for (var name in userProperties) payload[name] = { value: userProperties[name] };
      listener.applyUserProperties(payload);
    } catch (e) {
      window.__wePropertiesApplied = false;
      console.error('[WE Polyfill] applyUserProperties 失败:', e);
    }
    if (typeof listener.applyGeneralProperties === 'function') {
      try { listener.applyGeneralProperties({ properties: { fps: 30 } }); } catch (e) {}
    }
    return true;
  }

  var listenerSlot = null;
  try {
    Object.defineProperty(window, 'wallpaperPropertyListener', {
      configurable: true,
      enumerable: true,
      get: function () { return listenerSlot; },
      set: function (value) {
        listenerSlot = value;
        if (value) {
          // 壁纸赋值后可能还要补方法/初始化,稍等片刻再应用
          setTimeout(applyAll, 0);
          setTimeout(applyAll, 50);
          setTimeout(applyAll, 300);
        }
      },
    });
  } catch (e) {
    // defineProperty 失败(极旧内核)时退化为纯事件兜底
  }

  var retry = function () { applyAll(); setTimeout(applyAll, 0); setTimeout(applyAll, 100); };
  document.addEventListener('DOMContentLoaded', retry);
  window.addEventListener('load', retry);

  // ---- 暂停语义:页面隐藏时通知壁纸(对应 WE 切壁纸/全屏暂停) ----
  document.addEventListener('visibilitychange', function () {
    var listener = window.wallpaperPropertyListener;
    if (listener && typeof listener.setPaused === 'function') {
      try { listener.setPaused(document.hidden); } catch (e) {}
    }
  });

  // ---- 音频频谱 ----
  // WE 喂 128 个 0..1 浮点(左右声道各 64)。启动器没有系统音频捕获,
  // 但自己的音乐播放有实时频谱:主窗口暴露 __nekolauncherAudioBridge,
  // 沙箱 iframe 同源可见。桥不存在(浏览器直接预览等场景)时不回调,
  // 壁纸保持初始画面而不是黑屏。
  window.wallpaperRegisterAudioListener = function (callback) {
    window.__weAudioCallback = callback;
  };
  (function () {
    var bridge = null;
    try {
      bridge = window.parent && window.parent.__nekolauncherAudioBridge;
    } catch (e) { /* 跨域(不应发生) */ }
    if (typeof bridge !== 'function') return;
    var timer = setInterval(function () {
      var callback = window.__weAudioCallback;
      if (!callback) return;
      var spectrum = null;
      try { spectrum = bridge(); } catch (e) { clearInterval(timer); return; }
      if (spectrum && spectrum.length > 0) {
        try { callback(spectrum); } catch (e) {}
      }
    }, 33);
  })();

  // ---- 随机文件:没有用户的文件库,回调空串(壁纸保持当前内容) ----
  window.wallpaperRequestRandomFileForProperty = function (propertyName, callback) {
    var listener = window.wallpaperPropertyListener;
    if (listener && typeof listener.userDirectoryFilesChanged === 'function') {
      try { listener.userDirectoryFilesChanged(propertyName); } catch (e) {}
    }
    if (typeof callback === 'function') {
      try { callback(''); } catch (e) {}
    }
  };

  // ---- file:/// 重定向 ----
  // WE 的 file 类型属性(自定义视频/音乐/背景)值形如 "files/wallpaper.webm",
  // 壁纸 JS 用 'file:///' + 值 拼出绝对 file:// URL——WE 本体会映射到壁纸目录,
  // 我们是 iframe,file:// 直接失败(视频圆窗/自定义音频黑掉的根因)。
  // 这里劫持 media 元素 src 的两条赋值通道(setter 与 setAttribute),把
  // file:///xxx 重写到应用内路由 /wwwallpaper/file/xxx(后端按配置目录→
  // 渲染目录双兜底提供真实文件)。
  function rewriteFileUrl(value) {
    if (typeof value !== 'string') return value;
    var marker = 'file:///wwwallpaper/';
    if (value.indexOf(marker) === 0) return '/wwwallpaper/' + value.slice(marker.length);
    if (value.indexOf('file:///') !== 0) return value;
    return '/wwwallpaper/file/' + value.slice('file:///'.length);
  }
  try {
    var mediaProto = Object.getOwnPropertyDescriptor(HTMLMediaElement.prototype, 'src');
    if (mediaProto && mediaProto.set) {
      Object.defineProperty(HTMLMediaElement.prototype, 'src', {
        configurable: true,
        enumerable: mediaProto.enumerable,
        get: mediaProto.get,
        set: function (value) { mediaProto.set.call(this, rewriteFileUrl(value)); },
      });
    }
    var origSetAttr = Element.prototype.setAttribute;
    Element.prototype.setAttribute = function (name, value) {
      if (typeof name === 'string' && name.toLowerCase() === 'src') {
        value = rewriteFileUrl(value);
      }
      return origSetAttr.call(this, name, value);
    };
  } catch (e) {
    console.warn('[WE Polyfill] file:// 重定向安装失败:', e);
  }

  // ---- WebGL 常量 shim:gl.POINT ----
  // 部分壁纸脚本(如「完美壁纸」的樱花粒子)写了 gl.drawArrays(gl.POINT, ...),
  // 但 WebGL 只有 gl.POINTS(=0),gl.POINT 是 undefined——drawArrays(undefined)
  // 触发 INVALID_ENUM 被静默吞掉,花瓣/粒子一次都画不出来(只剩后期 bloom 光斑)。
  // WE 的 CEF 实现恰好容忍 undefined→按 0 处理,所以 WE 里显示正常。
  // 这里 hook getContext,给每个 WebGL 上下文补上 gl.POINT = gl.POINTS。
  try {
    var origGetContext = HTMLCanvasElement.prototype.getContext;
    var shimGL = function (gl) {
      if (gl && gl.drawArrays && gl.POINTS !== undefined && gl.POINT === undefined) {
        try { gl.POINT = gl.POINTS; } catch (e) {}
      }
      return gl;
    };
    HTMLCanvasElement.prototype.getContext = function (type) {
      return shimGL(origGetContext.apply(this, arguments));
    };
    // 已存在的上下文(polyfill 之后才初始化的场景)补丁由壁纸脚本调用时按属性查找,
    // 拿不到引用的不强求——主流壁纸都在 polyfill 之后才 getContext。
  } catch (e) {
    console.warn('[WE Polyfill] WebGL POINT shim 安装失败:', e);
  }
})();`
	return []byte(script)
}

// injectWebPolyfill 把 polyfill 脚本插到 HTML 最先执行的位置:
// 有 <head> 时插在 <head...> 标签之后(字节安全:<head> 是 ASCII,
// GBK 双字节序列不会横跨标签边界),否则前置到整个文档开头。
func injectWebPolyfill(html []byte, propertiesJSON string) []byte {
	script := weWebPolyfillScript(propertiesJSON)
	injected := append([]byte("<script>"), script...)
	injected = append(injected, []byte("</script>")...)

	if index := indexAfterHeadTag(html); index > 0 {
		out := make([]byte, 0, len(html)+len(injected)+len("\n"))
		out = append(out, html[:index]...)
		out = append(out, '\n')
		out = append(out, injected...)
		out = append(out, html[index:]...)
		return out
	}
	out := make([]byte, 0, len(html)+len(injected))
	out = append(out, injected...)
	out = append(out, html...)
	return out
}

// indexAfterHeadTag 找 <head ...> 标签结束位置(大小写不敏感);无 head 返回 0。
func indexAfterHeadTag(html []byte) int {
	lower := bytes.ToLower(html)
	index := bytes.Index(lower, []byte("<head"))
	if index < 0 {
		return 0
	}
	end := bytes.IndexByte(lower[index:], '>')
	if end < 0 {
		return 0
	}
	return index + end + 1
}

// weWebEntryIsHTML 判断入口文件是否 HTML(按扩展名)。
func weWebEntryIsHTML(entry string) bool {
	return strings.EqualFold(filepath.Ext(entry), ".html") || strings.EqualFold(filepath.Ext(entry), ".htm")
}
