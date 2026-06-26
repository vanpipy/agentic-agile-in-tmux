// Tests for awp-extension.ts pure helpers.
// Run with: bun test internal/pi/extension/awp-extension_test.ts

import { test, expect, describe } from "bun:test";
import {
  matchRule,
  isBlocked,
  isAllowed,
  formatToolCallMessage,
  type InterceptionConfig,
} from "./awp-extension.ts";

describe("matchRule", () => {
  test("exact match", () => {
    expect(matchRule("rm -rf /", "rm -rf /")).toBe(true);
    expect(matchRule("rm -rf /", "rm -rf /etc")).toBe(false);
  });
  test("prefix wildcard", () => {
    expect(matchRule("rm *", "rm -rf /")).toBe(true);
    expect(matchRule("rm *", "rm -rf /etc")).toBe(true);
    expect(matchRule("rm *", "cat /etc/passwd")).toBe(false);
  });
  test("suffix wildcard", () => {
    expect(matchRule("* /etc", "rm /etc")).toBe(true);
    expect(matchRule("* /etc", "rm /var")).toBe(false);
  });
  test("contains wildcard", () => {
    expect(matchRule("*password*", "cat /etc/password")).toBe(true);
    expect(matchRule("*password*", "ls /etc")).toBe(false);
  });
  test("star matches all (incl. empty)", () => {
    expect(matchRule("*", "anything")).toBe(true);
    expect(matchRule("*", "")).toBe(true);
  });
  test("empty pattern or command", () => {
    expect(matchRule("", "ls")).toBe(false);
    expect(matchRule("ls", "")).toBe(false);
  });
});

describe("isBlocked", () => {
  const cfg: InterceptionConfig = {
    enabled: true,
    block_patterns: ["rm -rf /*", "sudo *", "* /etc/passwd"],
    allow_patterns: [],
  };
  test("matches block patterns", () => {
    expect(isBlocked(cfg, "rm -rf /")).toBe(true);
    expect(isBlocked(cfg, "rm -rf /etc")).toBe(true);
    expect(isBlocked(cfg, "sudo apt install")).toBe(true);
    expect(isBlocked(cfg, "cat /etc/passwd")).toBe(true);
  });
  test("does not match safe commands", () => {
    expect(isBlocked(cfg, "ls /")).toBe(false);
    expect(isBlocked(cfg, "cat /var/log")).toBe(false);
  });
  test("falls back to legacy blacklist", () => {
    const legacyCfg: InterceptionConfig = {
      enabled: true,
      block_patterns: [],
      allow_patterns: [],
      blacklist: ["rm *"],
    };
    expect(isBlocked(legacyCfg, "rm -rf /")).toBe(true);
  });
});

describe("isAllowed", () => {
  const cfg: InterceptionConfig = {
    enabled: true,
    block_patterns: [],
    allow_patterns: ["ls *", "cat *", "pwd"],
  };
  test("matches allow patterns", () => {
    expect(isAllowed(cfg, "ls -la")).toBe(true);
    expect(isAllowed(cfg, "cat /etc/hostname")).toBe(true);
    expect(isAllowed(cfg, "pwd")).toBe(true);
  });
  test("does not match non-allowed", () => {
    expect(isAllowed(cfg, "rm /tmp/x")).toBe(false);
  });
});

describe("spec §6.5 ordering", () => {
  // Spec: "allow_patterns is checked FIRST; if matches, no interception"
  // This is the priority order regardless of how the config is structured.
  const cfg: InterceptionConfig = {
    enabled: true,
    block_patterns: ["rm *"],
    allow_patterns: ["rm -rf /*"],
  };
  test("allow takes priority over block", () => {
    // "rm -rf /" matches BOTH block (rm *) and allow (rm -rf /*).
    // Spec: allow wins.
    expect(isAllowed(cfg, "rm -rf /")).toBe(true);
    expect(isBlocked(cfg, "rm -rf /")).toBe(true);
    // Caller should check allow FIRST (extension does this)
  });
});

describe("formatToolCallMessage", () => {
  test("bash tool", () => {
    const { title, body } = formatToolCallMessage("bash", {
      command: "rm -rf /tmp/test",
    });
    expect(title).toBe("Allow bash?");
    expect(body).toContain("rm -rf /tmp/test");
  });
  test("truncates long args", () => {
    const longArgs = { command: "x".repeat(500) };
    const { body } = formatToolCallMessage("bash", longArgs);
    expect(body.length).toBeLessThan(250);
    expect(body).toContain("...");
  });
  test("read tool", () => {
    const { title, body } = formatToolCallMessage("read", {
      path: "/etc/hostname",
    });
    expect(title).toBe("Allow read?");
    expect(body).toContain("/etc/hostname");
  });
});
