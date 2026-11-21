// ESLint (flat config) для фронтенда: JS/TS recommended + правила
// typescript-eslint. Сгенерированный SDK-файл не линтим — он артефакт codegen.
import js from "@eslint/js";
import tseslint from "typescript-eslint";

export default tseslint.config(
  { ignores: ["dist/", "node_modules/", "src/api/schema.d.ts"] },
  js.configs.recommended,
  ...tseslint.configs.recommended,
  {
    rules: {
      // Ненулевые утверждения (querySelector(...)! ) — принятый стиль пилота.
      "@typescript-eslint/no-non-null-assertion": "off",
      "@typescript-eslint/no-unused-vars": ["error", { argsIgnorePattern: "^_" }],
    },
  },
  {
    // Конфиги vite/eslint сами живут в Node-окружении.
    files: ["**/*.config.{js,ts}", "eslint.config.js"],
    languageOptions: { globals: { process: "readonly", console: "readonly" } },
  },
);
