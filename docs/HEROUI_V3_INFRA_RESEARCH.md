# HeroUI v3 non-component infrastructure research

Scope: `@heroui/react@3.2.6` + `@heroui/styles@3.2.6` vs `@heroui/react@2.6.14` + `@heroui/theme@2.4.26`.
All package facts below were read from the **published tarball contents on unpkg** (not from memory), plus the
official docs fetched as markdown. Anything not directly verified says **unverified**.

Sources used (treated strictly as untrusted data):

- https://heroui.com/en/docs/react/migration/styling (full markdown at `.../styling.mdx`)
- https://heroui.com/en/docs/react/getting-started/quick-start
- https://heroui.com/en/docs/react/migration (index), `/full-migration`, `/incremental-migration`
- https://heroui.com/en/docs/react/getting-started/theming , `/colors` , `/styling` , `/animation` , `/composition` , `/frameworks`
- https://heroui.com/en/docs/react/migration/modal , `/popover` , `/tooltip` , `/dropdown` , `/link`
- https://unpkg.com/@heroui/styles@3.2.6/... (package.json, dist/*.css, dist/**/*.styles.js)
- https://unpkg.com/@heroui/react@3.2.6/... (package.json, dist/index.js, dist/**/*.d.ts, dist/components/**/*.js)
- https://reactspectrum.blob.core.windows.net/.../docs/react-aria/routing.html (React Aria "Client Side Routing")

---

## 0. BLOCKING prerequisite discovered in this repo

`frontend/package.json` pins **`react@18.3.1` / `react-dom@18.3.1`**.
HeroUI v3 requires **React 19+** (quick-start "Requirements"), and the peer range in
`@heroui/react@3.2.6/package.json` is `react: ">=19.0.0"`, `react-dom: ">=19.0.0"`.
React must be upgraded before or with the HeroUI switch — this is not optional.
`react-router-dom@^6.30.3` is fine (React Aria's documented example uses `react-router-dom`).

---

## 1. Install & CSS setup

### 1.1 Packages

```
npm uninstall @heroui/react @heroui/theme @heroui/system
npm install @heroui/styles @heroui/react
```

Also remove `@heroui/theme` from the `overrides` block in `frontend/package.json` (it is there today).

Peer dependencies of `@heroui/react@3.2.6` (verified from its package.json) — all must be satisfiable:

```
"@internationalized/date": "^3.12.4"   <-- NEW peer in 3.2.6 (v3.2.6 release note)
"@react-aria/ssr": "^3.10.1"
"@react-aria/utils": "^3.34.1"
"react": ">=19.0.0"
"react-aria": "^3.52.1"
"react-aria-components": "^1.21.1"
"react-dom": ">=19.0.0"
"tailwindcss": ">=4.0.0"
```

`@heroui/styles` has **no React dependency**; its only deps are `tailwind-variants@3.3.1` and
`tw-animate-css@1.4.0`, peer `tailwindcss >= 4.0.0`.

### 1.2 The Tailwind plugin is gone — exact before/after

| | v2 (this repo today) | v3 |
|---|---|---|
| CSS entry (`frontend/src/styles/globals.css`) | `@import "tailwindcss";` + `@plugin '../hero.ts';` + `@source '../../node_modules/@heroui/theme/dist/**/*.{js,ts,jsx,tsx}';` | `@import "tailwindcss";` then `@import "@heroui/styles";` |
| Plugin file | `frontend/src/hero.ts` (`export default heroui()`) | **delete the file** |
| `tailwind.config.js` | required for `heroui()` | not required by HeroUI at all; delete it if HeroUI was its only purpose, otherwise keep the file and remove the `heroui()` plugin entry |

Required literal v3 CSS (order matters — `tailwindcss` must be first):

```css
@import "tailwindcss";
@import "@heroui/styles";
```

`@heroui/styles/dist/index.css` itself begins with `@layer theme, base, components, utilities;` and then
`@import "tailwindcss"; @import "tw-animate-css";` plus `base/`, `components/`, `themes/default/`,
`utilities/`, `variants/` — so you do not need to re-declare the layer order yourself (harmless if you do).

Keep this repo's `@custom-variant dark (&:is(.dark *));` if `dark:` utilities are used: v3's own theme keys off
a `.dark` class **or** `[data-theme="dark"]` on any ancestor (theming doc + `themes/default/variables.css`).

### 1.3 What replaces the plugin options (colors / radius / fontSize / layout)

There is **no** plugin and therefore **no** equivalent of the `heroui({...})` option object:

- `layout.fontSize` → gone; v3 uses plain Tailwind text sizes (`text-xs/sm/base/lg`). There are no
  `--heroui-font-size-*` variables any more.
- `layout.radius` → replaced by one base token `--radius` (default `0.5rem`); everything else is
  `calc()`-derived (see §2.4).
- `themes.light.colors.*` → plain CSS custom properties in your stylesheet (see §2.5).
- `layout.dividerWeight`, `disabledOpacity`, etc. → `--border-width`, `--disabled-opacity`, `--cursor-*`.

### 1.4 Is `@source .../node_modules/...` still needed? → **No.**

Verified by reading the shipped JS: every variant function in
`@heroui/styles/dist/components/*/*.styles.js` emits **BEM class names only**, e.g.

```js
const buttonVariants = tv({ base: "button", variants: { variant: { primary: "button--primary" }, size: { md: "button--md" } } });
const tabsVariants = tv({ slots: { base: "tabs", tab: "tabs__tab", tabIndicator: "tabs__indicator" } });
```

No Tailwind utility strings are generated at runtime by HeroUI, and the visual styles live in
`@heroui/styles/dist/components/*.css` which Tailwind processes as part of the imported stylesheet
(`@apply` inside it is resolved by Tailwind). Consequently the v2 `@source '.../@heroui/theme/dist/**'`
line has **no v3 equivalent and should be deleted**. Nothing in the v3 web docs mentions `@source`
for the web packages (checked https://heroui.com/api/agent/search?q=%40source → only a Native hit).

### 1.5 Selective imports (optional, for reference)

```css
@layer theme, base, components, utilities;
@import "tailwindcss";
@import "@heroui/styles/base" layer(base);
@import "@heroui/styles/themes/shared/theme.css" layer(theme);
@import "@heroui/styles/themes/default" layer(theme);
@import "@heroui/styles/components" layer(components);
@import "@heroui/styles/utilities" layer(utilities);
@import "@heroui/styles/variants" layer(utilities);
```

---

## 2. Color system overhaul

### 2.1 The two naming layers (this is the key mental model)

v3.0.5 changed color tokens to "unprefixed source variables". There are two layers:

1. **Source variables** (`themes/default/variables.css`, `@layer base`) — what you override:
   `--accent`, `--surface`, `--muted`, `--border`, `--radius`, …
2. **Tailwind tokens** (`themes/shared/theme.css`, `@theme inline`) — what creates utility classes:
   `--color-accent: var(--accent)` → `bg-accent`, `text-accent`, `border-accent`, …

`--heroui-` prefix is **completely gone**: 0 occurrences of `--heroui-` in the shipped CSS.

### 2.2 Full mapping for the v2 variables this repo uses

| v2 | v3 | Notes |
|---|---|---|
| `--heroui-primary-50` … `-900` | **no equivalent** | scales deleted. Use `--accent-soft` (light), `--accent` (base), `--accent-hover` (dark) |
| `--heroui-primary` / `--heroui-primary-foreground` | `--accent` / `--accent-foreground` | |
| derived hover | `--accent-hover` = `color-mix(in oklab, var(--accent) 90%, var(--accent-foreground) 10%)` | |
| derived soft / soft-hover / soft-foreground | `--accent-soft` (15% accent + transparent), `--accent-soft-hover` (20%), `--accent-soft-foreground` (accent mixed with foreground) | |
| `--heroui-danger` (`danger-500`) | `--danger` | same name survives |
| `--heroui-danger-50` / `-600` | `--danger-soft` / `--danger-hover` | |
| `--heroui-success*`, `--heroui-warning*` | `--success`, `--success-foreground`, `--success-hover`, `--success-soft*`; same for `warning` | |
| `--heroui-default-50..900` | **scales gone**; `--default`, `--default-foreground`, `--default-hover`, `--default-soft`, `--default-soft-foreground`, `--default-soft-hover` | |
| `--heroui-secondary*` | **REMOVED** | no `--secondary` variable exists. In v3 "secondary" is only a *component variant*: `.button--secondary { --button-bg: var(--default); --button-fg: var(--accent-soft-foreground) }` |
| `--heroui-content1` | `--surface` (non-overlay: cards, accordions) — or `--overlay` for floating things | |
| `--heroui-content2` | `--surface-secondary` | |
| `--heroui-content3` | `--surface-tertiary` | |
| `--heroui-content4` | **does not exist** | the styling doc's `bg-surface-quaternary` is a **doc bug**: `--color-surface-quaternary` / `--color-surface-quaternary-foreground` are absent from `themes/shared/theme.css`, `variables.css` and the prebuilt `heroui.min.css` (0 occurrences of "quaternary"). Use `bg-surface-tertiary` or a `color-mix`. |
| `--heroui-foreground-400` / `-500` | `--muted` | |
| (backgrounds) | `--background`, `--background-secondary`, `--background-tertiary`, `--background-inverse` | |
| (borders/dividers) | `--border`, `--border-secondary`, `--border-tertiary`, `--separator`, `--separator-secondary`, `--separator-tertiary` | |
| (fields) | `--field-background`, `--field-foreground`, `--field-placeholder`, `--field-border`, `--field-hover`, `--field-focus`, `--field-border-hover`, `--field-border-focus`, `--field-radius` | new subsystem |
| (misc) | `--focus`, `--link`, `--segment`, `--segment-foreground`, `--backdrop`, `--scrollbar-*`, `--surface-hover` | |
| (shadows) | `--surface-shadow`, `--overlay-shadow`, `--field-shadow` | size-based `box-shadow-small/medium/large` gone |
| (radius) | see §2.4 | |

### 2.3 HSL → OKLCH — exact override format

v2 values were bare HSL **channels** (`212 100% 47%`) consumed as `hsl(var(--heroui-primary-500) / 0.22)`.
v3 values are **complete colors** in OKLCH, so they must not be wrapped in `hsl(...)`:

```css
:root {
  --accent: oklch(0.6204 0.195 253.83); /* L C H  — L may be a fraction or a percentage */
  --accent-foreground: oklch(0.9911 0 0); /* == var(--snow) */
}
```

Two verified consequences:

1. **Alpha must become `color-mix`.** `hsl(var(--heroui-primary-500) / 0.22)` →
   `color-mix(in oklab, var(--accent) 22%, transparent)`.
   This affects 13 places in `frontend/src/styles/globals.css` (lines ~156, 387, 390, 550–551, 558, 561,
   580–584, 614–615).
2. **Derived tokens update automatically.** `--accent-hover`, `--accent-soft`, `--accent-soft-hover` are declared
   as `color-mix(... var(--accent) ...)` in `variables.css`; overriding only `--accent` /
   `--accent-foreground` re-derives all of them. `frontend/src/theme-color.tsx` currently writes **11**
   variables (`--heroui-primary-50…900`, `--heroui-primary`, `--heroui-primary-foreground`); in v3 the same
   effect is **2** variables, written in exactly the same way (inline style on `document.documentElement`,
   which outranks the `@layer base` theme block).

Verified bonus for the runtime theme feature: in `themes/default/variables.css` the common `:root, :host` block
declares `--accent` and `--accent-foreground` **once, outside** the light/dark blocks, so a single override
covers light *and* dark. (`--danger` and `--warning` do differ per theme; `--success` is also common.)

Precedence caveat: `:root` theme values live in `@layer base`, so unlayered CSS and inline styles both win —
the existing `root.style.setProperty(...)` approach keeps working. (**unverified**: whether a *later* `:root`
rule in the repo's own globals.css also wins — it should, being unlayered, but I did not build the project.)

### 2.4 `--radius-*` tokens (from `themes/shared/theme.css`)

```
--radius: 0.5rem;                     /* single knob */
--radius-xs:  calc(var(--radius) * 0.25)  /*  2px */
--radius-sm:  calc(var(--radius) * 0.5)   /*  4px */
--radius-md:  calc(var(--radius) * 0.75)  /*  6px */
--radius-lg:  calc(var(--radius) * 1)     /*  8px */
--radius-xl:  calc(var(--radius) * 1.5)   /* 12px */
--radius-2xl: calc(var(--radius) * 2)     /* 16px */
--radius-3xl: calc(var(--radius) * 3)     /* 24px */
--radius-4xl: calc(var(--radius) * 4)     /* 32px */
--radius-field: var(--field-radius, calc(var(--radius) * 1.5))  /* 12px */
```

Difference vs v2: v2 had **fixed, independent** values (`small 8px`, `medium 12px`, `large 14px`) set through
`layout.radius`; v3 derives the whole scale from one `--radius`, so changing it moves every size at once.
These `@theme inline` declarations **override Tailwind v4's built-in `--radius-*` scale**, so `rounded-sm` is
4px, not Tailwind's 2px.

Two different answers depending on intent:

- Mechanical rename from the docs table: `rounded-small`→`rounded-sm` (8px→4px), `rounded-medium`→`rounded-md`
  (12px→6px), `rounded-large`→`rounded-lg` (14px→8px). All **visually smaller**.
- Visual parity at default `--radius: 0.5rem`: `rounded-small`(8px)→`rounded-lg`, `rounded-medium`(12px)→
  `rounded-xl`, `rounded-large`(14px)→`rounded-[14px]` (no token matches 14px).

Also note components got much rounder by default: `.button` uses `rounded-3xl` (24px), Card `rounded-3xl`,
Chip `rounded-2xl`; inputs use `rounded-field`.
The v3.0.4 release note mentions "border-radius design tokens with `min()` capping" — **not present in 3.2.6**:
`themes/shared/theme.css` uses plain `calc(var(--radius) * n)`.

### 2.5 Where to put overrides

```css
/* globals.css */
@import "tailwindcss";
@import "@heroui/styles";

:root {
  --accent: oklch(0.62 0.19 253);
  --radius: 0.75rem;
}
.dark, [data-theme="dark"] {
  --danger: oklch(0.594 0.1967 24.63);
}

/* to add a NEW semantic color it must also be bridged to Tailwind: */
@theme inline {
  --color-info: var(--info);
  --color-info-foreground: var(--info-foreground);
}
```

---

## 3. Utility class renames

**There is no complete rename table.** The styling-example page's "Utility Classes Mapping" table has ~16 rows
and is explicitly partial; component docs carry the rest. Ground truth for what classes exist is the
`@theme inline` block in `themes/shared/theme.css` (every `--color-x` there yields `bg-x`, `text-x`, `border-x`,
`fill-x`, …). Mapping for the classes this repo uses heavily:

| v2 | v3 | status |
|---|---|---|
| `bg-primary` | `bg-accent` | doc-verified |
| `text-primary-500`, `bg-primary-500` | `text-accent`, `bg-accent` | scale removed |
| `bg-primary-100`, `bg-primary-500/15` | `bg-accent-soft`, `bg-accent/15` | opacity modifier works because `--color-accent` is a real color |
| `text-primary-600 dark:text-primary-300` | `text-accent` (or `text-accent-soft-foreground` for the softer look) | |
| `bg-primary-900/40` | `bg-accent-soft` (already theme-aware) | |
| `bg-content1` | `bg-surface`, **or `bg-overlay`** for popovers/modals/menus/tooltips | doc-verified |
| `bg-content2/3/4` | `bg-surface-secondary` / `bg-surface-tertiary` / (**no quaternary — see §2.2**) | |
| `bg-default-100` | `bg-default` (v2 `default-100` was the light neutral fill) — alternative `bg-surface-secondary` | **derived, not doc-stated** |
| `hover:bg-default-200`, `bg-default-200/70` | `hover:bg-default-hover`, `bg-default-hover/70` (token `--color-default-hover` exists) | derived |
| `bg-default-300/60` | `bg-border/60` or `bg-surface-tertiary` | derived |
| `text-foreground-400`, `text-foreground-500` | `text-muted` | doc-verified |
| `text-tiny` | `text-xs` | doc-verified, same 12px/16px |
| `text-small` | `text-sm` | doc-verified, same 14px/20px |
| `text-medium` / `text-large` | `text-base` / `text-lg` | doc-verified |
| `border-default-200`, `border-default-200/60` | `border-border`, `border-border/60` | token `--color-border` exists; `--color-border-secondary` / `-tertiary` for stronger lines |
| `border-default-300` | `border-border-secondary` | derived |
| `border-small/medium/large` | `border` / `border-2` / `border-[3px]` | doc-verified |
| `rounded-small/medium/large` | see §2.4 | doc-verified but **values change** |
| `.transition-background`, `.transition-colors-opacity`, `.transition-width/-height/-size/-left`, `.transition-transform-*` | removed → standard `transition-*` (v2 default was 250ms `ease`) | doc-verified |
| `.scrollbar-hide`, `.scrollbar-default` | `scrollbar`, `scrollbar-thin`, `scrollbar-default`, `scrollbar-none` (from `@heroui/styles`) + `data-scrollbar="thin\|default\|none"` on an ancestor | doc-verified; 0 occurrences of `scrollbar-hide` in v3 CSS |
| `.leading-inherit` | `leading-[inherit]` | doc-verified |
| `.tap-highlight-transparent` | `[-webkit-tap-highlight-color:transparent]` or the `no-highlight` utility | doc-verified |
| `.input-search-cancel-button-none` | custom CSS | doc-verified |
| spinner animation utilities | internal to components, not exported | doc-verified |

Practical warning for the migration: an unknown Tailwind v4 utility in JSX/TSX is **silently not generated**
(no error), so stale `bg-content1`/`text-primary-500` just render unstyled. Inside `@apply` Tailwind v4 *does*
throw "Cannot apply unknown utility class". `frontend/src/styles/globals.css` contains no `@apply`
(verified by grep) — its v2 coupling is the 13 raw `hsl(var(--heroui-primary-*))` uses described in §2.3.

Also: `classNames` → `className` everywhere (styling doc, top note).

---

## 4. Provider removal & router integration

### 4.1 `HeroUIProvider` is gone

- Not exported by `@heroui/react@3.2.6`. `dist/index.js` contains **0** occurrences of `HeroUIProvider`.
- `@heroui/system` is no longer a dependency and should be removed (`frontend/package.json` line 17,
  and the import in `frontend/src/provider.tsx` line 20).
- Nothing needs to wrap the app for portals/overlays. React Aria portals overlays to `document.body` itself;
  `@heroui/styles/dist/base/base.css` defines the shared stacking contract
  (`--z-index-overlay: 100000`, `--z-index-toast: calc(var(--z-index-overlay) + 1)`) and explicitly documents
  that portal mount order decides paint order.
- Locale is the only thing that may still need a provider: React Aria's `I18nProvider` (re-exported from
  `@heroui/react`). Optional.

### 4.2 Router: `RouterProvider` replaces the `navigate`/`useHref` provider props

`dist/index.js` re-exports exactly these from `react-aria-components`:

```js
export {
  Collection, DisclosureStateContext, Focusable, I18nProvider, ListBoxLoadMoreItem, ListLayout,
  OverlayTriggerStateContext, Pressable, RouterProvider, TableLayout, Virtualizer, isRTL,
  parseColor, useFilter, useLocale
} from 'react-aria-components';
```

So `import { RouterProvider } from "@heroui/react"` (or from `react-aria-components`) is the direct
replacement for `<HeroUIProvider navigate={navigate} useHref={useHref}>`. React Aria's Client Side Routing
page documents `navigate` (required) and `useHref` (optional, for `basename`), and gives the React Router
recipe verbatim. The type-augmentation module also changes from `@react-types/shared` to
`react-aria-components`.

Verified that this actually affects HeroUI's own `Link`: `dist/components/link/link.js` renders
`react-aria-components/Link`, so it consumes the RAC router context.

Drop-in rewrite of `frontend/src/provider.tsx`:

```tsx
import type { NavigateOptions } from "react-router-dom";

import { MotionConfig } from "framer-motion";
import { RouterProvider } from "@heroui/react";   // re-export of react-aria-components
import { useHref, useNavigate } from "react-router-dom";

declare module "react-aria-components" {           // was "@react-types/shared"
  interface RouterConfig {
    routerOptions: NavigateOptions;
  }
}

export function Provider({ children }: { children: React.ReactNode }) {
  const navigate = useNavigate();

  return (
    // <Routes> must be rendered INSIDE RouterProvider (React Aria routing doc)
    <RouterProvider navigate={navigate} useHref={useHref}>
      {children}
    </RouterProvider>
  );
}
```

`<MotionConfig reducedMotion="user">` can stay (framer-motion remains a direct dependency of this repo) —
it is no longer required by HeroUI, but it still governs the repo's own motion usages.
The theming/styling packages do not need the provider at all.

### 4.3 Other ways to make links use the router

- `render` prop (the **verified** polymorphic mechanism in 3.2.6; the composition doc shows it for `Link`,
  `Popover.Content`, `Tooltip.Content`, `Button`, …):

  ```tsx
  <Link render={({ ref, ...domProps }) => (
    <ReactRouterLink {...domProps} ref={ref as React.Ref<HTMLAnchorElement>} to="/privacy" />
  )}>
    Privacy Policy
  </Link>
  ```

  Rules: same element type as expected, single root element, spread the provided props onto the DOM node.
- Style the router's own component: `<ReactRouterLink className="link" to="/about">` (BEM block `.link`
  exists in `@heroui/styles/dist/components/link.css`), or `className={buttonVariants({variant:"primary"})}`
  (`buttonVariants`, `linkVariants`, `cn`, `tv` are all exported from `@heroui/react` **and** `@heroui/styles`).
- `Button` has **no `href`** (verified: `ButtonRootProps` = RAC `Button` props; `Link` is the anchor).
  Link-buttons are `Link` + `buttonVariants(...)` or `Link` + `render`.

### 4.4 `asChild` / `as` — not available

`asChild` appears **0** times in the sampled 3.2.6 dist files (`index.js`, `button.js`, `link.js`, `card.js`,
`tabs.js`, `utils/compose.js`). The Link migration doc states "v3 Link does not support `as` or `asChild`",
and the v3.0.0-beta.3 release note records `asChild` removal. The full-migration checklist line
"Consider using `asChild` prop for flexible composition" is **stale documentation** — use `render`.

---

## 5. framer-motion removal

- `framer-motion` is **not** in `@heroui/react@3.2.6` peerDependencies, dependencies, or optionalDependencies
  (verified from package.json), and the string `framer-motion` appears **0** times in `dist/index.js`.
- `motionProps` appears **0** times in `dist/index.js`. Per-component migration guides list it as removed for
  Modal, Popover, Tooltip, Dropdown — and state "animations handled differently". (The same row is expected for
  other v2 overlay components; I verified the four named ones only.)
- `disableAnimation` (provider-level) is gone too; there is **no global animation toggle** in v3.

How v3 animates (from the Animation doc + shipped CSS):

1. **CSS transitions/animations inside the component CSS**, driven by `data-*` attributes and native
   pseudo-classes: `[data-entering]` / `[data-exiting]` (overlay enter/exit), `[data-hovered]`,
   `[data-pressed]`, `[data-focus-visible]`, `[data-disabled]`, `[aria-expanded]`. Example from the docs:

   ```css
   .popover[data-entering] { @apply animate-in zoom-in-90 fade-in-0 duration-200; }
   .popover[data-exiting]  { @apply animate-out zoom-out-95 fade-out duration-150; }
   ```
   (the `animate-in` / `zoom-in-90` / `fade-in-0` utilities come from `tw-animate-css`, a dependency of
   `@heroui/styles` that `dist/index.css` imports for you).
2. **Per-component `render` prop** when a JS animation library is genuinely needed — the composition doc's
   example: `<Button render={(domProps, {isPressed}) => <motion.button {...domProps} animate={{scale: isPressed ? 0.9 : 1}} />}>`.
3. **Wrapping** with the library of your choice (`motion.div`, `AnimatePresence`) — the Animation doc keeps a
   full Framer Motion section, but only as an external library you bring yourself. Wrap HeroUI components; do
   not pass `motionProps` into them.
4. **Reduced motion**: HeroUI extends Tailwind's `motion-reduce:` variant so it also honours a
   `data-reduce-motion="true"` attribute on `<html>`/`<body>`; component CSS puts `motion-reduce:transition-none`
   *after* `transition` for specificity. `MotionConfig reducedMotion="user"` is the framer-side equivalent.

Recommendation for this repo: `framer-motion@11.18.2` is already a **direct** dependency
(`frontend/package.json` line 23) and is used for the repo's own animations, so it can stay. What must be
removed is the ~48 `motionProps`/`popoverProps={{motionProps: ...}}` / `tooltipMotionProps` pass-throughs into
HeroUI components (and `frontend/src/lib/motion.ts` becomes dead code for the HeroUI path — it may still be
needed for the repo's own `motion.*` usages). `frontend/src/main.tsx`'s "no StrictMode because of
framer-motion double-mount" note is about the repo's own usage, not HeroUI.

---

## 6. Tailwind version requirement

- `@heroui/styles@3.2.6` peerDependencies: **`"tailwindcss": ">=4.0.0"`**; `@heroui/react@3.2.6` has the same
  peer. This repo has `tailwindcss@4.1.11` + `@tailwindcss/postcss@4.1.11` + `@tailwindcss/vite@4.1.11`
  → **sufficient**; no Tailwind upgrade needed. Docs say "Tailwind CSS v4" (quick-start Requirements).
- **No `@plugin` directive for HeroUI.** There is no v3 Tailwind plugin: `@heroui/styles` exports only
  `dist/index.js` (variant functions) and `dist/index.css` (styles) plus subpath CSS; nothing registers a
  Tailwind plugin. The `@plugin '../hero.ts'` line in `frontend/src/styles/globals.css` and the whole
  `frontend/src/hero.ts` file must be deleted. (Note: the package *description* string still says
  "HeroUI core styles and Tailwind plugin" — that wording is stale.)
- Consequence: the v2 `content` glob `./node_modules/@heroui/theme/dist/**/*.{js,ts,jsx,tsx}` is unnecessary,
  and Tailwind v4's automatic content detection (which skips `node_modules`) is not a problem, per §1.4.

---

## 7. Residual uncertainty / things I could not verify

- I could not run a Tailwind build in this repo (no network from the shell; `npm`/registry access fails), so
  all class-generation claims are derived from reading the shipped `@theme inline` definitions rather than
  from an actual compiled output.
- The `bg-default-100 → bg-default` / `bg-default-200 → bg-default-hover` rows are **my derivation** from the
  available tokens, not statements in the docs. Visually v2 `default-100/200` were the light neutral greys and
  v3 exposes only one `--default` plus `--default-hover`/`--default-soft`; exact pixel parity is not guaranteed.
- Whether `@heroui/styles` behaves identically under `@tailwindcss/vite` vs `@tailwindcss/postcss` — unverified;
  this repo uses both.
- The styling doc's `bg-surface-quaternary` is documented but does not exist in 3.2.6 (verified absent). If a
  future release adds `--surface-quaternary`, that row becomes valid; treat as of 3.2.6.
