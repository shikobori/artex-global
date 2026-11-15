// Channel field definitions and configuration parsing helpers.
//
// Keep this separate from the page because it is data, not a view: it describes each channel's fields,
// their controls, and bidirectional conversion between form text and JSON configuration.
// Adding a channel requires changes here without modifying the page itself.
//
// This is a pure data module, not a client component, and cannot call useTranslations.
// Store i18n keys in the notifyPage namespace instead of hardcoded labels. Rendering components
// (page.tsx / channel-form.tsx) resolve them with t(). Keys are ASCII; the upstream Chinese text
// remains in messages/zh.json. Channel display names use notifyPage.kind.<kind>.

// Configuration field definitions for each channel.
//
// Maintain frontend field definitions separately from backend schemas: backend Validate checks required
// fields and formats, while the UI needs layout and control types.
// The only shared contract is secret_keys, supplied by the backend to choose password inputs,
// because each channel implementation knows which values are credentials (the entire WeCom webhook,
// but only a secret within DingTalk). A missing entry for a new channel leaves the form blank,
// with an explicit hasFields warning below rather than a silent failure.
//
// label, help, and options[].label contain i18n keys in the notifyPage namespace;
// placeholder retains literal URLs and examples without translation.
export type FieldKind = "text" | "password" | "number" | "select" | "textarea" | "switch" | "kv" | "list";
export interface FieldDef {
  key: string;
  /** i18n key in the notifyPage namespace */
  label: string;
  kind: FieldKind;
  placeholder?: string;
  /** i18n key in the notifyPage namespace */
  help?: string;
  /** options[].label also contains i18n keys */
  options?: { value: string; label: string }[];
}
export const CHANNEL_FIELDS: Record<string, FieldDef[]> = {
  dingtalk: [
    {
      key: "webhook",
      label: "field.dingtalk.webhook.label",
      kind: "text",
      placeholder: "https://oapi.dingtalk.com/robot/send?access_token=...",
    },
    {
      key: "secret",
      label: "field.dingtalk.secret.label",
      kind: "password",
      help: "field.dingtalk.secret.help",
    },
  ],
  feishu: [
    {
      key: "webhook",
      label: "field.feishu.webhook.label",
      kind: "text",
      placeholder: "https://open.feishu.cn/open-apis/bot/v2/hook/...",
    },
    { key: "secret", label: "field.feishu.secret.label", kind: "password", help: "field.feishu.secret.help" },
  ],
  wecom: [
    {
      key: "webhook",
      label: "field.wecom.webhook.label",
      kind: "text",
      placeholder: "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=...",
    },
  ],
  webhook: [
    {
      key: "url",
      label: "field.webhook.url.label",
      kind: "text",
      placeholder: "https://your-endpoint.example.com/hook",
    },
    {
      key: "method",
      label: "field.webhook.method.label",
      kind: "select",
      options: [
        { value: "POST", label: "field.webhook.method.opt.POST" },
        { value: "PUT", label: "field.webhook.method.opt.PUT" },
        { value: "PATCH", label: "field.webhook.method.opt.PATCH" },
        { value: "GET", label: "field.webhook.method.opt.GET" },
      ],
    },
    { key: "headers", label: "field.webhook.headers.label", kind: "kv", help: "field.webhook.headers.help" },
    {
      key: "body_template",
      label: "field.webhook.body_template.label",
      kind: "textarea",
      help: "field.webhook.body_template.help",
    },
  ],
  telegram: [
    { key: "bot_token", label: "field.telegram.bot_token.label", kind: "password", placeholder: "123456:ABC-DEF..." },
    { key: "chat_id", label: "field.telegram.chat_id.label", kind: "text", placeholder: "-1001234567890" },
    {
      key: "base_url",
      label: "field.telegram.base_url.label",
      kind: "text",
      placeholder: "https://api.telegram.org",
      help: "field.telegram.base_url.help",
    },
  ],
  email: [
    { key: "host", label: "field.email.host.label", kind: "text", placeholder: "smtp.example.com" },
    {
      key: "port",
      label: "field.email.port.label",
      kind: "number",
      placeholder: "587",
      help: "field.email.port.help",
    },
    { key: "username", label: "field.email.username.label", kind: "text" },
    { key: "password", label: "field.email.password.label", kind: "password" },
    { key: "from", label: "field.email.from.label", kind: "text", placeholder: "artex@example.com" },
    { key: "to", label: "field.email.to.label", kind: "list", help: "field.email.to.help" },
    { key: "tls", label: "field.email.tls.label", kind: "switch", help: "field.email.tls.help" },
  ],
};

// value is the min_severity filter value; label is a notifyPage.severityOpt.* i18n key.
export const SEVERITY_OPTIONS = [
  { value: "", label: "severityOpt.all" },
  { value: "low", label: "severityOpt.low" },
  { value: "medium", label: "severityOpt.medium" },
  { value: "high", label: "severityOpt.high" },
  { value: "critical", label: "severityOpt.critical" },
];

export type ChannelForm = {
  name: string;
  kind: string;
  mode: "realtime" | "digest";
  enabled: boolean;
  ratePerMin: string;
  config: Record<string, unknown>;
  minSeverity: string;
  includeText: string;
  excludeText: string;
  taskIDsText: string;
  assetIDsText: string;
  onStatusChange: boolean;
};

export const emptyForm = (kind: string): ChannelForm => ({
  name: "",
  kind,
  mode: "realtime",
  enabled: true,
  ratePerMin: "",
  config: {},
  minSeverity: "",
  includeText: "",
  excludeText: "",
  taskIDsText: "",
  assetIDsText: "",
  onStatusChange: false,
});

// parseKV parses a textarea containing one KEY=VALUE pair per line.
export function parseKV(text: string): Record<string, string> {
  const out: Record<string, string> = {};
  for (const line of text.split("\n")) {
    const t = line.trim();
    if (!t) continue;
    const i = t.indexOf("=");
    if (i > 0) out[t.slice(0, i).trim()] = t.slice(i + 1).trim();
  }
  return out;
}
// parseIDs parses comma/whitespace-separated IDs. Retain fullwidth commas as separators:
// users may paste lists containing them; this is input parsing, not display text.
export function parseIDs(text: string): number[] {
  return text
    .split(/[\s,，]+/)
    .map((s) => s.trim())
    .filter(Boolean)
    .map((s) => Number(s))
    .filter((n) => Number.isFinite(n) && n > 0);
}
// parseKeywords splits keywords by line or comma because vulnerability type names may contain spaces.
// Also retain fullwidth commas as input separators.
export function parseKeywords(text: string): string[] {
  return text
    .split(/[\n,，]+/)
    .map((s) => s.trim())
    .filter(Boolean);
}
