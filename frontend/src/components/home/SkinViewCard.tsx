/*
 * 玩家 3D 皮肤展示卡片：用 skinview3d 渲染当前选中账号的皮肤。
 * 渲染核心在 SkinPreviewPanel（与账户页共用），这里只负责卡片外壳、
 * 账号切换下拉与账号名展示。
 */
import type { SelectedAccountSummary, WidgetRenderContext } from "../../plugin";

import React from "react";
import {
  ChevronDown20Regular,
  PeopleSwap20Regular,
  Person20Regular,
} from "@fluentui/react-icons";
import {
  Button,
  Dropdown,
  DropdownItem,
  DropdownMenu,
  DropdownTrigger,
} from "@heroui/react";

import { accountTypeLabel } from "../../lib/home";
import { dropdownMotionProps } from "../../lib/motion";
import SkinPreviewPanel from "../account/SkinPreviewPanel";
import { t } from "../../i18n";

import HomeCard from "./HomeCard";

/** 账号头像：皮肤头像加载失败时回落首字母占位 */
const AccountAvatar: React.FC<{
  account: SelectedAccountSummary;
  size: string;
}> = ({ account, size }) =>
  account.avatar ? (
    <img
      alt=""
      className={`${size} flex-none object-contain [image-rendering:pixelated]`}
      draggable={false}
      src={account.avatar}
    />
  ) : (
    <span
      className={`${size} flex flex-none items-center justify-center rounded-md bg-default-200 text-[10px] font-bold text-gray-500 dark:bg-default-100 dark:text-gray-300`}
    >
      {(account.name || "?").trim()[0]?.toUpperCase() || "?"}
    </span>
  );

const SkinViewCard: React.FC<{ context: WidgetRenderContext }> = ({
  context,
}) => {
  const account = context.selectedAccount;
  const accounts = context.accounts;
  const onSelectAccount = context.onSelectAccount;

  return (
    <HomeCard
      action={
        accounts.length >= 2 ? (
          <Dropdown motionProps={dropdownMotionProps} placement="bottom-end">
            <DropdownTrigger>
              <Button
                className="h-7 min-w-0 flex-none gap-1 px-2.5 text-[11px]"
                endContent={<ChevronDown20Regular />}
                radius="full"
                size="sm"
                startContent={<PeopleSwap20Regular />}
                title={t("切换展示的账号")}
                variant="flat"
              >
                {t("切换账号")}
              </Button>
            </DropdownTrigger>
            <DropdownMenu
              disallowEmptySelection
              aria-label={t("切换账号")}
              className="max-h-72 overflow-y-auto"
              selectedKeys={account ? [account.key] : []}
              selectionMode="single"
              onSelectionChange={(keys) => {
                const key = [...keys][0];

                if (key !== undefined) onSelectAccount(String(key));
              }}
            >
              {accounts.map((row) => (
                <DropdownItem
                  key={row.key}
                  textValue={row.name || t("未命名玩家")}
                >
                  <div className="flex items-center gap-2 py-0.5">
                    <AccountAvatar account={row} size="size-6" />
                    <span className="min-w-0 flex-1 truncate text-sm">
                      {row.name || t("未命名玩家")}
                    </span>
                    <span className="flex-none text-[10px] text-gray-400">
                      {accountTypeLabel(row.type)}
                    </span>
                  </div>
                </DropdownItem>
              ))}
            </DropdownMenu>
          </Dropdown>
        ) : null
      }
      icon={<Person20Regular />}
      label={t("皮肤展示")}
      value={account ? account.name || t("未命名玩家") : t("暂无账号")}
    >
      <SkinPreviewPanel accountKey={account?.key ?? ""} />
    </HomeCard>
  );
};

export default SkinViewCard;
