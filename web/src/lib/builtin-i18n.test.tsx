import { NextIntlClientProvider } from "next-intl";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";

import ko from "../../messages/ko.json";
import { useAgentLabels } from "./agent-i18n";
import { useBuiltinLabels } from "./builtin-i18n";
import englishToolSources from "./builtin-tool-sources.en.json";
import toolSources from "./builtin-tool-sources.json";

function labelsOf<T>(useLabels: () => T): T {
  let out: T | undefined;
  const Probe = () => {
    out = useLabels();
    return null;
  };
  renderToStaticMarkup(
    <NextIntlClientProvider locale="ko" messages={ko}>
      <Probe />
    </NextIntlClientProvider>,
  );
  return out as T;
}

describe("useBuiltinLabels", () => {
  const labels = labelsOf(useBuiltinLabels);

  it("translates rule names and notes matching seed text", () => {
    expect(labels.ruleName("[内置] 磁盘格式化 mkfs")).toBe(ko.builtinRules.mkfs);
    expect(labels.ruleName("[Built-in] Disk formatting mkfs")).toBe(ko.builtinRules.mkfs);
    expect(labels.assetNote("[内置] 政府网站 (.gov)")).toBe(ko.builtinAssetNotes.govGlobal);
    expect(labels.assetNote("[Built-in] Government sites (.gov)")).toBe(ko.builtinAssetNotes.govGlobal);
  });

  it("preserves user-edited values", () => {
    expect(labels.ruleName("내 규칙")).toBe("내 규칙");
    expect(labels.assetNote("내 메모")).toBe("내 메모");
    expect(labels.toolDescription({ key: "add_hint", system: true, description: "수정함" })).toBe("수정함");
    expect(labels.ruleName("[Built-in] Disk formatting mkfs (custom)")).toBe(
      "[Built-in] Disk formatting mkfs (custom)",
    );
    expect(labels.assetNote("[Built-in] Government sites (.gov) - custom")).toBe(
      "[Built-in] Government sites (.gov) - custom",
    );
    const edited = `${englishToolSources.add_hint} Custom instructions.`;
    expect(labels.toolDescription({ key: "add_hint", system: true, description: edited })).toBe(edited);
  });

  it("translates only report headings and preserves body text and code blocks", () => {
    const report = "## 概述\n概述 본문\n\n## PoC\n```text\n## 证据\n```\n\n## 修复建议\nfix";
    expect(labels.reportHeadings(report)).toBe(
      `## ${ko.builtinReportHeadings.overview}\n概述 본문\n\n## PoC\n\`\`\`text\n## 证据\n\`\`\`\n\n## ${ko.builtinReportHeadings.remediation}\nfix`,
    );
  });

  it("translates English report headings without changing evidence or custom headings", () => {
    const report =
      "## Overview\nEvidence in the body\n\n## PoC\n```text\n## Evidence\n```\n\n## Root cause\nAnalysis\n\n## Custom heading";
    expect(labels.reportHeadings(report)).toBe(
      `## ${ko.builtinReportHeadings.overview}\nEvidence in the body\n\n## PoC\n\`\`\`text\n## Evidence\n\`\`\`\n\n## ${ko.builtinReportHeadings.rootCause}\nAnalysis\n\n## Custom heading`,
    );
  });

  it("does not translate custom tools even when their descriptions match either seed generation", () => {
    for (const description of [toolSources.add_hint, englishToolSources.add_hint]) {
      expect(labels.toolDescription({ key: "add_hint", system: false, description })).toBe(description);
    }
  });

  it("translates both generations of every system tool description, including text with braces", () => {
    expect(Object.keys(englishToolSources).sort()).toEqual(Object.keys(toolSources).sort());
    for (const sources of [toolSources, englishToolSources]) {
      for (const [key, description] of Object.entries(sources)) {
        const translated = labels.toolDescription({ key, system: true, description });
        expect(translated).toBe(ko.builtinTools[key as keyof typeof ko.builtinTools]);
      }
    }
  });
});

describe("useAgentLabels reporter", () => {
  const labels = labelsOf(useAgentLabels);

  it("looks up translations only for seed values", () => {
    expect(labels.name({ key: "reporter", name: "보고서 작성" })).toBe(ko.builtinAgents.reporter.name);
    expect(labels.name({ key: "reporter", name: "Report writer" })).toBe(ko.builtinAgents.reporter.name);
    for (const description of [
      "Write detailed vulnerability reports: automatically triggered when a vulnerability is found, reads the evidence and execution trace, then writes a Markdown report to the vulnerability record.",
      "취약점 상세 보고서 작성: 취약점을 발견하면 자동으로 트리거되어, 증거와 실행 과정을 조회한 뒤 Markdown 보고서를 작성해 해당 취약점에 기록합니다.",
    ]) {
      expect(labels.description({ key: "reporter", description })).toBe(ko.builtinAgents.reporter.description);
      const edited = `${description} Custom instructions.`;
      expect(labels.description({ key: "reporter", description: edited })).toBe(edited);
    }
    expect(labels.name({ key: "reporter", name: "Report writer (custom)" })).toBe("Report writer (custom)");
    expect(labels.name({ key: "custom", name: "Report writer" })).toBe("Report writer");
    expect(labels.name({ key: "reporter", name: "내 보고서" })).toBe("내 보고서");
    expect(labels.description({ key: "reporter", description: "내 설명" })).toBe("내 설명");
  });
});
