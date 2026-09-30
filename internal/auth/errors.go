// Package auth 移植自 NyaLauncher.Core/Launch/Auth：Microsoft 设备码认证、
// authlib-injector 皮肤站认证、账号存储与敏感信息保护。
package auth

import "fmt"

// Microsoft 认证流程（设备码、Xbox、Minecraft 服务）失败时返回的错误。
// ErrorCode 携带原始错误码，便于调用方区分失败原因。
type MicrosoftAuthenticationError struct {
	Message   string
	Inner     error
	ErrorCode string
}

func (e *MicrosoftAuthenticationError) Error() string {
	if e.Inner != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Inner)
	}
	return e.Message
}

func (e *MicrosoftAuthenticationError) Unwrap() error { return e.Inner }

// 常量别名保留 C# 异常类的错误码常量（大小写与原始协议一致）。
const (
	// AuthorizationDeclined OAuth 设备码流程：用户拒绝授权。
	AuthorizationDeclined = "authorization_declined"
	// DeviceCodeExpired OAuth 设备码流程：设备码过期。
	DeviceCodeExpired = "expired_token"

	// XboxNoAccount 账号没有 Xbox Live 档案（XErr）。
	XboxNoAccount = "2148916233"
	// XboxChildAccount 儿童账号，需要家长加入家庭组。
	XboxChildAccount = "2148916235"
	// XboxConsentRequired 需要家长同意（可能未满 18 岁）。
	XboxConsentRequired = "2148916236"
	// XboxRegionBlocked 当前国家或地区不支持。
	XboxRegionBlocked = "2148916237"
	// XboxAgeVerification 需要年龄验证。
	XboxAgeVerification = "2148916238"
)

func newMicrosoftAuthError(message string) *MicrosoftAuthenticationError {
	return &MicrosoftAuthenticationError{Message: message}
}

func newMicrosoftAuthErrorCode(message, errorCode string) *MicrosoftAuthenticationError {
	return &MicrosoftAuthenticationError{Message: message, ErrorCode: errorCode}
}

func newMicrosoftAuthErrorWrap(message string, inner error) *MicrosoftAuthenticationError {
	return &MicrosoftAuthenticationError{Message: message, Inner: inner}
}

// RotatedCredentialsError 「刷新令牌已成功轮换，但后续 Minecraft 档案交换
// （XBL/XSTS/MC 登录/档案获取）失败」的复合错误。此时旧的 refresh_token 已被消费，
// RefreshedAccount 携带轮换后的新凭据——调用方捕获后必须立即持久化
// （AccountStoreService.UpdateMicrosoftAccount），否则下次刷新将因旧令牌失效而强制用户重新登录。
type RotatedCredentialsError struct {
	RefreshedAccount MicrosoftAccount
	Message          string
	Inner            error
}

func (e *RotatedCredentialsError) Error() string {
	return e.Message
}

func (e *RotatedCredentialsError) Unwrap() error { return e.Inner }

// AuthlibAuthenticationError 皮肤站认证流程中抛出的错误；Message 已面向用户可读。
// CaptchaRequired 为 true 表示皮肤站开启了验证码（Blessing Skin 私有行为，
// Yggdrasil 规范无此扩展）：调用方应引导用户到皮肤站网页完成验证后重试。
type AuthlibAuthenticationError struct {
	Message        string
	Inner          error
	CaptchaRequired bool
}

func (e *AuthlibAuthenticationError) Error() string {
	if e.Inner != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Inner)
	}
	return e.Message
}

func (e *AuthlibAuthenticationError) Unwrap() error { return e.Inner }

func newAuthlibError(message string) *AuthlibAuthenticationError {
	return &AuthlibAuthenticationError{Message: message}
}

// newAuthlibCaptchaError 皮肤站要求验证码时返回的结构化错误：
// Message 引导用户到网页端完成验证；CaptchaRequired 供绑定层/前端识别。
func newAuthlibCaptchaError(message string) *AuthlibAuthenticationError {
	return &AuthlibAuthenticationError{Message: message, CaptchaRequired: true}
}

func newAuthlibErrorWrap(message string, inner error) *AuthlibAuthenticationError {
	return &AuthlibAuthenticationError{Message: message, Inner: inner}
}
