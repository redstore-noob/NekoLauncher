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
/*
 * 一言卡片：Hitokoto API 每日一言。结果按本地日期缓存在 localStorage，
 * 同一天固定；右上角刷新可换一条。网络失败时回落到内置句子。
 */
import React, { useCallback, useEffect, useState } from "react";
import { Button } from "@heroui/react";
import {
  ArrowClockwise20Regular as RefreshIcon,
  Chat20Regular,
} from "@fluentui/react-icons";

import { t } from "../../i18n";

import HomeCard from "./HomeCard";

const STORAGE_KEY = "nekolauncher-hitokoto";
const API_URL = "https://v1.hitokoto.cn/?encode=json&charset=utf-8";

interface Quote {
  text: string;
  from: string;
}

/** 网络失败时的兜底句子（无版权顾虑的自造句） */
const FALLBACK_QUOTES: Quote[] = [
  { text: t("把该挖的矿挖完，把该做的梦做完。"), from: "NekoLauncher" },
  { text: t("世界很大，先去把存档备份了。"), from: "NekoLauncher" },
  { text: t("慢慢来，比较快。"), from: "NekoLauncher" },
  { text: t("每一个方块，都从第一镐开始。"), from: "NekoLauncher" },
];

function dateKey(now = new Date()): string {
  const y = now.getFullYear();
  const m = String(now.getMonth() + 1).padStart(2, "0");
  const d = String(now.getDate()).padStart(2, "0");

  return `${y}-${m}-${d}`;
}

function readCache(): Quote | null {
  try {
    const raw = localStorage.getItem(STORAGE_KEY);

    if (!raw) return null;
    const parsed = JSON.parse(raw) as {
      day?: string;
      text?: string;
      from?: string;
    };

    if (parsed.day !== dateKey() || !parsed.text) return null;

    return { text: parsed.text, from: parsed.from ?? "" };
  } catch {
    return null;
  }
}

function writeCache(quote: Quote) {
  try {
    localStorage.setItem(
      STORAGE_KEY,
      JSON.stringify({ day: dateKey(), ...quote }),
    );
  } catch {
    /* 存储不可用时忽略 */
  }
}

const HitokotoCard: React.FC = () => {
  const [quote, setQuote] = useState<Quote | null>(null);
  const [loading, setLoading] = useState(false);

  const fetchQuote = useCallback(async () => {
    setLoading(true);
    try {
      const response = await fetch(API_URL);

      if (!response.ok) throw new Error(`HTTP ${response.status}`);
      const data = (await response.json()) as {
        hitokoto?: string;
        from?: string;
        from_who?: string | null;
      };

      if (!data.hitokoto) throw new Error(t("空响应"));
      const next: Quote = {
        text: data.hitokoto,
        from: data.from_who || data.from || t("一言"),
      };

      setQuote(next);
      writeCache(next);
    } catch {
      // 网络失败：保留已有的一句，只有确实没内容时才用随机兜底
      const fallback =
        FALLBACK_QUOTES[Math.floor(Math.random() * FALLBACK_QUOTES.length)];

      setQuote((prev) => prev ?? fallback);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    const cached = readCache();

    if (cached) {
      setQuote(cached);

      return;
    }
    void fetchQuote();
  }, [fetchQuote]);

  return (
    <HomeCard
      action={
        <Button
          isIconOnly
          className="h-7 w-7 min-w-7 text-gray-400"
          isDisabled={loading}
          radius="full"
          size="sm"
          title={t("换一条")}
          variant="light"
          onPress={() => void fetchQuote()}
        >
          <RefreshIcon />
        </Button>
      }
      icon={<Chat20Regular />}
      label={t("一言")}
      value={quote?.from || t("每日一言")}
    >
      {quote ? (
        <p className="nya-enter text-sm leading-relaxed text-gray-700 dark:text-gray-300">
          「{quote.text}」
        </p>
      ) : (
        <div className="py-3 text-center text-xs text-gray-400">
          {t("正在取一句…")}
        </div>
      )}
    </HomeCard>
  );
};

export default HitokotoCard;
