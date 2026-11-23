// Из Lighthouse-отчёта делает два компактных файла для агента:
//   <out>/metrics.json   — категории + значения ключевых аудитов;
//   <out>/candidates.json — предварительные находки по порогам.
// Агент (workflow lighthouse-audit) на их основе выносит финальный вердикт;
// report.json целиком в контекст агента не отдаётся.

import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";

const [reportPath, outDir] = process.argv.slice(2);
if (!reportPath || !outDir) {
  console.error("usage: lighthouse-extract.mjs <report.json> <out-dir>");
  process.exit(1);
}

const CATEGORY_MIN = 0.9;
// id: {ms: порог} для numericValue, {savingsMs: порог} для opportunities,
// {num: порог} для безразмерных (CLS)
const RULES = {
  "server-response-time": { ms: 800 },
  "first-contentful-paint": { ms: 2000 },
  "largest-contentful-paint": { ms: 2500 },
  "total-blocking-time": { ms: 300 },
  "cumulative-layout-shift": { num: 0.1 },
  "interactive": { ms: 3800 },
  "render-blocking-resources": { savingsMs: 100 },
  "unused-css-rules": { savingsMs: 200 },
  "unused-javascript": { savingsMs: 200 },
};

const report = JSON.parse(readFileSync(reportPath, "utf8"));
const audits = report.audits ?? {};

const metrics = {
  generatedBy: report.lighthouseVersion,
  url: report.finalDisplayedUrl,
  fetchTime: report.fetchTime,
  categories: Object.fromEntries(
    Object.entries(report.categories ?? {}).map(([k, v]) => [k, v.score]),
  ),
  audits: Object.values(audits)
    .filter((a) => a && RULES[a.id])
    .map((a) => ({
      id: a.id,
      title: a.title,
      score: a.score,
      numericValue: a.numericValue ?? a.details?.overallSavingsMs ?? null,
      displayValue: a.displayValue,
      overallSavingsMs: a.details?.overallSavingsMs ?? null,
    })),
};

const findings = [];
for (const a of metrics.audits) {
  const rule = RULES[a.id];
  const v = a.overallSavingsMs ?? a.numericValue;
  if (v == null) continue;
  const limit = rule.savingsMs ?? rule.ms ?? rule.num;
  if (v <= limit) continue;
  findings.push({
    fingerprint: `lh:${a.id}`,
    auditId: a.id,
    title: a.title,
    severity: a.score != null && a.score < 0.5 ? "critical" : a.score != null && a.score < 0.9 ? "major" : "minor",
    value: a.displayValue ?? String(v),
    threshold: rule.savingsMs != null ? `экономия ≤ ${limit} ms` : `≤ ${limit}${rule.num != null ? "" : " ms"}`,
    rationale: "значение хуже порога воркфлоу",
  });
}
for (const [key, score] of Object.entries(metrics.categories)) {
  if (score == null || score >= CATEGORY_MIN) continue;
  findings.push({
    fingerprint: `lh:category:${key}`,
    auditId: `category:${key}`,
    title: `Категория ${key}: ${Math.round(score * 100)} из 100`,
    severity: score < 0.5 ? "critical" : "major",
    value: `${Math.round(score * 100)}/100`,
    threshold: `≥ ${CATEGORY_MIN * 100}/100`,
    rationale: "итоговый балл категории ниже минимума",
  });
}

mkdirSync(outDir, { recursive: true });
writeFileSync(join(outDir, "metrics.json"), JSON.stringify(metrics, null, 2));
writeFileSync(join(outDir, "candidates.json"), JSON.stringify({ findings }, null, 2));
console.log(`metrics: ${metrics.audits.length} аудитов, candidates: ${findings.length} находок`);
