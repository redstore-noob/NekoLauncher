/*
 * 内嵌微软登录全局进度浮层。
 * 内嵌登录会把主窗口导航到微软登录页、授权后再跳回启动器——SPA 在这个
 * 往返中被重载，登录前的弹层与事件订阅全部失效。因此进度状态由 Go 侧持有，
 * 这里在应用外壳全局挂载：挂载时轮询 GetMicrosoftLoginState 接力，
 * 之后的推进靠 auth:microsoftProgress 事件实时刷新。
 */
import type { bindings } from "../../wailsjs/go/models";

import React, { useEffect, useState } from "react";
import { Button, Modal, ModalContent, Progress } from "@heroui/react";
import {
  CheckmarkCircle20Regular,
  DismissCircle20Regular,
} from "@fluentui/react-icons";

import {
  CancelMicrosoftLogin,
  DismissMicrosoftLoginState,
  GetMicrosoftLoginState,
} from "../../wailsjs/go/bindings/AccountAPI";
import { EventsOn } from "../../wailsjs/runtime/runtime";
import { t } from "../i18n";

import { notify } from "./overlay/dialog";
import { ModalShell, modalBehaviorProps } from "./modal-shell";

type LoginState = bindings.MicrosoftBrowserLoginState;

// 登录结束事件：账户页监听它刷新账号列表（跨组件用 CustomEvent，
// 避免全局浮层与页面互相持有引用）。
export const MICROSOFT_LOGIN_FINISHED_EVENT =
  "nekolauncher:microsoft-login-finished";

function hasContent(state: LoginState | null | undefined): boolean {
  return !!state && (state.Active || state.Done || state.Failed);
}

const MicrosoftLoginProgress: React.FC = () => {
  const [state, setState] = useState<LoginState | null>(null);

  useEffect(() => {
    let alive = true;

    // 注入到微软页面的导航条"返回启动器"会带 ?msoauth=cancel 跳回；
    // 这里检测标记、通知后端取消本轮登录，并把参数从地址栏清掉。
    const params = new URLSearchParams(window.location.search);

    if (params.get("msoauth") === "cancel") {
      window.history.replaceState(null, "", window.location.pathname);
      CancelMicrosoftLogin().catch(() => {
        /* 无进行中的登录时后端报错，忽略 */
      });
      notify.info(t("已返回启动器，内嵌登录已取消"));
    }

    GetMicrosoftLoginState()
      .then((current) => {
        if (alive && hasContent(current as LoginState))
          setState(current as LoginState);
      })
      .catch(() => {
        /* 后端未就绪时忽略，等事件驱动 */
      });
    const unsubscribe = EventsOn(
      "auth:microsoftProgress",
      (next: LoginState) => {
        if (hasContent(next)) setState(next);
      },
    );

    return () => {
      alive = false;
      unsubscribe?.();
    };
  }, []);

  // 结束时广播（账户页刷新列表），完成后停留片刻自动收起
  useEffect(() => {
    if (!state || !(state.Done || state.Failed)) return;
    window.dispatchEvent(
      new CustomEvent(MICROSOFT_LOGIN_FINISHED_EVENT, { detail: state }),
    );
    const timer = window.setTimeout(() => void dismiss(), 5000);

    return () => window.clearTimeout(timer);
  }, [state?.Done, state?.Failed]); // eslint-disable-line react-hooks/exhaustive-deps

  const dismiss = async () => {
    setState(null);
    try {
      await DismissMicrosoftLoginState();
    } catch {
      /* 后端已清理时忽略 */
    }
  };

  if (!state) return null;

  return (
    <Modal isOpen size="sm" {...modalBehaviorProps}>
      <ModalContent>
        <ModalShell
          icon={
            state.Done ? (
              <CheckmarkCircle20Regular className="text-success" />
            ) : state.Failed ? (
              <DismissCircle20Regular className="text-danger" />
            ) : undefined
          }
          title={
            state.Done
              ? t("正版账号登录成功")
              : state.Failed
                ? t("微软登录失败")
                : t("正在登录微软账号")
          }
          onClose={() => void dismiss()}
        >
          <div className="flex flex-col gap-3">
            {state.Active ? (
              <>
                <Progress
                  aria-label={t("登录进度")}
                  isIndeterminate={state.Percent < 12}
                  size="sm"
                  value={state.Percent}
                />
                <div className="text-xs text-gray-500 dark:text-gray-400">
                  {state.Message}
                </div>
                <div className="flex justify-end">
                  <Button
                    color="danger"
                    radius="full"
                    size="sm"
                    variant="flat"
                    onPress={() => {
                      CancelMicrosoftLogin().catch(() => {
                        /* 后端已结束时忽略 */
                      });
                    }}
                  >
                    {t("取消登录")}
                  </Button>
                </div>
              </>
            ) : state.Done ? (
              <>
                <div className="text-sm text-gray-700 dark:text-gray-200">
                  {t("已添加账号：{0}", { "0": state.Username })}
                </div>
                <div className="flex justify-end">
                  <Button
                    color="primary"
                    radius="full"
                    size="sm"
                    onPress={() => void dismiss()}
                  >
                    {t("完成")}
                  </Button>
                </div>
              </>
            ) : (
              <>
                <div className="text-sm break-all text-gray-700 dark:text-gray-200">
                  {state.ErrorText}
                </div>
                <div className="flex justify-end">
                  <Button
                    color="primary"
                    radius="full"
                    size="sm"
                    onPress={() => void dismiss()}
                  >
                    {t("知道了")}
                  </Button>
                </div>
              </>
            )}
          </div>
        </ModalShell>
      </ModalContent>
    </Modal>
  );
};

export default MicrosoftLoginProgress;
