/*
 * Copyright 2026 烟花
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

/**
 * 首次启动引导覆盖层（OOBE），共三种形态：
 *
 * 1. 欢迎页：整页三选一（我是萌新 / 跳过引导 / 我是创作者）；
 * 2. 完整流程卡：单步展示当前步骤，可前进 / 后退 / 跳转；
 * 3. 跟随小窗：点了"前往某页面"后，引导收成右下角悬浮小卡继续提示
 *    当前步骤——切页不丢引导，操作完点"下一步"继续，也可随时展开
 *    回完整卡片或结束引导。
 *
 * 完成一律走 completeOobe 统一持久化（写 oobeCompleted 标记）。
 */
import React, { useState } from "react";
import { Button } from "@heroui/react";
import {
  ArrowMaximize20Regular,
  ArrowRight20Regular,
  Checkmark20Regular,
  ChevronLeft20Regular,
  Dismiss20Regular,
  Person20Regular,
  Wand20Regular,
} from "@fluentui/react-icons";

import { t } from "../../i18n";

import { useOobe, type OobeFlow } from "./OobeProvider";

/** 流程内一步：说明文案 + 可选的跳转动作 */
interface OobeStep {
  title: string;
  description: string;
  /** 按钮文案；缺省则该步没有跳转，只有"下一步" */
  actionLabel?: string;
  /** 跳转目标（导航总线的页面 id + 可选二级定位） */
  actionPage?: string;
  actionDetail?: string;
}

/** 萌新流程：把"能自动的都自动"，只带用户做必须的决策 */
const NEWBIE_STEPS: OobeStep[] = [
  {
    title: t("第一步：准备一个账户"),
    description: t(
      "玩 Minecraft 需要一个账户。正版玩家可以登录微软账户；也可以先创建一个离线账户，随时都能在「账户」页修改。",
    ),
    actionLabel: t("前往账户页登录"),
    actionPage: "account",
  },
  {
    title: t("第二步：Java 与内存（已自动检测）"),
    description: t(
      "启动器已自动扫描系统中的 Java 并按电脑内存给出推荐分配，通常无需改动。如果启动报错，可在「设置 → Java」中手动添加或下载。",
    ),
    actionLabel: t("查看 Java 设置"),
    actionPage: "settings",
    actionDetail: "java",
  },
  {
    title: t("第三步：下载你的第一个版本"),
    description: t(
      "打开「下载」页选择一个 Minecraft 版本（新手推荐最新正式版），点击下载后即可在主页一键启动。",
    ),
    actionLabel: t("前往下载页"),
    actionPage: "download",
  },
  {
    title: t("第四步：把主页变成你的启动台"),
    description: t(
      "主页由一个个小组件组成（快速下载、启动日志、日历运势等）。点主页右下角的圆形 + 按钮打开组件盒，把喜欢的组件拖到页面上或直接点击添加；长按组件 1 秒可以拖动排序，拖回组件盒即移除。现在就去试一试吧！",
    ),
    actionLabel: t("前往主页试一试"),
    actionPage: "home",
  },
  {
    title: t("小贴士：界面太热闹？"),
    description: t(
      "如果觉得功能太多，可以在「外观」页切换简洁布局、调整背景与主题色；「设置」里的每一项都有说明，随时可以改回来。",
    ),
    actionLabel: t("看看外观设置"),
    actionPage: "appearance",
  },
  {
    title: t("准备完毕！"),
    description: t(
      "账户就绪、版本下载完成后，回到主页点击启动就能进入游戏。遇到问题随时到「帮助」页查找解决方案。",
    ),
  },
];

/** 创作者流程：直接介绍工具箱入口 */
const CREATOR_STEPS: OobeStep[] = [
  {
    title: t("欢迎，创作者！"),
    description: t(
      "「创作中心」内置了一套创作工具箱：资源包可视化编辑、命令生成器、渐变文字、3D 立体字、皮肤编辑与整合包导出，无需切换外部工具。",
    ),
    actionLabel: t("前往创作中心"),
    actionPage: "creator",
  },
  {
    title: t("小贴士"),
    description: t(
      "资源包编辑器支持实时预览与直接编辑 textures / 模型 / 语言文件；做好的内容可以随时导出分享。外观页还可以把启动器本身调成你喜欢的样子。",
    ),
    actionLabel: t("看看外观设置"),
    actionPage: "appearance",
  },
  {
    title: t("一切就绪"),
    description: t("创作中心随时可以从侧边栏进入。祝创作顺利！"),
  },
];

const FLOW_STEPS: Record<OobeFlow, OobeStep[]> = {
  newbie: NEWBIE_STEPS,
  creator: CREATOR_STEPS,
};

/** 欢迎页的身份卡片数据 */
const WELCOME_CHOICES: Array<{
  flow: OobeFlow;
  icon: React.ReactNode;
  title: string;
  description: string;
}> = [
  {
    flow: "newbie",
    icon: <Person20Regular />,
    title: t("我是萌新"),
    description: t("跟着引导登录账户、下载版本，几分钟完成第一次启动。"),
  },
  {
    flow: "creator",
    icon: <Wand20Regular />,
    title: t("我是创作者"),
    description: t("了解创作中心的资源包编辑、命令生成等工具，直达工位。"),
  },
];

const OobeOverlay: React.FC = () => {
  const {
    oobeNeeded,
    docked,
    flow,
    beginFlow,
    backToWelcome,
    followToPage,
    expandGuide,
    completeOobe,
  } = useOobe();
  const [stepIndex, setStepIndex] = useState(0);

  if (!oobeNeeded) return null;

  // ---- 跟随小窗：切页后悬浮在右下角，持续提示当前步骤 ----
  if (docked && flow !== null) {
    const dockSteps = FLOW_STEPS[flow];
    const dockStep = dockSteps[Math.min(stepIndex, dockSteps.length - 1)];
    const dockIsLast = stepIndex >= dockSteps.length - 1;
    const dockFlowTitle = flow === "newbie" ? t("新手引导") : t("创作者引导");

    return (
      <div className="fixed right-4 bottom-4 z-40 w-80">
        <div className="flex flex-col gap-3 rounded-lg border border-gray-200/60 bg-white/80 p-5 shadow-xl backdrop-blur-md dark:border-gray-700/60 dark:bg-gray-800/80">
          <div className="flex items-center justify-between">
            <span className="text-xs font-medium text-primary-500">
              {dockFlowTitle} · {stepIndex + 1}/{dockSteps.length}
            </span>
            <div className="flex items-center gap-1">
              <Button
                isIconOnly
                aria-label={t("展开引导")}
                radius="full"
                size="sm"
                variant="light"
                onPress={expandGuide}
              >
                <ArrowMaximize20Regular />
              </Button>
              <Button
                isIconOnly
                aria-label={t("跳过引导")}
                radius="full"
                size="sm"
                variant="light"
                onPress={completeOobe}
              >
                <Dismiss20Regular />
              </Button>
            </div>
          </div>

          <h3 className="text-sm font-semibold">{dockStep.title}</h3>
          <p className="line-clamp-4 text-xs leading-relaxed text-gray-500 dark:text-gray-400">
            {dockStep.description}
          </p>

          <div className="flex items-center gap-2">
            {dockStep.actionLabel && (
              <Button
                color="primary"
                size="sm"
                startContent={<ArrowRight20Regular />}
                variant="flat"
                onPress={() =>
                  followToPage(dockStep.actionPage!, dockStep.actionDetail)
                }
              >
                {dockStep.actionLabel}
              </Button>
            )}
            <Button
              color="primary"
              size="sm"
              startContent={<Checkmark20Regular />}
              variant={dockStep.actionLabel ? "light" : "flat"}
              onPress={() =>
                dockIsLast ? completeOobe() : setStepIndex((i) => i + 1)
              }
            >
              {dockIsLast ? t("完成") : t("下一步")}
            </Button>
            {stepIndex > 0 && (
              <Button
                isIconOnly
                aria-label={t("上一步")}
                radius="full"
                size="sm"
                variant="light"
                onPress={() => setStepIndex((i) => i - 1)}
              >
                <ChevronLeft20Regular />
              </Button>
            )}
          </div>

          <div className="flex gap-1.5">
            {dockSteps.map((_, i) => (
              <span
                key={i}
                className={`h-1.5 rounded-full transition-all ${
                  i === stepIndex
                    ? "w-4 bg-primary-500"
                    : "w-1.5 bg-gray-300 dark:bg-gray-600"
                }`}
              />
            ))}
          </div>
        </div>
      </div>
    );
  }

  // ---- 第一屏：整页三选一（左 → 右） ----
  if (flow === null) {
    return (
      <div
        className="fixed inset-0 top-10 z-40 flex items-center justify-center overflow-y-auto p-6"
        style={{ backgroundColor: "rgb(var(--nya-shell) / 0.92)" }}
      >
        <div className="flex w-full max-w-3xl flex-col items-center gap-8">
          <div className="flex flex-col items-center gap-2 text-center">
            <img
              alt="Neko"
              className="h-20 w-20 select-none"
              draggable={false}
              src="/mascot/neko.png"
            />
            <h1 className="text-2xl font-semibold">
              {t("欢迎来到 NekoLauncher！")}
            </h1>
            <p className="text-sm text-gray-500 dark:text-gray-400">
              {t("选择最适合你的开始方式，之后可随时在设置中修改。")}
            </p>
          </div>

          <div className="grid w-full grid-cols-1 gap-4 sm:grid-cols-3">
            {WELCOME_CHOICES.map((choice) => (
              <button
                key={choice.flow}
                className="group flex flex-col items-center gap-3 rounded-lg border border-gray-200/60 bg-white/60 p-6 text-center backdrop-blur-md transition-all hover:-translate-y-1 hover:shadow-lg dark:border-gray-700/60 dark:bg-gray-800/60"
                type="button"
                onClick={() => {
                  setStepIndex(0);
                  beginFlow(choice.flow);
                }}
              >
                <span className="flex h-12 w-12 items-center justify-center rounded-full bg-primary-100 text-primary-600 dark:bg-primary-900/40 dark:text-primary-300">
                  {choice.icon}
                </span>
                <span className="text-base font-semibold">{choice.title}</span>
                <span className="text-xs leading-relaxed text-gray-500 dark:text-gray-400">
                  {choice.description}
                </span>
              </button>
            ))}

            {/* 跳过引导：不进流程，直接写完成标记 */}
            <button
              className="group flex flex-col items-center gap-3 rounded-lg border border-gray-200/60 bg-white/60 p-6 text-center backdrop-blur-md transition-all hover:-translate-y-1 hover:shadow-lg dark:border-gray-700/60 dark:bg-gray-800/60 sm:col-start-3"
              type="button"
              onClick={completeOobe}
            >
              <span className="flex h-12 w-12 items-center justify-center rounded-full bg-gray-100 text-gray-500 dark:bg-gray-700 dark:text-gray-300">
                <Dismiss20Regular />
              </span>
              <span className="text-base font-semibold">{t("跳过引导")}</span>
              <span className="text-xs leading-relaxed text-gray-500 dark:text-gray-400">
                {t("?!滚木!?")}
              </span>
            </button>
          </div>
        </div>
      </div>
    );
  }

  // ---- 流程内：单步展示，可前进 / 后退 / 跳转 / 结束 ----
  const steps = FLOW_STEPS[flow];
  const step = steps[Math.min(stepIndex, steps.length - 1)];
  const isLast = stepIndex >= steps.length - 1;
  const flowTitle = flow === "newbie" ? t("新手引导") : t("创作者引导");

  const goAction = () => {
    if (!step.actionPage) return;
    // 收成跟随小窗再切页：引导悬浮在目标页右下角，操作完点"下一步"继续
    followToPage(step.actionPage, step.actionDetail);
  };

  return (
    <div
      className="fixed inset-0 top-10 z-40 flex items-center justify-center overflow-y-auto p-6"
      style={{ backgroundColor: "rgb(var(--nya-shell) / 0.92)" }}
    >
      <div className="flex w-full max-w-md flex-col gap-6 rounded-lg border border-gray-200/60 bg-white/70 p-8 backdrop-blur-md dark:border-gray-700/60 dark:bg-gray-800/70">
        <div className="flex items-center justify-between">
          <span className="text-xs font-medium text-primary-500">
            {flowTitle} · {stepIndex + 1}/{steps.length}
          </span>
          <Button
            isIconOnly
            aria-label={t("跳过引导")}
            radius="full"
            size="sm"
            variant="light"
            onPress={completeOobe}
          >
            <Dismiss20Regular />
          </Button>
        </div>

        <h2 className="text-xl font-semibold">{step.title}</h2>
        <p className="text-sm leading-relaxed text-gray-600 dark:text-gray-300">
          {step.description}
        </p>

        <div className="flex flex-wrap items-center gap-2">
          {stepIndex > 0 && (
            <Button
              size="sm"
              startContent={<ChevronLeft20Regular />}
              variant="light"
              onPress={() => setStepIndex((i) => i - 1)}
            >
              {t("上一步")}
            </Button>
          )}
          {step.actionLabel && (
            <Button
              color="primary"
              size="sm"
              startContent={<ArrowRight20Regular />}
              variant="flat"
              onPress={goAction}
            >
              {step.actionLabel}
            </Button>
          )}
          <Button
            color="primary"
            size="sm"
            startContent={<Checkmark20Regular />}
            variant={step.actionLabel ? "light" : "flat"}
            onPress={() =>
              isLast ? completeOobe() : setStepIndex((i) => i + 1)
            }
          >
            {isLast ? t("完成") : t("下一步")}
          </Button>
          {!isLast && (
            <Button
              className="ml-auto"
              size="sm"
              variant="light"
              onPress={completeOobe}
            >
              {t("稍后再说")}
            </Button>
          )}
        </div>

        <div className="flex justify-center gap-1.5">
          {steps.map((_, i) => (
            <span
              key={i}
              className={`h-1.5 rounded-full transition-all ${
                i === stepIndex
                  ? "w-4 bg-primary-500"
                  : "w-1.5 bg-gray-300 dark:bg-gray-600"
              }`}
            />
          ))}
        </div>

        {stepIndex === 0 && (
          <Button size="sm" variant="light" onPress={backToWelcome}>
            {t("返回选择其它模式")}
          </Button>
        )}
      </div>
    </div>
  );
};

export default OobeOverlay;
