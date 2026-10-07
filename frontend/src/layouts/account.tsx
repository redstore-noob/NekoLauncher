/*
 * 账户管理页 —— 移植自旧版 views/settings/AccountView.tsx +
 * components/overlay/AccountLoginOverlay.tsx（对应 Avalonia AccountManagePage）。
 * 账号列表/默认账号/删除 + 三种添加流程（离线 / 微软设备码 / 皮肤站）+
 * 玩家外观（正版上传皮肤与披风激活、离线自定义皮肤与内置皮肤库）。
 * 界面为毛玻璃卡片 + Fluent 图标风格（与音乐/下载页统一）。
 * 注意：Wails WebView2 不支持 window.confirm/alert，所有确认交互均用两步按钮或弹层实现。
 */
import type { auth, bindings } from "../../wailsjs/go/models";

import React, { useEffect, useRef, useState } from "react";
import {
  Button,
  Input,
  Modal,
  ModalContent,
  Select,
  SelectItem,
  Tooltip,
} from "@heroui/react";
// 图标统一用 Fluent UI System Icons（20px 系）
import {
  Person20Regular,
  PersonAdd20Regular,
  Key20Regular,
  Globe20Regular,
  Delete20Regular,
  Star20Regular,
  ArrowUpload20Regular,
  FolderOpen20Regular,
  PaintBrush20Regular,
  Shirt20Regular,
  Copy20Regular,
  Checkmark20Regular,
  Info20Regular,
} from "@fluentui/react-icons";
import { AnimatePresence, motion } from "framer-motion";

import { selectPopoverProps } from "../lib/motion";
import { ModalShell, modalBehaviorProps } from "../components/modal-shell";
import SwitchTransition, {
  useSwitchDirection,
} from "../components/screen-transition";
import SegmentedTabs from "../components/segmented-tabs";
import { listItemVariants } from "../lib/motion";
import SkinPreviewPanel from "../components/account/SkinPreviewPanel";
import {
  GetAccounts,
  GetAccountStableKey,
  RemoveAccount,
  MoveAccountToTop,
  GetAvatarUrl,
  HasOfflineName,
  CreateOfflineAccount,
  LoginMicrosoft,
  LoginMicrosoftBrowser,
  CancelMicrosoftLogin,
  ConfirmAuthlibProfile,
  ResolveAuthlibServer,
  AuthlibLogin,
  UploadSkin,
  SetOfflineSkin,
  GetOfflineSkinCatalog,
  GetMinecraftProfile,
  SetActiveCape,
} from "../../wailsjs/go/bindings/AccountAPI";
import { SelectFile } from "../../wailsjs/go/bindings/SystemAPI";
import { asArray } from "../lib/guards";
import {
  EventsOn,
  BrowserOpenURL,
  ClipboardSetText,
} from "../../wailsjs/runtime/runtime";
import { t } from "../i18n";

import { useSimpleMode } from "./simple-mode";

type LaunchAccount = auth.LaunchAccount;
type CapeTexture = bindings.MinecraftProfileTexture;
type SkinChoice = bindings.OfflineSkinChoice;

function typeLabel(account: LaunchAccount): string {
  switch (account.Type) {
    case "microsoft":
      return t("正版账户");
    case "offline":
      return t("离线账户");
    case "authlib":
      return t("皮肤站账户");
    default:
      return t("第三方账户");
  }
}

// 账号类型徽章配色（主色 / 次色 / 中性）
function typeChipClass(type: string): string {
  switch (type) {
    case "microsoft":
      return "bg-primary/15 text-primary";
    case "authlib":
      return "bg-secondary/15 text-secondary-600 dark:text-secondary-400";
    default:
      return "bg-default-100 text-gray-500";
  }
}

function accountDetail(account: LaunchAccount): string {
  switch (account.Type) {
    case "microsoft":
      if (account.Microsoft) {
        const raw =
          typeof account.Microsoft.ExpiresAt === "number" &&
          account.Microsoft.ExpiresAt > 1e15
            ? account.Microsoft.ExpiresAt / 1e6
            : (account.Microsoft.ExpiresAt ?? 0);
        const expired = new Date(raw).getTime() <= Date.now();

        return expired
          ? t("令牌已过期，启动游戏时会自动刷新")
          : t("令牌有效期至 {0}", {
              "0": formatTime(account.Microsoft.ExpiresAt),
            });
      }

      return typeLabel(account);
    case "authlib":
      if (account.Authlib) {
        return account.Authlib.ServerName
          ? account.Authlib.ServerName
          : account.Authlib.ApiRoot || "";
      }

      return typeLabel(account);
    default:
      return typeLabel(account);
  }
}

function formatTime(value: unknown): string {
  const date = new Date(
    typeof value === "number" && value > 1e15 ? value / 1e6 : (value as string),
  );

  if (Number.isNaN(date.getTime())) return String(value ?? "");
  const pad = (n: number) => String(n).padStart(2, "0");

  return (
    `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())} ` +
    `${pad(date.getHours())}:${pad(date.getMinutes())}`
  );
}

interface AccountRow {
  account: LaunchAccount;
  stableKey: string;
  avatar: string;
}

const AccountPage: React.FC = () => {
  // S 模式隐藏皮肤 3D 展示（skinview3d + three 的预览卡）
  const { simpleMode } = useSimpleMode();
  const [rows, setRows] = useState<AccountRow[]>([]);
  const [selectedKey, setSelectedKey] = useState("");
  const [status, setStatus] = useState("");
  const [avatarMap, setAvatarMap] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState(false);
  const [confirmingDeleteKey, setConfirmingDeleteKey] = useState("");

  // ---- 添加账号弹层 ----
  const [addOpen, setAddOpen] = useState(false);
  const [addTab, setAddTab] = useState("offline");
  // 添加账号分段标签的切换方向
  const addTabDirection = useSwitchDirection(
    ["offline", "microsoft", "authlib"].indexOf(addTab),
  );
  const [addHint, setAddHint] = useState("");
  // 离线
  const [offlineName, setOfflineName] = useState("Player_01");
  // 设备码
  const [deviceCode, setDeviceCode] = useState("");
  const [msBusy, setMsBusy] = useState(false);
  const msActive = useRef(false);
  // 内嵌浏览器登录（主窗口跳微软登录页，授权后自动跳回）
  const [browserStarting, setBrowserStarting] = useState(false);
  // 皮肤站
  const [extServer, setExtServer] = useState("");
  const [extUsername, setExtUsername] = useState("");
  const [extPassword, setExtPassword] = useState("");
  const [extBusy, setExtBusy] = useState(false);
  const [extProfiles, setExtProfiles] = useState<
    Array<{ Index: number; Name: string }>
  >([]);
  const pendingExt = useRef<{
    server: auth.AuthlibServerInfo;
    username: string;
  } | null>(null);

  // 账号操作反馈与复制提示
  const [copiedKey, setCopiedKey] = useState("");
  const deleteTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const [codeCopied, setCodeCopied] = useState(false);

  // 常用皮肤站预设
  const PRESET_AUTHLIB_SERVERS = [
    { name: "LittleSkin", url: "https://littleskin.cn" },
    { name: "Blessing Skin", url: "https://skin.prinzeugen.net" },
    { name: "Ely.by", url: "https://authlib-injector.ely.by" },
  ];
  const [capeOpen, setCapeOpen] = useState(false);
  const [capeLoading, setCapeLoading] = useState(false);
  const [capeBusy, setCapeBusy] = useState(false);
  const [capeError, setCapeError] = useState("");
  const [capeList, setCapeList] = useState<CapeTexture[]>([]);
  const [catalogOpen, setCatalogOpen] = useState(false);
  const [catalog, setCatalog] = useState<SkinChoice[]>([]);
  const [modelOpen, setModelOpen] = useState(false);
  const [modelBusy, setModelBusy] = useState(false);
  const pendingSkinPath = useRef("");

  const selected = rows.find((r) => r.stableKey === selectedKey) ?? null;

  // 初始加载 / 微软登录事件 / 增删账号后的 reload 可能交叠：
  // 慢的旧响应会把新列表（甚至已删除的账号）盖回来，用自增序号只让最后一次落盘
  const reloadSeqRef = useRef(0);

  const reload = async () => {
    const seq = ++reloadSeqRef.current;

    try {
      const list = asArray<LaunchAccount>(await GetAccounts());
      const withKeys = await Promise.all(
        list.map(async (account) => ({
          account,
          stableKey: await GetAccountStableKey(account as never),
          avatar: "",
        })),
      );

      if (seq !== reloadSeqRef.current) return; // 已有更新的刷新在途，丢弃旧结果
      setRows(() =>
        withKeys.map((row) => ({
          ...row,
          avatar: avatarMap[row.stableKey] ?? "",
        })),
      );
      setSelectedKey(
        withKeys.some((r) => r.stableKey === selectedKey)
          ? selectedKey
          : (withKeys[0]?.stableKey ?? ""),
      );
      setStatus(withKeys.length === 0 ? t("暂无账号") : "");
      // 异步加载真实皮肤头像（失败保留字母占位）
      withKeys.forEach(({ stableKey }) => {
        GetAvatarUrl(stableKey)
          .then((uri) => {
            if (uri)
              setAvatarMap((m) =>
                m[stableKey] === uri ? m : { ...m, [stableKey]: uri },
              );
          })
          .catch(() => {
            /* 字母占位 */
          });
      });
    } catch (ex) {
      if (seq !== reloadSeqRef.current) return;
      setStatus(
        t("读取账号列表失败：{0}", { "0": (ex as Error)?.message ?? ex }),
      );
    }
    // avatarMap 闭包仅在刷新瞬间参与，头像并入由下方 effect 处理
  };

  useEffect(() => {
    reload();
  }, []); // eslint-disable-line react-hooks/exhaustive-deps

  // 内嵌微软登录在 Go 侧直接入库（SPA 经登录页跳转往返后原调用方已丢失），
  // 结束后这里只负责刷新列表。全局进度浮层负责展示。
  useEffect(
    () =>
      EventsOn("auth:microsoftLoggedIn", () => {
        void reload();
      }),
    [], // eslint-disable-line react-hooks/exhaustive-deps
  );

  // 头像加载完成后并入行数据
  useEffect(() => {
    setRows((prev) => {
      let changed = false;
      const next = prev.map((row) => {
        const uri = avatarMap[row.stableKey];

        if (uri && row.avatar !== uri) {
          changed = true;

          return { ...row, avatar: uri };
        }

        return row;
      });

      return changed ? next : prev;
    });
  }, [avatarMap]);

  // 切换选中账号后退出「确认删除」状态
  useEffect(() => {
    setConfirmingDeleteKey("");
    if (deleteTimerRef.current) {
      clearTimeout(deleteTimerRef.current);
      deleteTimerRef.current = null;
    }
  }, [selectedKey]);

  useEffect(() => {
    return () => {
      if (deleteTimerRef.current) clearTimeout(deleteTimerRef.current);
    };
  }, []);

  const copyAccountName = async (e: React.MouseEvent, row: AccountRow) => {
    e.stopPropagation();
    try {
      await ClipboardSetText(row.account.DisplayName);
      setCopiedKey(row.stableKey);
      setTimeout(() => {
        setCopiedKey((prev) => (prev === row.stableKey ? "" : prev));
      }, 1800);
    } catch {
      /* ignore */
    }
  };

  // ---- 账号操作 ----

  const setDefault = async (row: AccountRow) => {
    try {
      await MoveAccountToTop(row.account as never);
      setStatus(t("已设为默认：{0}", { "0": row.account.DisplayName }));
      await reload();
    } catch (ex) {
      setStatus(
        t("设置默认账号失败：{0}", { "0": (ex as Error)?.message ?? ex }),
      );
    }
  };

  const removeAccount = async (row: AccountRow) => {
    // 两步确认（WebView2 不支持 window.confirm）；每个账号各自确认，4 秒内未再次点击自动恢复
    if (confirmingDeleteKey !== row.stableKey) {
      setConfirmingDeleteKey(row.stableKey);
      if (deleteTimerRef.current) clearTimeout(deleteTimerRef.current);
      deleteTimerRef.current = setTimeout(() => {
        setConfirmingDeleteKey((prev) => (prev === row.stableKey ? "" : prev));
      }, 4000);

      return;
    }
    if (deleteTimerRef.current) {
      clearTimeout(deleteTimerRef.current);
      deleteTimerRef.current = null;
    }
    setConfirmingDeleteKey("");
    try {
      await RemoveAccount(row.account as never);
      setStatus(t("已删除账号：{0}", { "0": row.account.DisplayName }));
      setSelectedKey("");
      await reload();
    } catch (ex) {
      setStatus(t("删除账号失败：{0}", { "0": (ex as Error)?.message ?? ex }));
    }
  };

  // ---- 添加：离线 ----

  const addOffline = async () => {
    const name = offlineName.trim();

    if (!name) {
      setAddHint(t("请输入离线用户名"));

      return;
    }
    try {
      if (await HasOfflineName(name)) {
        setAddHint(t("已存在同名离线账号：{0}", { "0": name }));

        return;
      }
      await CreateOfflineAccount(name);
      setAddOpen(false);
      setStatus(t("已添加离线账号：{0}", { "0": name }));
      await reload();
    } catch (ex) {
      setAddHint(
        t("创建离线账号失败：{0}", { "0": (ex as Error)?.message ?? ex }),
      );
    }
  };

  // ---- 添加：微软设备码 ----

  const onDeviceCodeEvent = (info: {
    UserCode?: string;
    VerificationUri?: string;
  }) => {
    setDeviceCode(info?.UserCode ?? "");
    ClipboardSetText(info?.UserCode ?? "").catch(() => {
      /* ignore */
    });
    if (info?.VerificationUri) {
      BrowserOpenURL(
        `https://www.microsoft.com/link?user_code=${info.UserCode ?? ""}`,
      );
    }
  };

  // 设备码事件订阅只摘自己的监听：EventsOff("auth:deviceCode") 会把该事件
  // 的全部监听器清掉——旧一轮登录的异步 catch 触发时会把新一轮登录的监听
  // 一并杀死，新登录就永远卡在"正在请求设备码…"。
  const deviceCodeUnsubRef = useRef<(() => void) | null>(null);

  const subscribeDeviceCode = () => {
    deviceCodeUnsubRef.current?.();
    deviceCodeUnsubRef.current = EventsOn("auth:deviceCode", onDeviceCodeEvent);
  };

  const unsubscribeDeviceCode = () => {
    deviceCodeUnsubRef.current?.();
    deviceCodeUnsubRef.current = null;
  };

  // 卸载（切页）时退订并取消后端轮询：后台轮询会白跑 15 分钟
  useEffect(
    () => () => {
      unsubscribeDeviceCode();
      msActive.current = false;
      CancelMicrosoftLogin().catch(() => {
        /* 后端已结束轮询时忽略 */
      });
    },
    [],
  );

  const startMicrosoftLogin = () => {
    setAddTab("microsoft");
    setMsBusy(true);
    setDeviceCode("");
    setAddHint("");
    msActive.current = true;
    subscribeDeviceCode();

    LoginMicrosoft()
      .then(async (username) => {
        msActive.current = false;
        unsubscribeDeviceCode();
        // 登录结果（含两类令牌与 XUID/UUID）由后端直接入库，
        // 前端只收到玩家名
        setMsBusy(false);
        setAddOpen(false);
        setStatus(t("正版账号登录成功：{0}", { "0": username }));
        await reload();
      })
      .catch((ex) => {
        const cancelled = !msActive.current;

        msActive.current = false;
        unsubscribeDeviceCode();
        setMsBusy(false);
        if (!cancelled)
          setAddHint(
            t("微软账号登录失败：{0}", { "0": (ex as Error)?.message ?? ex }),
          );
      });
  };

  const cancelMicrosoftLogin = async () => {
    msActive.current = false;
    unsubscribeDeviceCode();
    try {
      await CancelMicrosoftLogin();
    } catch {
      /* 后端已结束轮询时忽略 */
    }
    setMsBusy(false);
  };

  // ---- 添加：微软内嵌浏览器登录 ----
  // 主窗口直接导航到微软登录页（不弹系统浏览器），授权后自动跳回启动器；
  // 进度由全局浮层展示，这里只负责发起。
  const startMicrosoftBrowserLogin = async () => {
    if (browserStarting) return;
    setBrowserStarting(true);
    setAddHint("");
    try {
      await LoginMicrosoftBrowser(window.location.origin);
      setAddHint(t("正在打开微软登录页，请在页面中完成授权…"));
    } catch (ex) {
      setAddHint(
        t("内嵌登录启动失败：{0}", { "0": (ex as Error)?.message ?? ex }),
      );
    }
    setBrowserStarting(false);
  };

  // ---- 添加：皮肤站 ----

  const addExternal = async (
    profile: { Index: number; Name: string },
    _username: string,
  ) => {
    if (!pendingExt.current) return;

    // 选角与入库全在后端完成（会话内含访问令牌与角色 UUID，不经过 WebView）
    const profileName = await ConfirmAuthlibProfile(profile.Index);

    pendingExt.current = null;
    setExtProfiles([]);
    setAddOpen(false);
    setStatus(t("已添加皮肤站账号：{0}", { "0": profileName }));
    await reload();
  };

  const loginExternal = async () => {
    const serverText = extServer.trim();
    const username = extUsername.trim();

    if (!serverText) {
      setAddHint(t("请输入皮肤站地址"));

      return;
    }
    if (!username || !extPassword) {
      setAddHint(t("请输入皮肤站账号与密码"));

      return;
    }
    setExtBusy(true);
    try {
      setAddHint(t("正在解析皮肤站…"));
      const server = (await ResolveAuthlibServer(
        serverText,
      )) as auth.AuthlibServerInfo;

      setAddHint(t("正在登录 {0}…", { "0": server.ServerName || "皮肤站" }));
      // 令牌与角色 UUID 留在后端会话里；前端只拿到"序号 + 角色名"
      const profiles = await AuthlibLogin(
        server.ApiRoot,
        username,
        extPassword,
      );

      if (!profiles || profiles.length === 0) {
        setAddHint(t("该账号在此皮肤站没有角色档案，请先在皮肤站创建角色。"));

        return;
      }
      pendingExt.current = { server, username };
      if (profiles.length === 1) {
        await addExternal(profiles[0], username);
      } else {
        setExtProfiles(profiles);
        setAddHint(
          t("该账号有 {0} 个角色", {
            "0": profiles.length,
          }),
        );
      }
    } catch (ex) {
      const message = (ex as Error)?.message ?? String(ex);

      // 皮肤站开启验证码（Blessing Skin 私有行为，密码登录无法携带）：
      // 自动在浏览器打开皮肤站首页，引导用户完成一次网页登录验证后回来重试
      if (message.includes("该皮肤站开启了验证码")) {
        const homepage =
          serverText.replace(/\/api\/yggdrasil\/?$/i, "") || serverText;

        void BrowserOpenURL(homepage);
        setAddHint(
          t(
            "该皮肤站开启了验证码：已在浏览器打开皮肤站，请完成一次登录验证后回来重试。",
          ),
        );

        return;
      }

      setAddHint(t("登录失败：{0}", { "0": message }));
    } finally {
      setExtBusy(false);
    }
  };

  const openAddModal = () => {
    setAddHint("");
    setExtProfiles([]);
    setDeviceCode("");
    setAddTab("offline");
    setAddOpen(true);
  };

  // ---- 外观：皮肤 ----

  const changeMicrosoftSkin = async () => {
    if (!selected) return;
    let path = "";

    try {
      path = await SelectFile(
        t("选择 Minecraft Java 皮肤"),
        t("Minecraft 皮肤 PNG"),
        "*.png",
      );
    } catch {
      /* 取消 */
    }
    if (!path) return;
    // WebView2 不支持 confirm：弹出模型选择弹层
    pendingSkinPath.current = path;
    setModelOpen(true);
  };

  const uploadSkinWithModel = async (variant: "classic" | "slim") => {
    if (!selected || !pendingSkinPath.current) return;
    setModelBusy(true);
    try {
      await UploadSkin(selected.stableKey, pendingSkinPath.current, variant);
      setModelOpen(false);
      setStatus(t("正版皮肤已更新。"));
      await reload();
    } catch (ex) {
      setStatus(t("皮肤上传失败：{0}", { "0": (ex as Error)?.message ?? ex }));
    } finally {
      setModelBusy(false);
      pendingSkinPath.current = "";
    }
  };

  const changeOfflineSkinFile = async () => {
    if (!selected) return;
    let path = "";

    try {
      path = await SelectFile(t("选择皮肤贴图（png）"), t("图片文件"), "*.png");
    } catch {
      /* 取消 */
    }
    if (!path) return;
    setBusy(true);
    try {
      await SetOfflineSkin(selected.stableKey, path);
      setStatus(
        t("已设置离线自定义皮肤：{0}", {
          "0": path.split(/[\\/]/).pop() ?? "",
        }),
      );
      await reload();
    } catch (ex) {
      setStatus(t("更换皮肤失败：{0}", { "0": (ex as Error)?.message ?? ex }));
    } finally {
      setBusy(false);
    }
  };

  const openCatalog = async () => {
    if (!selected) return;
    try {
      setCatalog(asArray(await GetOfflineSkinCatalog()));
      setCatalogOpen(true);
    } catch (ex) {
      setStatus(
        t("读取皮肤库失败：{0}", { "0": (ex as Error)?.message ?? ex }),
      );
    }
  };

  const applyCatalogSkin = async (choice: SkinChoice) => {
    if (!selected) return;
    setCatalogOpen(false);
    setBusy(true);
    try {
      await SetOfflineSkin(selected.stableKey, choice.id);
      setStatus(t("已选择离线默认皮肤：{0}", { "0": choice.displayName }));
      await reload();
    } catch (ex) {
      setStatus(t("更换皮肤失败：{0}", { "0": (ex as Error)?.message ?? ex }));
    } finally {
      setBusy(false);
    }
  };

  // ---- 外观：披风 ----

  const openCapes = async () => {
    if (!selected) return;
    setCapeOpen(true);
    setCapeLoading(true);
    setCapeError("");
    setCapeList([]);
    try {
      const profile = await GetMinecraftProfile(selected.stableKey);

      setCapeList(profile?.capes ?? []);
    } catch (ex) {
      setCapeError(String((ex as Error)?.message ?? ex));
    } finally {
      setCapeLoading(false);
    }
  };

  const applyCape = async (cape: CapeTexture, index = 0) => {
    if (!selected || capeBusy) return;
    setCapeBusy(true);
    setCapeError("");
    try {
      await SetActiveCape(selected.stableKey, cape.id);
      setCapeOpen(false);
      setStatus(
        t("已激活披风：{0}", {
          "0": cape.alias?.trim() || `披风 ${index + 1}`,
        }),
      );
    } catch (ex) {
      setCapeError(String((ex as Error)?.message ?? ex));
    } finally {
      setCapeBusy(false);
    }
  };

  const disableCape = async () => {
    if (!selected || capeBusy) return;
    setCapeBusy(true);
    setCapeError("");
    try {
      await SetActiveCape(selected.stableKey, "");
      setCapeOpen(false);
      setStatus(t("已停用披风。"));
    } catch (ex) {
      setCapeError(String((ex as Error)?.message ?? ex));
    } finally {
      setCapeBusy(false);
    }
  };

  // ---- 渲染 ----

  return (
    <div className="nya-scroll h-full w-full overflow-y-auto">
      <div className="mx-auto flex max-w-5xl flex-col gap-4 px-6 py-5">
        {/* 标题区 */}
        <div className="flex flex-none items-center gap-3">
          <div className="flex min-w-0 flex-1 flex-col gap-0.5">
            <h1 className="overflow-hidden text-xl font-bold tracking-tight text-ellipsis whitespace-nowrap">
              {t("账户管理")}
            </h1>
            <span className="truncate text-[11px] text-gray-400">
              {status ||
                (rows.length > 0
                  ? t("已保存 {0} 个账号", { "0": rows.length })
                  : "")}
            </span>
          </div>
          <Button
            color="primary"
            radius="full"
            size="sm"
            startContent={<PersonAdd20Regular />}
            onPress={openAddModal}
          >
            {t("添加账号")}
          </Button>
        </div>

        {/* 账号列表（左）与玩家外观（右）：无卡两栏，靠留白和细线立住 */}
        <div className="flex flex-1 flex-col gap-5 md:flex-row md:gap-6">
          {/* 左列：账号列表（不铺面板，行只保留一条选中带） */}
          <div className="flex min-w-0 flex-1 flex-col">
            <div className="px-3 pt-1 pb-1.5 text-[11px] font-semibold tracking-wider text-gray-400 uppercase">
              {t("账号列表")}
            </div>
            {rows.length === 0 ? (
              <p className="px-3 py-2 text-xs text-gray-400">{t("暂无账号")}</p>
            ) : (
              <div className="flex flex-col gap-1">
                <AnimatePresence initial={false}>
                  {rows.map((row, idx) => {
                    const active = selectedKey === row.stableKey;

                    return (
                      <motion.div
                        key={row.stableKey}
                        layout
                        animate="center"
                        className="overflow-hidden"
                        exit="exit"
                        initial="enter"
                        variants={listItemVariants}
                      >
                        <div
                          className={`group relative flex cursor-pointer items-center gap-3 overflow-hidden rounded-lg px-3 py-2.5 transition-colors ${
                            active
                              ? "bg-primary/10 dark:bg-primary/15"
                              : "hover:bg-default-100/70 dark:hover:bg-white/5"
                          }`}
                          role="button"
                          tabIndex={0}
                          onClick={() => setSelectedKey(row.stableKey)}
                          onKeyDown={(event) => {
                            if (event.key === "Enter" || event.key === " ") {
                              event.preventDefault();
                              setSelectedKey(row.stableKey);
                            }
                          }}
                        >
                          {/* 无卡列表的选中态：一条左侧强调线，不靠边框与阴影抬升 */}
                          {active ? (
                            <div className="absolute inset-y-0 left-0 w-[3px] bg-primary" />
                          ) : null}

                          {/* 头像：皮肤双层（基础+帽子）8×8 头部，失败回退首字母 */}
                          <div className="flex size-11 flex-shrink-0 items-center justify-center overflow-hidden rounded-lg bg-default-200 dark:bg-gray-800">
                            {row.avatar ? (
                              <img
                                alt=""
                                className="w-full h-full object-contain [image-rendering:pixelated]"
                                src={row.avatar}
                              />
                            ) : (
                              <span className="text-sm font-bold text-gray-500 dark:text-gray-300">
                                {(row.account.DisplayName || "?")
                                  .trim()[0]
                                  ?.toUpperCase() || "?"}
                              </span>
                            )}
                          </div>
                          <div className="flex min-w-0 flex-1 flex-col gap-0.5">
                            <div className="flex min-w-0 items-center gap-1.5">
                              <span className="overflow-hidden text-sm font-semibold text-ellipsis whitespace-nowrap">
                                {row.account.DisplayName}
                              </span>
                              {/* 快捷复制用户名 */}
                              <Tooltip
                                content={
                                  copiedKey === row.stableKey
                                    ? t("已复制用户名")
                                    : t("复制用户名")
                                }
                                delay={300}
                              >
                                <button
                                  className="flex size-6 flex-none items-center justify-center rounded-md text-gray-400 opacity-0 group-hover:opacity-100 hover:bg-default-200 hover:text-gray-700 dark:hover:text-gray-200 transition-all cursor-pointer"
                                  type="button"
                                  onClick={(e) => copyAccountName(e, row)}
                                >
                                  {copiedKey === row.stableKey ? (
                                    <Checkmark20Regular className="w-3.5 h-3.5 text-success" />
                                  ) : (
                                    <Copy20Regular className="w-3.5 h-3.5" />
                                  )}
                                </button>
                              </Tooltip>
                              <span
                                className={`flex-none rounded-full px-2 py-0.5 text-[10px] font-medium ${typeChipClass(row.account.Type)}`}
                              >
                                {typeLabel(row.account)}
                              </span>
                              {idx === 0 ? (
                                <span className="flex flex-none items-center gap-0.5 rounded-full bg-warning-500/15 px-2 py-0.5 text-[10px] font-medium text-warning-600 dark:text-warning-400">
                                  <Star20Regular className="w-3 h-3" />

                                  {t("默认")}
                                </span>
                              ) : null}
                            </div>
                            <span className="overflow-hidden text-[11px] text-gray-400 text-ellipsis whitespace-nowrap">
                              {accountDetail(row.account)}
                            </span>
                          </div>
                          {idx !== 0 ? (
                            <Button
                              className="flex-none"
                              radius="full"
                              size="sm"
                              variant="flat"
                              onClick={(e) => e.stopPropagation()}
                              onPress={() => void setDefault(row)}
                            >
                              {t("设为默认")}
                            </Button>
                          ) : null}
                          <Button
                            className="flex-none"
                            color="danger"
                            radius="full"
                            size="sm"
                            startContent={
                              confirmingDeleteKey === row.stableKey ? (
                                <Delete20Regular />
                              ) : undefined
                            }
                            variant={
                              confirmingDeleteKey === row.stableKey
                                ? "solid"
                                : "light"
                            }
                            onClick={(e) => e.stopPropagation()}
                            onPress={() => void removeAccount(row)}
                          >
                            {confirmingDeleteKey === row.stableKey
                              ? t("确认删除？")
                              : t("删除")}
                          </Button>
                        </div>
                      </motion.div>
                    );
                  })}
                </AnimatePresence>
              </div>
            )}
          </div>

          {/* 右列：皮肤展示 + 玩家外观（无卡：只与左列表隔一条细线） */}
          <aside className="nya-border flex min-w-0 flex-col gap-4 md:w-[320px] md:flex-none md:border-l md:pl-6">
            {/* 当前账号：一行，皮肤展示与玩家外观共用，不再各写一遍 */}
            {selected ? (
              <div className="flex min-w-0 flex-none items-center gap-2">
                <span className="overflow-hidden text-sm font-semibold text-ellipsis whitespace-nowrap">
                  {selected.account.DisplayName}
                </span>
                <span
                  className={`flex-none rounded-full px-2 py-0.5 text-[10px] font-medium ${typeChipClass(
                    selected.account.Type,
                  )}`}
                >
                  {selected.account.Type === "microsoft"
                    ? t("正版凭据已就绪")
                    : selected.account.Type === "authlib"
                      ? t("外置验证档案")
                      : t("离线本地档案")}
                </span>
              </div>
            ) : !simpleMode ? null : (
              /* 非 S 模式下 3D 预览自己会写一行"未选择账号"，这里不再重复 */
              <p className="flex-none text-xs text-gray-400">
                {t("未选择账号")}
              </p>
            )}

            {/* 皮肤展示：3D 预览当前选中账号（与主页皮肤展示小组件同源渲染）。
                S 模式只隐藏预览本身，玩家外观的操作仍然保留 */}
            {!simpleMode ? (
              <section className="flex flex-none flex-col gap-2">
                <div className="text-[11px] font-semibold tracking-wider text-gray-400 uppercase">
                  {t("皮肤展示")}
                </div>
                <SkinPreviewPanel
                  accountKey={selected?.stableKey ?? ""}
                  height={260}
                />
              </section>
            ) : null}

            <section
              className={`flex flex-col gap-2 ${
                !simpleMode ? "nya-border border-t pt-4" : ""
              }`}
            >
              <div className="text-[11px] font-semibold tracking-wider text-gray-400 uppercase">
                {t("玩家外观")}
              </div>
              <div className="flex flex-wrap items-center gap-2">
                <Tooltip
                  content={
                    !selected
                      ? t("请先选择账号")
                      : selected.account.Type !== "microsoft"
                        ? t("仅微软正版账号支持上传到官方皮肤服务器")
                        : t("更换正版 Minecraft 官方皮肤")
                  }
                  delay={300}
                >
                  <div>
                    <Button
                      isDisabled={
                        !selected ||
                        busy ||
                        selected.account.Type !== "microsoft"
                      }
                      radius="full"
                      size="sm"
                      startContent={<ArrowUpload20Regular />}
                      variant="flat"
                      onPress={changeMicrosoftSkin}
                    >
                      {t("上传正版皮肤")}
                    </Button>
                  </div>
                </Tooltip>

                <Tooltip
                  content={
                    !selected
                      ? t("请先选择账号")
                      : selected.account.Type !== "offline"
                        ? t("仅离线账号支持直接加载本地 PNG 贴图文件")
                        : t("从本地电脑选择 .png 皮肤文件")
                  }
                  delay={300}
                >
                  <div>
                    <Button
                      isDisabled={
                        !selected || busy || selected.account.Type !== "offline"
                      }
                      radius="full"
                      size="sm"
                      startContent={<FolderOpen20Regular />}
                      variant="flat"
                      onPress={changeOfflineSkinFile}
                    >
                      {t("离线自定义皮肤")}
                    </Button>
                  </div>
                </Tooltip>

                <Tooltip
                  content={
                    !selected
                      ? t("请先选择账号")
                      : selected.account.Type !== "offline"
                        ? t("仅离线账号支持应用内置预设皮肤")
                        : t("从经典/纤细等内置默认皮肤库中挑选")
                  }
                  delay={300}
                >
                  <div>
                    <Button
                      isDisabled={
                        !selected || busy || selected.account.Type !== "offline"
                      }
                      radius="full"
                      size="sm"
                      startContent={<PaintBrush20Regular />}
                      variant="flat"
                      onPress={openCatalog}
                    >
                      {t("从皮肤库选择")}
                    </Button>
                  </div>
                </Tooltip>

                <Tooltip
                  content={
                    !selected
                      ? t("请先选择账号")
                      : selected.account.Type !== "microsoft"
                        ? t("仅微软正版账号支持装备或停用官方披风")
                        : t("查看与更换账号拥有的官方披风")
                  }
                  delay={300}
                >
                  <div>
                    <Button
                      color="primary"
                      isDisabled={
                        !selected ||
                        busy ||
                        selected.account.Type !== "microsoft"
                      }
                      radius="full"
                      size="sm"
                      startContent={<Shirt20Regular />}
                      variant="flat"
                      onPress={openCapes}
                    >
                      {t("更换披风")}
                    </Button>
                  </div>
                </Tooltip>
              </div>
              {selected?.account.Type === "authlib" ? (
                <div className="flex items-center gap-1.5 text-xs text-secondary-600 dark:text-secondary-400">
                  <Info20Regular className="w-4 h-4 flex-none" />
                  <span>
                    {t(
                      "皮肤站账号（Authlib-Injector）请直接登录对应皮肤站网页端进行皮肤与披风管理，启动器会自动同步。",
                    )}
                  </span>
                </div>
              ) : null}
            </section>
          </aside>
        </div>
      </div>

      {/* 添加账号弹层（离线 / 微软设备码 / 皮肤站） */}
      {/* isDismissable=false（modalBehaviorProps）：点外部不关闭，防止误触——关闭一律走右上角 X */}
      <Modal
        isOpen={addOpen}
        size="md"
        onClose={() => {
          if (!msBusy) setAddOpen(false);
        }}
        {...modalBehaviorProps}
      >
        <ModalContent>
          <ModalShell
            closeGuard={() => !msBusy}
            title={t("添加账号")}
            onClose={() => {
              if (!msBusy) setAddOpen(false);
            }}
          >
            {/* 方式选择：胶囊分段控件（与下载页标签栏同风格，主色滑块切换） */}
            <SegmentedTabs
              className="flex items-center gap-1 rounded-full bg-default-100/80 p-1"
              disabled={msBusy}
              itemClassName="flex-1 px-2 py-1.5 text-[13px]"
              items={[
                {
                  key: "offline",
                  label: (
                    <>
                      <Person20Regular />

                      {t("离线")}
                    </>
                  ),
                },
                {
                  key: "microsoft",
                  label: (
                    <>
                      <Key20Regular />

                      {t("正版登录")}
                    </>
                  ),
                },
                {
                  key: "authlib",
                  label: (
                    <>
                      <Globe20Regular />

                      {t("皮肤站")}
                    </>
                  ),
                },
              ]}
              layoutId="account-add-method"
              value={addTab}
              onChange={(key) => {
                if (!msBusy) setAddTab(key);
              }}
            />

            <SwitchTransition
              activeKey={addTab}
              className="flex flex-col"
              direction={addTabDirection}
            >
              {/* 离线 */}
              {addTab === "offline" && (
                <div className="flex flex-col gap-3">
                  <div>
                    <div className="mb-1 text-[13px] text-gray-600 dark:text-gray-300">
                      {t("离线用户名")}
                    </div>
                    <Input
                      isClearable
                      classNames={{
                        inputWrapper:
                          "bg-default-100/80 data-[hover=true]:bg-default-200",
                      }}
                      placeholder={t("例如 Player_01")}
                      radius="lg"
                      size="sm"
                      value={offlineName}
                      onKeyDown={(e) => {
                        if (e.key === "Enter") {
                          e.preventDefault();
                          void addOffline();
                        }
                      }}
                      onValueChange={setOfflineName}
                    />
                  </div>
                  <Button
                    color="primary"
                    radius="full"
                    size="sm"
                    onPress={addOffline}
                  >
                    {t("创建离线账号")}
                  </Button>
                </div>
              )}

              {/* 正版登录 */}
              {addTab === "microsoft" && (
                <div className="flex flex-col gap-3">
                  {msBusy ? (
                    <>
                      <div className="text-sm text-gray-700 dark:text-gray-300">
                        {deviceCode
                          ? t("在浏览器打开 microsoft.com/link 并输入验证码：")
                          : t("正在请求设备码…")}
                      </div>
                      {deviceCode ? (
                        <>
                          <button
                            className="group relative block w-full cursor-pointer rounded-lg bg-primary py-5 text-center text-primary-foreground shadow-md shadow-primary/25 transition-transform hover:scale-[1.01] active:scale-[0.99]"
                            title={t("点击复制设备码")}
                            type="button"
                            onClick={async () => {
                              try {
                                await ClipboardSetText(deviceCode);
                                setCodeCopied(true);
                                setTimeout(() => setCodeCopied(false), 2000);
                              } catch {
                                /* ignore */
                              }
                            }}
                          >
                            <span className="block text-2xl font-bold tracking-[0.3em] text-white">
                              {deviceCode}
                            </span>
                            <span className="text-[11px] text-white/80 transition-opacity">
                              {codeCopied
                                ? t("✓ 已复制到剪贴板！")
                                : t("点击此处快速复制")}
                            </span>
                          </button>
                          <div className="flex justify-center gap-2">
                            <Button
                              radius="full"
                              size="sm"
                              variant="flat"
                              onPress={() =>
                                BrowserOpenURL(
                                  `https://www.microsoft.com/link?user_code=${deviceCode}`,
                                )
                              }
                            >
                              {t("重新打开浏览器")}
                            </Button>
                            <Button
                              color="danger"
                              radius="full"
                              size="sm"
                              variant="flat"
                              onPress={cancelMicrosoftLogin}
                            >
                              {t("取消登录")}
                            </Button>
                          </div>
                        </>
                      ) : null}
                    </>
                  ) : (
                    <>
                      <Button
                        color="primary"
                        isDisabled={browserStarting}
                        isLoading={browserStarting}
                        radius="full"
                        size="sm"
                        startContent={<Globe20Regular />}
                        onPress={startMicrosoftBrowserLogin}
                      >
                        {t("启动器内登录（推荐）")}
                      </Button>
                      <div className="text-xs text-gray-400">
                        {t(
                          "在当前窗口打开微软登录页，授权后自动返回，无需外部浏览器。",
                        )}
                      </div>
                      <Button
                        radius="full"
                        size="sm"
                        variant="flat"
                        onPress={startMicrosoftLogin}
                      >
                        {t("使用设备码登录")}
                      </Button>
                    </>
                  )}
                </div>
              )}

              {/* 皮肤站 */}
              {addTab === "authlib" && (
                <div className="flex flex-col gap-3">
                  <div>
                    <div className="mb-1 flex items-center justify-between text-[13px] text-gray-600 dark:text-gray-300">
                      <span>{t("皮肤站地址")}</span>
                      {/* 常用皮肤站快捷预设 */}
                      <div className="flex items-center gap-1.5 text-[11px]">
                        <span className="text-gray-400">{t("快捷选择:")}</span>
                        {PRESET_AUTHLIB_SERVERS.map((preset) => (
                          <button
                            key={preset.url}
                            className="rounded px-1.5 py-0.5 text-primary hover:bg-primary/10 transition-colors cursor-pointer"
                            type="button"
                            onClick={() => setExtServer(preset.url)}
                          >
                            {preset.name}
                          </button>
                        ))}
                      </div>
                    </div>
                    <Input
                      classNames={{
                        inputWrapper:
                          "bg-default-100/80 data-[hover=true]:bg-default-200",
                      }}
                      placeholder="https://littleskin.cn"
                      radius="lg"
                      size="sm"
                      value={extServer}
                      onKeyDown={(e) => {
                        if (e.key === "Enter") {
                          e.preventDefault();
                          void loginExternal();
                        }
                      }}
                      onValueChange={setExtServer}
                    />
                  </div>
                  <div className="grid grid-cols-2 gap-3">
                    <div>
                      <div className="mb-1 text-[13px] text-gray-600 dark:text-gray-300">
                        {t("用户名")}
                      </div>
                      <Input
                        classNames={{
                          inputWrapper:
                            "bg-default-100/80 data-[hover=true]:bg-default-200",
                        }}
                        radius="lg"
                        size="sm"
                        value={extUsername}
                        onKeyDown={(e) => {
                          if (e.key === "Enter") {
                            e.preventDefault();
                            void loginExternal();
                          }
                        }}
                        onValueChange={setExtUsername}
                      />
                    </div>
                    <div>
                      <div className="mb-1 text-[13px] text-gray-600 dark:text-gray-300">
                        {t("密码")}
                      </div>
                      <Input
                        classNames={{
                          inputWrapper:
                            "bg-default-100/80 data-[hover=true]:bg-default-200",
                        }}
                        radius="lg"
                        size="sm"
                        type="password"
                        value={extPassword}
                        onKeyDown={(e) => {
                          if (e.key === "Enter") {
                            e.preventDefault();
                            void loginExternal();
                          }
                        }}
                        onValueChange={setExtPassword}
                      />
                    </div>
                  </div>
                  {extProfiles.length > 0 ? (
                    <Select
                      defaultSelectedKeys={["0"]}
                      label={t("选择角色")}
                      popoverProps={selectPopoverProps}
                      radius="lg"
                      size="sm"
                      onSelectionChange={(keys) => {
                        const idx = Number(Array.from(keys)[0] ?? "-1");
                        const profile = extProfiles[idx];

                        if (profile)
                          void addExternal(profile, extUsername.trim());
                      }}
                    >
                      {extProfiles.map((p, i) => (
                        <SelectItem key={String(i)}>{p.Name}</SelectItem>
                      ))}
                    </Select>
                  ) : null}
                  <Button
                    color="primary"
                    isLoading={extBusy}
                    radius="full"
                    size="sm"
                    onPress={loginExternal}
                  >
                    {t("登录皮肤站")}
                  </Button>
                </div>
              )}
            </SwitchTransition>

            {addHint ? (
              <div className="rounded-lg bg-default-100/80 px-3 py-2 text-xs text-gray-500 dark:text-gray-400 break-all">
                {addHint}
              </div>
            ) : null}
          </ModalShell>
        </ModalContent>
      </Modal>

      {/* 披风选择弹层（正版账号）——同样走 modalBehaviorProps（毛玻璃遮罩 +
          CSS 入场，见 modal-shell.tsx 说明） */}
      <Modal
        isOpen={capeOpen}
        size="md"
        onClose={() => {
          if (!capeBusy) setCapeOpen(false);
        }}
        {...modalBehaviorProps}
      >
        <ModalContent>
          <ModalShell
            closeGuard={() => !capeBusy}
            title={t("更换披风")}
            onClose={() => {
              if (!capeBusy) setCapeOpen(false);
            }}
          >
            {capeLoading ? (
              <div className="py-8 text-center text-sm text-gray-400">
                {t("正在读取披风…")}
              </div>
            ) : capeList.length === 0 ? (
              <div className="flex flex-col items-center gap-3 py-8 text-center text-gray-400">
                <div className="flex size-16 items-center justify-center rounded-lg bg-gradient-to-br from-default-200 to-default-100 dark:from-gray-800 dark:to-gray-800/50 shadow-inner">
                  <Shirt20Regular className="w-8 h-8" />
                </div>
                <div className="text-sm text-gray-500 dark:text-gray-400">
                  {capeError || t("该账号暂未拥有披风")}
                </div>
              </div>
            ) : (
              <>
                <div className="nya-scroll grid grid-cols-3 gap-2.5 max-h-[46vh] overflow-y-auto">
                  {capeList.map((cape, index) => (
                    <button
                      key={cape.id}
                      className={`group relative flex flex-col items-center gap-2 rounded-large border border-transparent nya-panel p-3 backdrop-blur-md transition-all hover:border-primary/30 hover:bg-primary/[0.06] disabled:cursor-default ${
                        cape.isActive
                          ? "ring-2 ring-primary/60 bg-primary/10 border-primary/40 shadow-sm"
                          : ""
                      }`}
                      disabled={capeBusy || cape.isActive}
                      onClick={() => applyCape(cape, index)}
                    >
                      {/* 激活标记徽章 */}
                      {cape.isActive ? (
                        <div className="absolute top-2 right-2 flex size-5 items-center justify-center rounded-full bg-primary text-white shadow-sm">
                          <Checkmark20Regular className="w-3.5 h-3.5" />
                        </div>
                      ) : null}
                      {/* 披风预览：裁剪贴图正面区域 (1,0)-(11,16)，×4 放大（pixelated） */}
                      <div className="flex w-16 h-16 items-center justify-center overflow-hidden rounded-lg bg-default-100 dark:bg-gray-800 transition-transform group-hover:scale-105">
                        <div
                          className="w-10 h-16"
                          style={{
                            backgroundImage: `url("${cape.url}")`,
                            backgroundSize: "256px 128px",
                            backgroundPosition: "-4px 0px",
                            imageRendering: "pixelated",
                          }}
                        />
                      </div>
                      <span className="max-w-full truncate text-xs font-medium text-gray-700 dark:text-gray-300">
                        {cape.alias?.trim() ||
                          t("披风 {0}", { "0": index + 1 })}
                        {cape.isActive ? (
                          <span className="ml-1 text-[10px] text-primary font-semibold">
                            ({t("已装备")})
                          </span>
                        ) : null}
                      </span>
                    </button>
                  ))}
                </div>
                {capeError ? (
                  <div className="text-xs text-danger">{capeError}</div>
                ) : null}
                <div className="flex items-center justify-end gap-3 pt-1">
                  <Button
                    color="danger"
                    isDisabled={
                      capeBusy || !capeList.some((cape) => cape.isActive)
                    }
                    radius="full"
                    size="sm"
                    variant="flat"
                    onPress={disableCape}
                  >
                    {t("停用披风")}
                  </Button>
                </div>
              </>
            )}
          </ModalShell>
        </ModalContent>
      </Modal>

      {/* 离线皮肤库弹层（内置皮肤网格） */}
      <Modal
        isOpen={catalogOpen}
        size="md"
        onClose={() => setCatalogOpen(false)}
        {...modalBehaviorProps}
      >
        <ModalContent>
          <ModalShell
            title={t("选择离线默认皮肤")}
            onClose={() => setCatalogOpen(false)}
          >
            <div className="nya-scroll grid grid-cols-3 gap-2.5 max-h-[56vh] overflow-y-auto">
              {catalog.map((choice) => {
                const active =
                  choice.id.toLowerCase() ===
                  (selected?.account.OfflineSkinId || "steve").toLowerCase();

                return (
                  <button
                    key={choice.id}
                    className={`group relative flex flex-col items-center gap-2 rounded-large border border-transparent nya-panel p-3 backdrop-blur-md transition-all hover:border-primary/30 hover:bg-primary/[0.06] ${
                      active
                        ? "ring-2 ring-primary/60 bg-primary/10 border-primary/40 shadow-sm"
                        : ""
                    }`}
                    onClick={() => applyCatalogSkin(choice)}
                  >
                    {/* 激活标记徽章 */}
                    {active ? (
                      <div className="absolute top-2 right-2 flex size-5 items-center justify-center rounded-full bg-primary text-white shadow-sm">
                        <Checkmark20Regular className="w-3.5 h-3.5" />
                      </div>
                    ) : null}
                    <div className="flex w-16 h-16 items-center justify-center overflow-hidden rounded-lg bg-default-100 dark:bg-gray-800 transition-transform group-hover:scale-105">
                      {choice.source ? (
                        <img
                          alt=""
                          className="w-full h-full object-contain [image-rendering:pixelated]"
                          src={choice.source}
                        />
                      ) : (
                        <span className="text-xl font-bold text-gray-500 dark:text-gray-300">
                          {choice.fallbackText}
                        </span>
                      )}
                    </div>
                    <span className="text-xs font-medium text-gray-700 dark:text-gray-300">
                      {choice.displayName}
                      {active ? (
                        <span className="ml-1 text-[10px] text-primary font-semibold">
                          ({t("当前使用")})
                        </span>
                      ) : null}
                    </span>
                    <span className="text-[10px] text-gray-400">
                      {choice.model === "slim"
                        ? t("纤细 · Slim")
                        : t("经典 · Classic")}
                    </span>
                  </button>
                );
              })}
            </div>
          </ModalShell>
        </ModalContent>
      </Modal>

      {/* 正版皮肤模型选择弹层（WebView2 不支持 confirm，改用弹层） */}
      <Modal
        isOpen={modelOpen}
        size="sm"
        onClose={() => {
          if (!modelBusy) setModelOpen(false);
        }}
        {...modalBehaviorProps}
      >
        <ModalContent>
          <ModalShell
            closeGuard={() => !modelBusy}
            title={t("选择皮肤模型")}
            onClose={() => {
              if (!modelBusy) setModelOpen(false);
            }}
          >
            <div className="flex gap-3">
              <Button
                className="flex-1"
                isDisabled={modelBusy}
                radius="full"
                variant="flat"
                onPress={() => uploadSkinWithModel("classic")}
              >
                {t("经典 · Steve")}
              </Button>
              <Button
                className="flex-1"
                color="primary"
                isDisabled={modelBusy}
                radius="full"
                variant="flat"
                onPress={() => uploadSkinWithModel("slim")}
              >
                {t("纤细 · Alex")}
              </Button>
            </div>
          </ModalShell>
        </ModalContent>
      </Modal>
    </div>
  );
};

export default AccountPage;
