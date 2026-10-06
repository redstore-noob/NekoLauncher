/*
 * 运行日志查看器：遮罩弹层展示本次运行的日志文件内容。
 * 支持刷新 / 复制 / 导出，并按日志级别着色（INFO 蓝 / WARNING 黄 / ERROR 红 / SUCCESS 绿）。
 */
import React, {
  createContext,
  memo,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import { Button, Modal, ModalContent } from "@heroui/react";
import {
  ArrowClockwise20Regular as RefreshIcon,
  Copy20Regular,
  Save20Regular,
  DocumentText20Regular,
} from "@fluentui/react-icons";

import {
  ClearLogs,
  ExportCurrentLog,
  GetCurrentLog,
  SaveFile,
} from "../../wailsjs/go/bindings/SystemAPI";
import { ClipboardSetText } from "../../wailsjs/runtime/runtime";
import { LOG_LEVEL_CLASS, parseLogLevel } from "../lib/logs";
import { startVisiblePoll } from "../lib/visibility";
import { t } from "../i18n";

import { ModalShell, modalBehaviorProps } from "./modal-shell";

/** 打开期间自动刷新的间隔 */
const POLL_INTERVAL_MS = 1500;

/** 运行日志单行（memo）：1.5 秒轮询重刷时，内容未变的历史行跳过重渲染 */
const LogLine = memo(function LogLine({ line }: { line: string }) {
  return (
    <div
      className={`whitespace-pre-wrap break-all ${LOG_LEVEL_CLASS[parseLogLevel(line)]}`}
    >
      {line || " "}
    </div>
  );
});

interface LogViewerProps {
  isOpen: boolean;
  onClose: () => void;
}

const LogViewer: React.FC<LogViewerProps> = ({ isOpen, onClose }) => {
  const [content, setContent] = useState("");
  const [hint, setHint] = useState("");
  const [busy, setBusy] = useState(false);
  const scrollRef = useRef<HTMLDivElement>(null);

  const reload = useCallback(async () => {
    try {
      const text = (await GetCurrentLog()) as string;

      setContent(text ?? "");
    } catch (ex) {
      setHint(t("读取日志失败：{0}", { "0": (ex as Error)?.message ?? ex }));
    }
  }, []);

  // 打开时立即读取，并在打开期间轮询刷新
  useEffect(() => {
    if (!isOpen) return;
    setHint("");
    void reload();

    return startVisiblePoll(() => void reload(), POLL_INTERVAL_MS);
  }, [isOpen, reload]);

  const lines = useMemo(() => {
    const arr = content.split("\n");

    while (arr.length > 0 && arr[arr.length - 1] === "") arr.pop();

    return arr;
  }, [content]);

  // 新日志到达时滚到底部——但只在自己本来就贴着底部时：
  // 无条件滚动会让用户没法往上翻看历史（日志是持续追加的）
  useEffect(() => {
    const el = scrollRef.current;

    if (!el) return;
    const atBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 24;

    if (atBottom) el.scrollTop = el.scrollHeight;
  }, [lines.length, isOpen]);

  const copy = async () => {
    try {
      await ClipboardSetText(content ?? "");
      setHint(t("已复制到剪贴板"));
    } catch (ex) {
      setHint(t("复制失败：{0}", { "0": (ex as Error)?.message ?? ex }));
    }
  };

  const exportLog = async () => {
    setBusy(true);
    setHint("");
    try {
      const stamp = new Date().toISOString().slice(0, 19).replace(/[:T]/g, "-");
      const path = (await SaveFile(
        t("导出运行日志"),
        `NekoLauncher-${stamp}.log`,
        t("日志文件"),
        "*.log",
      )) as string;

      if (!path) return; // 取消
      await ExportCurrentLog(path);
      setHint(t("已导出日志"));
    } catch (ex) {
      setHint(t("导出失败：{0}", { "0": (ex as Error)?.message ?? ex }));
    } finally {
      setBusy(false);
    }
  };

  const clear = async () => {
    setBusy(true);
    setHint("");
    try {
      const count = await ClearLogs();

      setHint(t("已清空 {0} 个日志文件", { "0": count }));
      await reload();
    } catch (ex) {
      setHint(t("清空失败：{0}", { "0": (ex as Error)?.message ?? ex }));
    } finally {
      setBusy(false);
    }
  };

  return (
    <Modal isOpen={isOpen} size="3xl" onClose={onClose} {...modalBehaviorProps}>
      <ModalContent className="max-h-[85vh]">
        <ModalShell
          icon={<DocumentText20Regular />}
          subtitle={t("本次运行 · 共 {0} 行", { "0": lines.length })}
          title={t("运行日志")}
          onClose={onClose}
        >
          {/* 工具行 */}
          <div className="flex flex-wrap items-center gap-2">
            <Button
              isLoading={busy}
              size="sm"
              startContent={!busy ? <RefreshIcon /> : undefined}
              variant="flat"
              onPress={() => void reload()}
            >
              {t("刷新")}
            </Button>
            <Button
              size="sm"
              startContent={<Copy20Regular />}
              variant="flat"
              onPress={() => void copy()}
            >
              {t("复制")}
            </Button>
            <Button
              color="primary"
              size="sm"
              startContent={<Save20Regular />}
              onPress={() => void exportLog()}
            >
              {t("导出")}
            </Button>
            <Button
              className="ml-auto"
              color="danger"
              size="sm"
              variant="light"
              onPress={() => void clear()}
            >
              {t("清空")}
            </Button>
          </div>

          {hint ? <div className="text-xs text-gray-400">{hint}</div> : null}

          {/* 日志正文：等宽字体 + 级别着色 */}
          <div
            ref={scrollRef}
            className="nya-scroll h-[55vh] overflow-y-auto rounded-lg border nya-border bg-black/[0.03] p-3 font-mono text-[11px] leading-relaxed dark:bg-white/[0.03]"
          >
            {lines.length === 0 ? (
              <div className="py-10 text-center text-gray-400">
                {t("暂无日志")}
              </div>
            ) : (
              lines.map((line, index) => <LogLine key={index} line={line} />)
            )}
          </div>
        </ModalShell>
      </ModalContent>
    </Modal>
  );
};

export default LogViewer;

// ---- 全局单例：任何地方都能通过 useLogViewer().openLogs() 打开同一个遮罩层 ----
// 标题栏、设置页、运行日志小组件共用这一个查看器，避免各自维护弹层状态。

interface LogViewerContextValue {
  openLogs: () => void;
}

const LogViewerContext = createContext<LogViewerContextValue>({
  openLogs: () => {
    /* Provider 未挂载时的空实现 */
  },
});

export function useLogViewer(): LogViewerContextValue {
  return useContext(LogViewerContext);
}

/**
 * 模块级打开入口：供非 React 上下文（如崩溃提示、事件回调）调用。
 * LogViewerProvider 挂载时把真实的 openLogs 注册进来；未挂载时是空实现。
 */
let openLogsBridge: (() => void) | null = null;

/** 打开运行日志查看器（组件外可用）。 */
export function openLogViewer(): void {
  openLogsBridge?.();
}

export const LogViewerProvider: React.FC<{ children: React.ReactNode }> = ({
  children,
}) => {
  const [isOpen, setIsOpen] = useState(false);
  const openLogs = useCallback(() => setIsOpen(true), []);
  const close = useCallback(() => setIsOpen(false), []);

  // 注册/注销全局桥接，让崩溃提示等非组件代码也能打开同一个查看器
  useEffect(() => {
    openLogsBridge = openLogs;

    return () => {
      openLogsBridge = null;
    };
  }, [openLogs]);

  return (
    <LogViewerContext.Provider value={{ openLogs }}>
      {children}
      <LogViewer isOpen={isOpen} onClose={close} />
    </LogViewerContext.Provider>
  );
};
