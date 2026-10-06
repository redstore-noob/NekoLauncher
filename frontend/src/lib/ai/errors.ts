/*
 * AI 助手逻辑层（由 layouts/ai.tsx 拆分）：API 错误分类与友好提示文案。
 */
import { t } from "../../i18n";

// API 错误分类与友好提示
// ------------------------------------------------------------------

export type ErrorCategory =
  | "not_configured"
  | "auth_failed"
  | "insufficient_balance"
  | "rate_limited"
  | "content_moderated"
  | "model_not_found"
  | "server_error"
  | "network_error"
  | "unknown";

export interface ClassifiedError {
  category: ErrorCategory;
  title: string;
  message: string;
  suggestion: string;
}

export function classifyApiError(error: unknown): ClassifiedError {
  const raw = error instanceof Error ? error.message : String(error);
  const lower = raw.toLowerCase();

  // 网络错误
  if (
    lower.includes("failed to fetch") ||
    lower.includes("networkerror") ||
    lower.includes("network error") ||
    lower.includes("enotfound") ||
    lower.includes("econnrefused") ||
    lower.includes("timeout") ||
    lower.includes("timed out")
  ) {
    return {
      category: "network_error",
      title: t("网络连接失败"),
      message: raw,
      suggestion: t("请检查网络连接，或确认 API 地址是否正确、是否需要代理。"),
    };
  }

  // 提取 HTTP 状态码
  const statusMatch = raw.match(/HTTP\s*(\d{3})/);
  const status = statusMatch ? parseInt(statusMatch[1], 10) : 0;

  // 401 / 403 认证失败
  if (
    status === 401 ||
    status === 403 ||
    lower.includes("invalid_api_key") ||
    lower.includes("incorrect api key") ||
    lower.includes("unauthorized")
  ) {
    return {
      category: "auth_failed",
      title: t("API Key 无效或已过期"),
      message: raw,
      suggestion: t(
        "请在左下角「AI 设置」中检查 API Key 是否正确填写，是否已过期或被撤销。",
      ),
    };
  }

  // 402 余额不足
  if (
    status === 402 ||
    lower.includes("insufficient_quota") ||
    lower.includes("insufficient quota") ||
    lower.includes("billing") ||
    lower.includes("余额不足") ||
    lower.includes("欠费") ||
    lower.includes("no credit") ||
    lower.includes("out of credit")
  ) {
    return {
      category: "insufficient_balance",
      title: t("API 余额不足"),
      message: raw,
      suggestion: t(
        "该 API Key 的账户余额已用尽，请前往对应平台充值后再试，或更换其他 API Key。",
      ),
    };
  }

  // 429 速率限制
  if (
    status === 429 ||
    lower.includes("rate_limit") ||
    lower.includes("rate limit") ||
    lower.includes("too many requests")
  ) {
    if (lower.includes("insufficient_quota") || lower.includes("quota")) {
      return {
        category: "insufficient_balance",
        title: t("API 配额/余额不足"),
        message: raw,
        suggestion: t(
          "已达到该 API Key 的配额上限或余额不足，请充值后再试，或稍后重试。",
        ),
      };
    }

    return {
      category: "rate_limited",
      title: t("请求过于频繁"),
      message: raw,
      suggestion: t(
        "已触发 API 供应商的速率限制，请稍等片刻后再试，或降低发送频率。",
      ),
    };
  }

  // 内容审查 / 安全策略
  if (
    lower.includes("content_policy") ||
    lower.includes("content policy") ||
    lower.includes("moderation") ||
    lower.includes("safety") ||
    lower.includes("sensitive") ||
    lower.includes("inappropriate") ||
    lower.includes("harmful") ||
    lower.includes("violation") ||
    lower.includes("审查") ||
    lower.includes("违规") ||
    lower.includes("敏感") ||
    lower.includes("安全策略") ||
    lower.includes("内容审核") ||
    lower.includes("refusal")
  ) {
    return {
      category: "content_moderated",
      title: t("内容被安全策略拦截"),
      message: raw,
      suggestion: t(
        "该请求触发了 API 供应商的内容安全审查。请尝试调整措辞，避免涉及敏感、违规或不安全的内容。",
      ),
    };
  }

  // 404 模型不存在
  if (
    status === 404 ||
    lower.includes("model_not_found") ||
    lower.includes("model not found") ||
    lower.includes("no such model")
  ) {
    return {
      category: "model_not_found",
      title: t("模型不存在或无权访问"),
      message: raw,
      suggestion: t(
        "请检查模型名称是否拼写正确，以及该 API Key 是否有权限访问此模型。",
      ),
    };
  }

  // 5xx 服务器错误
  if (status >= 500 && status < 600) {
    return {
      category: "server_error",
      title: t("API 服务器错误"),
      message: raw,
      suggestion: t(
        "API 供应商的服务器暂时不可用，这通常是临时问题，请稍后重试。",
      ),
    };
  }

  // 未知错误
  return {
    category: "unknown",
    title: t("调用失败"),
    message: raw,
    suggestion: t(
      "请检查 API Key、API 地址、模型名称是否正确，以及网络连接是否正常。",
    ),
  };
}

export function formatErrorMessage(err: ClassifiedError): string {
  return `❌ **${err.title}**\n\n${err.message}\n\n💡 ${err.suggestion}`;
}
