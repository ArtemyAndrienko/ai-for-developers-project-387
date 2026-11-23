// Синтетический Lighthouse-отчёт для ДЕМОНСТРАЦИИ учебного сценария.
// Это НЕ боевой замер: числа заведомо провальные, структура совместима с
// lighthouse-extract.mjs. Реальные отчёты в git не попадают (см. .gitignore).

import { writeFileSync } from "node:fs";

const out = process.argv[2] ?? "report.json";

const numeric = (id, title, score, numericValue, displayValue) => ({
  id, title, score, scoreDisplayMode: "numeric", numericValue, displayValue,
});
const opportunity = (id, title, score, overallSavingsMs, displayValue) => ({
  id, title, score, scoreDisplayMode: "numeric", numericValue: overallSavingsMs,
  displayValue, details: { type: "opportunity", overallSavingsMs },
});

const report = {
  lighthouseVersion: "12.0.0-training-fixture",
  requestedUrl: "https://example.invalid/учебный-фикстур",
  finalDisplayedUrl: "https://example.invalid/учебный-фикстур",
  fetchTime: new Date().toISOString(),
  categories: {
    performance: { id: "performance", title: "Performance", score: 0.42 },
    accessibility: { id: "accessibility", title: "Accessibility", score: 1 },
    "best-practices": { id: "best-practices", title: "Best Practices", score: 0.83 },
    seo: { id: "seo", title: "SEO", score: 0.91 },
  },
  audits: {
    "server-response-time": numeric("server-response-time", "Reduce initial server response time", 0.2, 1500, "Root document took 1500 ms"),
    "first-contentful-paint": numeric("first-contentful-paint", "First Contentful Paint", 0.2, 3100, "3.1 s"),
    "largest-contentful-paint": numeric("largest-contentful-paint", "Largest Contentful Paint", 0.15, 5200, "5.2 s"),
    "total-blocking-time": numeric("total-blocking-time", "Total Blocking Time", 0.1, 620, "620 ms"),
    "cumulative-layout-shift": numeric("cumulative-layout-shift", "Cumulative Layout Shift", 0.2, 0.27, "0.27"),
    interactive: numeric("interactive", "Time to Interactive", 0.3, 9000, "9.0 s"),
    "render-blocking-resources": opportunity("render-blocking-resources", "Eliminate render-blocking resources", 0, 900, "Est savings of 900 ms"),
    "unused-css-rules": opportunity("unused-css-rules", "Reduce unused CSS", 0.5, 250, "Est savings of 250 ms"),
    "color-contrast": { id: "color-contrast", title: "Color contrast", score: 1, scoreDisplayMode: "binary" },
    deprecations: { id: "deprecations", title: "Avoids deprecated APIs", score: 0, scoreDisplayMode: "binary" },
  },
};

writeFileSync(out, JSON.stringify(report, null, 2));
console.log(`учебный фикстур записан: ${out}`);
