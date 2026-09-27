import js from "@eslint/js";
import react from "eslint-plugin-react";
import reactHooks from "eslint-plugin-react-hooks";
import jsxA11Y from "eslint-plugin-jsx-a11y";
import prettierPlugin from "eslint-plugin-prettier";
import eslintConfigPrettier from "eslint-config-prettier";
import unusedImports from "eslint-plugin-unused-imports";
import _import from "eslint-plugin-import";
import typescriptEslint from "@typescript-eslint/eslint-plugin";
import tsParser from "@typescript-eslint/parser";
import globals from "globals";

// 原生 flat config：
// - eslint-plugin-react 尚未提供可用的 flat recommended（configs.flat 在当前版本为空），
//   这里手动注册插件并沿用其 eslintrc 版 recommended 的规则表；
// - react-hooks 6 / jsx-a11y 6.10 自带 flat 配置，直接使用；
// - eslint-plugin-prettier 的 configs.recommended 仍是 eslintrc 形状（plugins 为数组），
//   因此只注册插件本体 + 用 eslint-config-prettier 关闭冲突规则。
export default [
  {
    ignores: [
      "dist/**",
      "coverage/**",
      "wailsjs/**",
      "node_modules/**",
      "**/*.css",
      "**/*.config.js",
    ],
  },

  js.configs.recommended,

  // 浏览器全局（window/document 等）；TS 文件里 no-undef 交由 tsc 负责，单独关闭
  {
    languageOptions: {
      ecmaVersion: 2022,
      sourceType: "module",
      globals: { ...globals.browser },
    },
  },
  {
    files: ["**/*.ts", "**/*.tsx"],
    rules: { "no-undef": "off" },
  },

  {
    files: ["**/*.{js,mjs,cjs,ts,tsx}"],
    plugins: { react },
    languageOptions: {
      parserOptions: { ecmaFeatures: { jsx: true } },
    },
    settings: { react: { version: "detect" } },
    rules: {
      ...react.configs.recommended.rules,
      "react/prop-types": "off",
      "react/jsx-uses-react": "off",
      "react/react-in-jsx-scope": "off",
      "react/self-closing-comp": "warn",
      "react/jsx-sort-props": [
        "warn",
        {
          callbacksLast: true,
          shorthandFirst: true,
          noSortAlphabetically: false,
          reservedFirst: true,
        },
      ],
    },
  },

  // react-hooks 6 的 flat/recommended 是数组形状，统一摊平后再并入
  ...(Array.isArray(reactHooks.configs["flat/recommended"])
    ? reactHooks.configs["flat/recommended"]
    : [reactHooks.configs["flat/recommended"]]),

  jsxA11Y.flatConfigs.recommended,

  {
    files: ["**/*.{js,mjs,cjs,ts,tsx}"],
    plugins: {
      "@typescript-eslint": typescriptEslint,
      "unused-imports": unusedImports,
      import: _import,
      prettier: prettierPlugin,
    },
    languageOptions: {
      parser: tsParser,
    },
    rules: {
      // 只禁调试用的 log/info：error/warn 是各 catch 分支的正经上报，
      // 一并报 warn 会让 33 条噪音把新问题埋掉（console.log 仅剩插件 log API 一处）
      "no-console": ["warn", { allow: ["error", "warn"] }],
      "prettier/prettier": "warn",
      "no-unused-vars": "off",
      "unused-imports/no-unused-vars": "off",
      "unused-imports/no-unused-imports": "warn",

      "@typescript-eslint/no-unused-vars": [
        "warn",
        {
          args: "after-used",
          ignoreRestSiblings: false,
          argsIgnorePattern: "^_.*?$",
        },
      ],

      "jsx-a11y/click-events-have-key-events": "warn",
      "jsx-a11y/interactive-supports-focus": "warn",
      "jsx-a11y/label-has-associated-control": "warn",
      "jsx-a11y/no-static-element-interactions": "warn",

      "import/order": [
        "warn",
        {
          groups: [
            "type",
            "builtin",
            "object",
            "external",
            "internal",
            "parent",
            "sibling",
            "index",
          ],
          pathGroups: [
            {
              pattern: "~/**",
              group: "external",
              position: "after",
            },
          ],
          "newlines-between": "always",
        },
      ],

      "padding-line-between-statements": [
        "warn",
        {
          blankLine: "always",
          prev: "*",
          next: "return",
        },
        {
          blankLine: "always",
          prev: ["const", "let", "var"],
          next: "*",
        },
        {
          blankLine: "any",
          prev: ["const", "let", "var"],
          next: ["const", "let", "var"],
        },
      ],
    },
  },

  // 必须放在最后：关闭所有与 prettier 冲突的格式类规则
  eslintConfigPrettier,
];
