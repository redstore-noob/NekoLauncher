/*
 * RSS 订阅卡片：后端抓取并解析订阅源（RSS 2.0 / Atom），展示最近条目，
 * 点击用系统浏览器打开。订阅地址存 launcher.yaml（homeRssFeedUrl），
 * 卡片右上角可编辑；每 10 分钟自动刷新。
 */
import React, { useCallback, useEffect, useRef, useState } from "react";
import { Button, Input } from "@heroui/react";
import {
  ArrowClockwise20Regular as RefreshIcon,
  Checkmark20Regular,
  Rss20Regular,
} from "@fluentui/react-icons";

import { GetValue, SetValue } from "../../../wailsjs/go/bindings/ConfigAPI";
import { FetchRssFeed } from "../../../wailsjs/go/bindings/SystemAPI";
import { BrowserOpenURL } from "../../../wailsjs/runtime/runtime";
import { t } from "../../i18n";

import HomeCard from "./HomeCard";

/** 订阅地址的配置键（launcher.yaml） */
const RSS_URL_KEY = "homeRssFeedUrl";
/** 默认订阅源：Minecraft 官方社区内容 */
const DEFAULT_RSS_URL =
  "https://www.minecraft.net/en-us/feeds/community-content/rss";
/** 自动刷新间隔 */
const REFRESH_INTERVAL_MS = 10 * 60 * 1000;

interface FeedItem {
  Title: string;
  Link: string;
  Published: string;
}

const RssFeedCard: React.FC = () => {
  const [items, setItems] = useState<FeedItem[]>([]);
  const [feedUrl, setFeedUrl] = useState("");
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState("");
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);
  // 换源后旧请求迟到的结果不要覆盖新源
  const loadSeqRef = useRef(0);

  const load = useCallback(async (url: string) => {
    const seq = ++loadSeqRef.current;

    setLoading(true);
    try {
      const fetched = (await FetchRssFeed(url)) as FeedItem[];

      if (seq !== loadSeqRef.current) return;
      setItems(fetched ?? []);
      setError("");
    } catch (ex) {
      if (seq !== loadSeqRef.current) return;
      setError(String((ex as Error)?.message ?? ex));
    } finally {
      if (seq === loadSeqRef.current) setLoading(false);
    }
  }, []);

  useEffect(() => {
    GetValue(RSS_URL_KEY)
      .then((saved) => setFeedUrl(saved || DEFAULT_RSS_URL))
      .catch(() => setFeedUrl(DEFAULT_RSS_URL));
  }, []);

  useEffect(() => {
    if (!feedUrl) return;
    void load(feedUrl);
    const timer = window.setInterval(
      () => void load(feedUrl),
      REFRESH_INTERVAL_MS,
    );

    return () => window.clearInterval(timer);
  }, [feedUrl, load]);

  const startEditing = () => {
    setDraft(feedUrl);
    setEditing(true);
  };

  const applyDraft = async () => {
    const next = draft.trim();

    setEditing(false);
    if (!next || next === feedUrl) return;
    await SetValue(RSS_URL_KEY, next);
    setFeedUrl(next);
  };

  return (
    <HomeCard
      action={
        editing ? (
          <Button
            isIconOnly
            aria-label={t("保存订阅地址")}
            size="sm"
            title={t("保存订阅地址")}
            variant="light"
            onPress={() => void applyDraft()}
          >
            <Checkmark20Regular />
          </Button>
        ) : (
          <div className="flex items-center gap-1">
            <Button
              isIconOnly
              aria-label={t("编辑订阅地址")}
              className="text-[11px] text-gray-400"
              radius="full"
              size="sm"
              title={t("编辑订阅地址")}
              variant="light"
              onPress={startEditing}
            >
              <Rss20Regular />
            </Button>
            <Button
              isIconOnly
              aria-label={t("刷新")}
              className="text-[11px] text-gray-400"
              radius="full"
              size="sm"
              title={t("刷新")}
              variant="light"
              onPress={() => void load(feedUrl)}
            >
              <RefreshIcon className={loading ? "animate-spin" : ""} />
            </Button>
          </div>
        )
      }
      value={
        editing ? (
          <Input
            autoFocus
            aria-label={t("订阅地址")}
            className="w-full"
            placeholder="https://example.com/feed.xml"
            size="sm"
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") void applyDraft();
            }}
          />
        ) : (
          t("RSS 订阅")
        )
      }
    >
      {!editing && error ? (
        <div className="text-xs text-amber-600 dark:text-amber-400">
          {t("加载失败：{0}", { "0": error })}
        </div>
      ) : null}
      {!editing && !error && items.length === 0 ? (
        <div className="text-xs text-gray-400">
          {loading ? t("正在加载…") : t("暂无内容")}
        </div>
      ) : null}
      {!editing && (
        <ul className="flex flex-col gap-1.5">
          {items.map((item, index) => (
            <li key={`${item.Link}-${index}`} className="min-w-0">
              <button
                className="group flex w-full cursor-pointer items-baseline gap-2 text-left"
                type="button"
                onClick={() => item.Link && BrowserOpenURL(item.Link)}
              >
                <span className="min-w-0 flex-1 truncate text-xs text-gray-700 group-hover:text-primary dark:text-gray-300">
                  {item.Title}
                </span>
                {item.Published ? (
                  <span className="flex-none text-[10px] tabular-nums text-gray-400">
                    {item.Published}
                  </span>
                ) : null}
              </button>
            </li>
          ))}
        </ul>
      )}
    </HomeCard>
  );
};

export default RssFeedCard;
