/*
 * Copyright 2024 Next UI
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
 * 贡献者名单：从 GitHub API 拉取仓库贡献者（头像 + 主页链接）。
 * 匿名 API 限额 60 次/时/IP，所以结果落 localStorage 缓存 24 小时；
 * 离线/限额时展示缓存（若有过期缓存也先用）并提供打开仓库主页的兜底入口。
 */
import React, { useEffect, useState } from "react";
import { Button } from "@heroui/react";

import { BrowserOpenURL } from "../../../wailsjs/runtime/runtime";
import { GITHUB_REPO, GITHUB_REPO_URL } from "../../lib/licenses";
import { t } from "../../i18n";

/** GitHub API 未认证限额 60 次/时/IP，缓存 24 小时足够礼貌 */
const CACHE_KEY = "nekolauncher_contributors_cache_v1";
const CACHE_TTL_MS = 24 * 60 * 60 * 1000;

interface Contributor {
  login: string;
  avatar_url: string;
  html_url: string;
  contributions: number;
}

interface CacheShape {
  fetchedAt: number;
  contributors: Contributor[];
}

function readCache(): Contributor[] | null {
  try {
    const raw = localStorage.getItem(CACHE_KEY);

    if (!raw) return null;
    const parsed = JSON.parse(raw) as CacheShape;

    return Array.isArray(parsed.contributors) && parsed.contributors.length > 0
      ? parsed.contributors
      : null;
  } catch {
    return null;
  }
}

function writeCache(contributors: Contributor[]) {
  try {
    const payload: CacheShape = {
      fetchedAt: Date.now(),
      contributors,
    };

    localStorage.setItem(CACHE_KEY, JSON.stringify(payload));
  } catch {
    /* 存储不可用时只影响下次仍需联网 */
  }
}

async function fetchContributors(signal: AbortSignal): Promise<Contributor[]> {
  const response = await fetch(
    `https://api.github.com/repos/${GITHUB_REPO}/contributors?per_page=100`,
    {
      signal,
      headers: { Accept: "application/vnd.github+json" },
    },
  );

  if (!response.ok) throw new Error(`GitHub API ${response.status}`);
  const list = (await response.json()) as Contributor[];

  // 匿名限额/仓库不存在时返回的是对象或空数组，过滤成干净列表
  return Array.isArray(list)
    ? list.filter((c) => c.login && c.avatar_url && c.html_url)
    : [];
}

type Status = "loading" | "ok" | "error";

const Contributors: React.FC = () => {
  // 先用缓存（即使过期）立即渲染，后台再刷新
  const [contributors, setContributors] = useState<Contributor[]>(
    () => readCache() ?? [],
  );
  const [status, setStatus] = useState<Status>(readCache() ? "ok" : "loading");

  useEffect(() => {
    const controller = new AbortController();
    const cached = readCache();

    // 缓存新鲜（24h 内）就不发请求
    try {
      const raw = localStorage.getItem(CACHE_KEY);

      if (raw) {
        const parsed = JSON.parse(raw) as CacheShape;

        if (Date.now() - parsed.fetchedAt < CACHE_TTL_MS) return;
      }
    } catch {
      /* 缓存损坏则照常拉取 */
    }

    fetchContributors(controller.signal)
      .then((list) => {
        if (controller.signal.aborted) return;
        if (list.length > 0) {
          writeCache(list);
          setContributors(list);
        }
        setStatus("ok");
      })
      .catch(() => {
        if (controller.signal.aborted) return;
        // 有旧缓存兜底时不算错误，只是没刷新
        setStatus(cached ? "ok" : "error");
      });

    return () => controller.abort();
  }, []);

  if (status === "error") {
    return (
      <div className="flex flex-wrap items-center gap-2 justify-end">
        <span className="text-xs text-gray-400">
          {t("无法加载（网络不可用？）")}
        </span>
        <Button
          size="sm"
          variant="flat"
          onPress={() => BrowserOpenURL(GITHUB_REPO_URL)}
        >
          {t("打开仓库主页")}
        </Button>
      </div>
    );
  }

  if (status === "loading") {
    return (
      <div className="flex flex-wrap items-center gap-2 justify-end">
        {[0, 1, 2].map((i) => (
          <div
            key={i}
            className="h-7 w-7 animate-pulse rounded-full bg-gray-200 dark:bg-gray-700"
          />
        ))}
      </div>
    );
  }

  return (
    <div className="flex flex-wrap items-center gap-1.5 justify-end">
      {contributors.map((contributor) => (
        <button
          key={contributor.login}
          aria-label={t("查看贡献者 {0}", { "0": contributor.login })}
          className="group flex items-center gap-1.5 rounded-full border border-transparent py-0.5 pr-2.5 pl-0.5 transition-colors hover:border-gray-200 hover:bg-gray-100 dark:hover:border-gray-700 dark:hover:bg-gray-800"
          title={t("{0} · {1} 次提交", {
            "0": contributor.login,
            "1": contributor.contributions,
          })}
          type="button"
          onClick={() => BrowserOpenURL(contributor.html_url)}
        >
          <img
            alt={t("{0} 的头像", { "0": contributor.login })}
            className="h-6 w-6 rounded-full object-cover"
            loading="lazy"
            referrerPolicy="no-referrer"
            src={contributor.avatar_url}
          />
          <span className="max-w-[8rem] truncate text-xs text-gray-600 dark:text-gray-300">
            {contributor.login}
          </span>
        </button>
      ))}
    </div>
  );
};

export default Contributors;
