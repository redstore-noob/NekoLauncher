package launch

import "strings"

// 系统语言（BCP-47，如 zh-CN / en-US / zh-Hant-TW）→ Minecraft 语言码
// （options.txt 的 lang 值，如 zh_cn / en_us）。Minecraft 只认它自带语言
// 文件的那组代码，映射不到时返回空串（调用方保持游戏默认，不写脏值）。

// minecraftLanguageCodes Minecraft 自带语言文件对应的语言码集合（覆盖 1.21 全集的常用部分）。
var minecraftLanguageCodes = map[string]bool{
	"ar_sa": true, "az_az": true, "bg_bg": true, "ca_es": true, "cs_cz": true,
	"da_dk": true, "de_at": true, "de_ch": true, "de_de": true, "el_gr": true,
	"en_au": true, "en_ca": true, "en_gb": true, "en_nz": true, "en_us": true,
	"es_ar": true, "es_cl": true, "es_ec": true, "es_es": true, "es_mx": true,
	"es_uy": true, "es_ve": true, "fi_fi": true, "fr_ca": true, "fr_fr": true,
	"he_il": true, "hi_in": true, "hr_hr": true, "hu_hu": true, "id_id": true,
	"is_is": true, "it_it": true, "ja_jp": true, "ko_kr": true, "nl_nl": true,
	"nn_no": true, "no_no": true, "pl_pl": true, "pt_br": true, "pt_pt": true,
	"ro_ro": true, "ru_ru": true, "sk_sk": true, "sl_si": true, "sr_sp": true,
	"sv_se": true, "th_th": true, "tr_tr": true, "uk_ua": true, "vi_vn": true,
	"zh_cn": true, "zh_hk": true, "zh_tw": true,
}

// languageDefaultRegion 语言只有主标签、没有区域时的兜底区域。
var languageDefaultRegion = map[string]string{
	"ar": "sa", "az": "az", "bg": "bg", "ca": "es", "cs": "cz", "da": "dk",
	"de": "de", "el": "gr", "en": "us", "es": "es", "fi": "fi", "fr": "fr",
	"he": "il", "hi": "in", "hr": "hr", "hu": "hu", "id": "id", "is": "is",
	"it": "it", "ja": "jp", "ko": "kr", "nl": "nl", "no": "no", "nn": "no",
	"pl": "pl", "pt": "br", "ro": "ro", "ru": "ru", "sk": "sk", "sl": "si",
	"sr": "sp", "sv": "se", "th": "th", "tr": "tr", "uk": "ua", "vi": "vn",
	"zh": "cn",
}

// readSystemLocaleForTest 测试注入点：替换系统语言来源（nil 时读真实系统）。
var readSystemLocaleForTest = readSystemLocale

// systemMinecraftLanguage 返回系统语言对应的 Minecraft 语言码；无法识别返回空串。
func systemMinecraftLanguage() string {
	reader := readSystemLocaleForTest
	if reader == nil {
		reader = readSystemLocale
	}

	return minecraftLanguageCode(reader())
}

// minecraftLanguageCode 把 BCP-47 区域名映射成 Minecraft 语言码。
func minecraftLanguageCode(locale string) string {
	locale = strings.TrimSpace(strings.ToLower(strings.ReplaceAll(locale, "_", "-")))
	if locale == "" {
		return ""
	}

	lang, script, region := parseBcp47(locale)
	if lang == "" {
		return ""
	}
	// 中文按文字体系区分：简体 zh_cn，繁体 zh_tw（含 zh-hant，无区域时），
	// 港澳使用 zh_hk；其它情况默认简体。
	if lang == "zh" {
		switch {
		case region == "hk" || region == "mo":
			return "zh_hk"
		case script == "hant" || region == "tw":
			return "zh_tw"
		default:
			return "zh_cn"
		}
	}

	if region == "" {
		region = languageDefaultRegion[lang]
	}
	if region == "" {
		return ""
	}
	code := lang + "_" + region
	if !minecraftLanguageCodes[code] {
		// 系统 region 对应的语言文件 MC 没有（如 en_xx），回落默认区域
		code = lang + "_" + languageDefaultRegion[lang]
		if !minecraftLanguageCodes[code] {
			return ""
		}
	}

	return code
}

// parseBcp47 拆出主语言、文字体系（Hans/Hant 等 4 字母标签）与区域。
func parseBcp47(locale string) (lang, script, region string) {
	for _, part := range strings.Split(locale, "-") {
		switch {
		case part == "":
			// 忽略空段
		case len(part) == 4: // 文字体系标签固定 4 字母（hans / hant / cyrl…）
			if script == "" {
				script = part
			}
		case len(part) == 2 && region == "" && lang != "":
			region = part
		case lang == "":
			lang = part
		}
	}

	return lang, script, region
}
