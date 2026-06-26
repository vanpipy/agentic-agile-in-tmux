/**
 * awp-extension.ts — pi extension for awp v2 interception (Phase 4).
 *
 * Flow:
 *   1. pi starts with --extension /path/to/awp-extension.ts
 *   2. On every tool_call / user_bash / input / confirm event,
 *      this extension:
 *      a) Reads ~/.config/awp/interception.json
 *      b) Checks if command matches whitelist (allow) or blacklist (block)
 *      c) If neither: calls ctx.ui.confirm(...) to ask awp's TUI
 *   3. awp's TUI shows a modal, user decides (y/n/a/esc)
 *   4. awp sends extension_ui_response { id, confirmed, ... } to pi
 *   5. The confirm() promise resolves; we return block: true|false
 *
 * The extension is intentionally small (~150 lines) and stateless —
 * all decision logic lives in awp's TUI or in the config file.
 *
 * To load: `pi --extension /path/to/awp-extension.ts`
 *
 * See pi-mono/packages/coding-agent/src/core/extensions/types.ts for
 * the ExtensionAPI surface.
 */

import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";

// ============================================================================
// Pure helpers (testable without pi)
// ============================================================================

export type InterceptionConfig = {
  enabled: boolean;
  // BlockPatterns (canonical, per SYSTEM_DESIGN §6.5)
  block_patterns: string[];
  // AllowPatterns (canonical, per SYSTEM_DESIGN §6.5)
  allow_patterns: string[];
  // Legacy aliases (Phase 4 initial; still supported for backward compat)
  blacklist?: string[];
  whitelist?: string[];
};

/**
 * matchRule reports whether pattern matches command using simple glob.
 * - "literal"   → exact match
 * - "prefix*"   → starts with
 * - "*suffix"   → ends with
 * - "*foo*"     → contains
 * - "*"         → match all
 */
export function matchRule(pattern: string, command: string): boolean {
  if (pattern === "") return false;
  if (pattern === "*") return true;
  if (command === "") return false;
  const hasPrefix = pattern.startsWith("*");
  const hasSuffix = pattern.endsWith("*");
  const trimmed = pattern.slice(
    hasPrefix ? 1 : 0,
    hasSuffix ? -1 : undefined
  );
  if (hasPrefix && hasSuffix) return command.includes(trimmed);
  if (hasSuffix) return command.startsWith(trimmed);
  if (hasPrefix) return command.endsWith(trimmed);
  return pattern === command;
}

/**
 * isBlocked / isAllowed — check if command matches any rule.
 * Spec §6.5: allow_patterns checked FIRST (if matches, no interception).
 */
export function isBlocked(cfg: InterceptionConfig, cmd: string): boolean {
  const all = [...(cfg.block_patterns ?? []), ...(cfg.blacklist ?? [])];
  return all.some((p) => matchRule(p, cmd));
}

export function isAllowed(cfg: InterceptionConfig, cmd: string): boolean {
  const all = [...(cfg.allow_patterns ?? []), ...(cfg.whitelist ?? [])];
  return all.some((p) => matchRule(p, cmd));
}

/**
 * formatToolCallMessage produces the modal title and body for a
 * tool_call event. Truncates long args.
 */
export function formatToolCallMessage(
  toolName: string,
  args: unknown
): { title: string; body: string } {
  const argsStr = safeStringify(args);
  const truncated = argsStr.length > 200 ? argsStr.slice(0, 197) + "..." : argsStr;
  return {
    title: `Allow ${toolName}?`,
    body: `Pi wants to run ${toolName}(${truncated})`,
  };
}

function safeStringify(v: unknown): string {
  if (typeof v === "string") return v;
  try {
    return JSON.stringify(v);
  } catch {
    return String(v);
  }
}

// ============================================================================
// Config loading (best effort — never block)
// ============================================================================

const CONFIG_PATH_ENV = "AWP_INTERCEPTION_CONFIG";
const RELOAD_DEBOUNCE_MS = 200; // debounce file change events

function defaultConfigPath(): string {
  // Linux: ~/.config/awp/interception.json
  // macOS: same (XDG-style)
  // Windows: %APPDATA%/awp/interception.json (not used in Phase 4)
  const home = process.env.HOME || process.env.USERPROFILE || ".";
  return `${home}/.config/awp/interception.json`;
}

async function loadConfig(): Promise<InterceptionConfig> {
  // Try AWP_INTERCEPTION_CONFIG override first
  const override = process.env[CONFIG_PATH_ENV];
  const path = override || defaultConfigPath();
  try {
    const fs = await import("fs/promises");
    const data = await fs.readFile(path, "utf-8");
    const parsed = JSON.parse(data);
    return {
      enabled: parsed.enabled === true,
      block_patterns: Array.isArray(parsed.block_patterns) ? parsed.block_patterns : [],
      allow_patterns: Array.isArray(parsed.allow_patterns) ? parsed.allow_patterns : [],
      blacklist: Array.isArray(parsed.blacklist) ? parsed.blacklist : undefined,
      whitelist: Array.isArray(parsed.whitelist) ? parsed.whitelist : undefined,
    };
  } catch (e) {
    // File missing or malformed → return default (disabled)
    return { enabled: false, block_patterns: [], allow_patterns: [] };
  }
}

// ============================================================================
// Extension entry point
// ============================================================================

export default function awpExtension(pi: ExtensionAPI): void {
  // Phase 4 cross-validation: hot-reload config. Reads on every
  // event, but uses an in-memory cache + mtime check to avoid
  // hitting disk on each tool call. User edits interception.json
  // → next tool call sees the new rules.
  let configCache: { mtimeMs: number; config: InterceptionConfig } | null = null;

  const getConfig = async (): Promise<InterceptionConfig> => {
    const path = process.env[CONFIG_PATH_ENV] || defaultConfigPath();
    try {
      const fs = await import("fs/promises");
      const stat = await fs.stat(path);
      if (configCache && configCache.mtimeMs === stat.mtimeMs) {
        return configCache.config;
      }
      const data = await fs.readFile(path, "utf-8");
      const parsed = JSON.parse(data);
      const config: InterceptionConfig = {
        enabled: parsed.enabled === true,
        block_patterns: Array.isArray(parsed.block_patterns) ? parsed.block_patterns : [],
        allow_patterns: Array.isArray(parsed.allow_patterns) ? parsed.allow_patterns : [],
        blacklist: Array.isArray(parsed.blacklist) ? parsed.blacklist : undefined,
        whitelist: Array.isArray(parsed.whitelist) ? parsed.whitelist : undefined,
      };
      configCache = { mtimeMs: stat.mtimeMs, config };
      return config;
    } catch (e) {
      // File missing or malformed → return default (disabled)
      return { enabled: false, block_patterns: [], allow_patterns: [] };
    }
  };

  // Invalidate cache on session start (config may have changed)
  pi.on("session_start", async () => {
    configCache = null;
    const cfg = await getConfig();
    if (cfg.enabled) {
      // Log to pi's stderr; user can see in --debug
      console.error(
        `[awp] interception enabled: ${cfg.block_patterns.length} block, ${cfg.allow_patterns.length} allow rules`
      );
    }
  });

  // ------------------------------------------------------------------
  // tool_call — block/allow based on rules, or ask user
  // Spec §6.5: allow_patterns checked FIRST (if matches, no interception).
  // ------------------------------------------------------------------
  pi.on("tool_call", async (event, ctx) => {
    const cfg = await getConfig();
    if (!cfg.enabled) return; // let pi handle it natively

    const toolName = event.toolName;
    const cmd = extractCommand(toolName, event.input);

    // Spec: allow_patterns is checked first
    if (isAllowed(cfg, cmd)) {
      return; // allow silently
    }
    if (isBlocked(cfg, cmd)) {
      return { block: true, reason: `Blocked by awp block_patterns: ${cmd}` };
    }
    // Neither: ask user
    const { title, body } = formatToolCallMessage(toolName, event.input);
    const confirmed = await ctx.ui.confirm(title, body);
    if (!confirmed) {
      return { block: true, reason: "Denied by user" };
    }
    return; // allow
  });

  // ------------------------------------------------------------------
  // user_bash — user typed !! <command>; same logic
  // ------------------------------------------------------------------
  pi.on("user_bash", async (event, ctx) => {
    const cfg = await getConfig();
    if (!cfg.enabled) return;
    const cmd = event.command || "";
    if (isAllowed(cfg, cmd)) {
      return;
    }
    if (isBlocked(cfg, cmd)) {
      return { block: true, reason: `Blocked by awp block_patterns: ${cmd}` };
    }
    const confirmed = await ctx.ui.confirm(
      "Run user command?",
      `Pi wants to run: ${cmd}`
    );
    if (!confirmed) {
      return { block: true };
    }
    return;
  });

  // ------------------------------------------------------------------
  // input — extension needs user-typed string
  // ------------------------------------------------------------------
  pi.on("input", async (event, ctx) => {
    const cfg = await getConfig();
    if (!cfg.enabled) {
      // Fall through to pi's default input handling
      return;
    }
    const value = await ctx.ui.input(event.prompt || "Input:", "");
    return { value };
  });

  // ------------------------------------------------------------------
  // confirm — extension needs a yes/no from the user
  // ------------------------------------------------------------------
  pi.on("confirm", async (event, ctx) => {
    const cfg = await getConfig();
    if (!cfg.enabled) {
      return;
    }
    const confirmed = await ctx.ui.confirm(
      event.title || "Confirm?",
      event.message || ""
    );
    return { confirmed };
  });
}

// ============================================================================
// Helper: extract command string from a tool call input
// ============================================================================

function extractCommand(toolName: string, input: unknown): string {
  if (!input || typeof input !== "object") return "";
  const obj = input as Record<string, unknown>;
  switch (toolName) {
    case "bash":
      return typeof obj.command === "string" ? obj.command : "";
    case "read":
    case "write":
    case "edit":
      return typeof obj.path === "string" ? obj.path : "";
    case "find":
    case "grep":
      return typeof obj.pattern === "string" ? obj.pattern : "";
    case "ls":
      return typeof obj.path === "string" ? obj.path : "";
    default:
      // Unknown tool: stringify the input as the "command"
      try {
        return JSON.stringify(input);
      } catch {
        return "";
      }
  }
}
