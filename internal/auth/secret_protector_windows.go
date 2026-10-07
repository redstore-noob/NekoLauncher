//go:build windows

package auth

import (
	"encoding/base64"
	"syscall"
	"unsafe"
)

// platformSecretUsesDPAPI Windows 使用 DPAPI（CurrentUser）。
func platformSecretUsesDPAPI() bool { return true }

// platformProtectDPAPI 加密并剥掉前缀后的 Base64 密文；失败返回空串。
func platformProtectDPAPI(plaintext string) string { return dpapiProtect(plaintext) }

// platformUnprotectDPAPI 解密（入参已去掉 EncryptedPrefix）；失败返回空串。
func platformUnprotectDPAPI(encodedBase64 string) string { return dpapiUnprotect(encodedBase64) }

var (
	crypt32                = syscall.NewLazyDLL("crypt32.dll")
	procCryptProtectData   = crypt32.NewProc("CryptProtectData")
	procCryptUnprotectData = crypt32.NewProc("CryptUnprotectData")
)

// cryptProtectUiForbidden 对应 CRYPTPROTECT_UI_FORBIDDEN。
const cryptProtectUiForbidden = 0x1

// dataBlob 对应 Win32 CRYPTOAPI_BLOB（与 C# DATA_BLOB 布局一致）。
type dataBlob struct {
	cbData uint32
	pbData *byte
}

// dpapiProtect 使用 DPAPI（CurrentUser）加密，返回去掉前缀后的 Base64 密文；
// 失败返回空串。
func dpapiProtect(plaintext string) string {
	plain := []byte(plaintext)
	if len(plain) == 0 {
		// 空明文没有可加密字节：与加密失败一致返回空串，避免 &plain[0] 越界 panic
		return ""
	}
	var in, entropy, out dataBlob
	in.cbData = uint32(len(plain))
	in.pbData = &plain[0]
	entropy.cbData = uint32(len(additionalEntropy))
	entropy.pbData = &additionalEntropy[0]

	ret, _, _ := procCryptProtectData.Call(
		uintptr(unsafe.Pointer(&in)),
		0, // szDataDescr
		uintptr(unsafe.Pointer(&entropy)),
		0, 0,
		uintptr(cryptProtectUiForbidden),
		uintptr(unsafe.Pointer(&out)),
	)
	if ret == 0 {
		return ""
	}
	defer localFree(out.pbData)
	result := make([]byte, out.cbData)
	copy(result, unsafe.Slice(out.pbData, out.cbData))
	return base64.StdEncoding.EncodeToString(result)
}

// dpapiUnprotect 解密 dpapiProtect 产出的 Base64 密文（无前缀）；失败返回空串。
func dpapiUnprotect(encodedBase64 string) string {
	cipherBytes, err := base64.StdEncoding.DecodeString(encodedBase64)
	if err != nil || len(cipherBytes) == 0 {
		return ""
	}
	var in, entropy, out dataBlob
	in.cbData = uint32(len(cipherBytes))
	in.pbData = &cipherBytes[0]
	entropy.cbData = uint32(len(additionalEntropy))
	entropy.pbData = &additionalEntropy[0]

	ret, _, _ := procCryptUnprotectData.Call(
		uintptr(unsafe.Pointer(&in)),
		0, // ppszDataDescr
		uintptr(unsafe.Pointer(&entropy)),
		0, 0,
		uintptr(cryptProtectUiForbidden),
		uintptr(unsafe.Pointer(&out)),
	)
	if ret == 0 {
		return ""
	}
	defer localFree(out.pbData)
	plain := make([]byte, out.cbData)
	copy(plain, unsafe.Slice(out.pbData, out.cbData))
	return string(plain)
}

// kernel32 与 LocalFree 提为包级变量：localFree 每次调用都 NewLazyDLL
// 会重复做 DLL 查找与过程绑定（DPAPI 解密热路径上每个账号一次）
var (
	kernel32DLL   = syscall.NewLazyDLL("kernel32.dll")
	localFreeProc = kernel32DLL.NewProc("LocalFree")
)

func localFree(pointer *byte) {
	localFreeProc.Call(uintptr(unsafe.Pointer(pointer)))
}
