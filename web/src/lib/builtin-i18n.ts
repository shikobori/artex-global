import { useMemo } from "react";

import { useTranslations } from "next-intl";

import englishToolSources from "./builtin-tool-sources.en.json";
import toolSources from "./builtin-tool-sources.json";

// Built-in blocking-rule names, asset-rule notes, and tool descriptions are database seed values.
// Translate their display using builtinRules, builtinAssetNotes, and builtinTools in web/messages/*.json.
// Preserve values that differ from the original seeds, including user edits and changed descriptions.
// Stored tool descriptions are model instructions, so do not put translations into edit forms.
// Use t.raw to bypass ICU parsing because the source text contains braces.

// Accept current English seeds and legacy database values without translating user edits.
const RULE_KEYS: Record<string, string> = {
  "[Built-in] Recursive forced deletion rm -rf": "rmRf",
  "[Built-in] Delete critical system directories": "rmSystemDirs",
  "[Built-in] Disk formatting mkfs": "mkfs",
  "[Built-in] Overwrite disk devices with dd": "ddOverwrite",
  "[Built-in] Fork bomb": "forkBomb",
  "[Built-in] Shutdown / reboot": "shutdownReboot",
  "[Built-in] Kill all processes": "killAll",
  "[Built-in] Disk erasure shred / wipe": "shredWipe",
  "[Built-in] Flush firewall rules": "flushFirewall",
  "[Built-in] SQL DROP DATABASE / TABLE / SCHEMA": "sqlDrop",
  "[Built-in] SQL TRUNCATE": "sqlTruncate",
  "[Built-in] MongoDB drop / dropDatabase": "mongoDrop",
  "[Built-in] Redis FLUSHALL / FLUSHDB": "redisFlush",
  "[Built-in] curl / wget DELETE requests": "curlWgetDelete",
  "[Built-in] Python HTTP client DELETE (requests/httpx/aiohttp)": "pythonHttpDelete",
  "[Built-in] HTTP DELETE declarations in scripts (JS/general)": "scriptHttpDelete",
  "[Built-in] Bulk clear / purge endpoint paths": "bulkClearPath",
  "[Built-in] Destructive system commands": "destructiveCommand",
  "[Built-in] Data exfiltration pipeline": "exfilPipe",
  "[Built-in] Delete endpoint paths": "deletePath",
  "[内置] 递归强制删除 rm -rf": "rmRf",
  "[内置] 删除系统关键目录": "rmSystemDirs",
  "[内置] 磁盘格式化 mkfs": "mkfs",
  "[内置] 覆写磁盘设备 dd": "ddOverwrite",
  "[内置] Fork 炸弹": "forkBomb",
  "[内置] 关机 / 重启": "shutdownReboot",
  "[内置] 杀死全部进程": "killAll",
  "[内置] 磁盘擦除 shred / wipe": "shredWipe",
  "[内置] 清空防火墙规则": "flushFirewall",
  "[内置] SQL DROP DATABASE / TABLE / SCHEMA": "sqlDrop",
  "[内置] SQL TRUNCATE": "sqlTruncate",
  "[内置] MongoDB drop / dropDatabase": "mongoDrop",
  "[内置] Redis FLUSHALL / FLUSHDB": "redisFlush",
  "[内置] curl / wget 发送 DELETE 请求": "curlWgetDelete",
  "[内置] Python HTTP 客户端 DELETE（requests/httpx/aiohttp）": "pythonHttpDelete",
  "[内置] 脚本中声明 HTTP DELETE 方法（JS/通用）": "scriptHttpDelete",
  "[内置] 批量清空 / 清除接口路径": "bulkClearPath",
  "[内置] 破坏性系统命令": "destructiveCommand",
  "[内置] 数据外泄管道": "exfilPipe",
  "[内置] 删除类接口路径": "deletePath",
};

const ASSET_NOTE_KEYS: Record<string, string> = {
  "[Built-in] Government sites (.gov)": "govGlobal",
  "[Built-in] Government sites (.gov.cn)": "govCn",
  "[Built-in] Education sites (.edu)": "eduGlobal",
  "[Built-in] Education sites (.edu.cn)": "eduCn",
  "[内置] 政府网站 (.gov)": "govGlobal",
  "[内置] 政府网站 (.gov.cn)": "govCn",
  "[内置] 教育网站 (.edu)": "eduGlobal",
  "[内置] 教育网站 (.edu.cn)": "eduCn",
};

// English snapshot from agent.BuiltinToolSeeds() and server/traffic tool definitions.
// Retain the legacy source map for existing installs.
const TOOL_SOURCES: Record<string, string>[] = [englishToolSources, toolSources];

// Current English and legacy Chinese headings from the report-agent prompt in agent/promptcatalog.go.
// Translate only for display; retain stored reports and copied/exported text unchanged. Do not translate PoC.
const REPORT_HEADING_KEYS: Record<string, string> = {
  Overview: "overview",
  Impact: "impact",
  "Affected scope": "scope",
  "Reproduction steps": "reproduce",
  Evidence: "evidence",
  "Root cause": "rootCause",
  Remediation: "remediation",
  概述: "overview",
  影响与危害: "impact",
  受影响范围: "scope",
  复现步骤: "reproduce",
  证据: "evidence",
  根因分析: "rootCause",
  修复建议: "remediation",
};

export function useBuiltinLabels() {
  const tRule = useTranslations("builtinRules");
  const tNote = useTranslations("builtinAssetNotes");
  const tTool = useTranslations("builtinTools");
  const tHeading = useTranslations("builtinReportHeadings");

  return useMemo(() => {
    const ruleName = (name: string): string => {
      const key = RULE_KEYS[name];
      return key && tRule.has(key) ? (tRule.raw(key) as string) : name;
    };

    const assetNote = (note: string): string => {
      const key = ASSET_NOTE_KEYS[note];
      return key && tNote.has(key) ? (tNote.raw(key) as string) : note;
    };

    const toolDescription = (tool: { key: string; system: boolean; description: string }): string => {
      if (
        tool.system &&
        TOOL_SOURCES.some((sources) => sources[tool.key] === tool.description) &&
        tTool.has(tool.key)
      ) {
        return tTool.raw(tool.key) as string;
      }
      return tool.description;
    };

    // Preserve code-block lines as original evidence.
    const reportHeadings = (markdown: string): string => {
      let inFence = false;
      return markdown
        .split("\n")
        .map((line) => {
          if (/^\s*(```|~~~)/.test(line)) inFence = !inFence;
          if (inFence) return line;
          const match = /^(#{1,6}\s+)(.+?)\s*$/.exec(line);
          const key = match && REPORT_HEADING_KEYS[match[2]];
          return match && key ? match[1] + (tHeading.raw(key) as string) : line;
        })
        .join("\n");
    };

    return { ruleName, assetNote, toolDescription, reportHeadings };
  }, [tRule, tNote, tTool, tHeading]);
}
