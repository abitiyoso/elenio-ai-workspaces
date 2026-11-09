import js from "@eslint/js";
import globals from "globals";
import reactHooks from "eslint-plugin-react-hooks";
import reactRefresh from "eslint-plugin-react-refresh";
import tseslint from "typescript-eslint";

export default tseslint.config(
  { ignores: ["dist", "node_modules"] },
  {
    files: ["**/*.{ts,tsx}"],
    extends: [js.configs.recommended, ...tseslint.configs.strict],
    languageOptions: { ecmaVersion: 2022, globals: globals.browser },
    plugins: { "react-hooks": reactHooks, "react-refresh": reactRefresh },
    rules: {
      ...reactHooks.configs.recommended.rules,
      "react-refresh/only-export-components": ["warn", { allowConstantExport: true }],
      // Identity and case state never live in browser storage; harmless UI prefs go through src/app/prefs.ts only.
      "no-restricted-globals": ["error", { name: "localStorage", message: "Use src/app/prefs.ts (UI prefs only)." },
        { name: "sessionStorage", message: "Do not persist identity or case state in sessionStorage." }],
      "no-restricted-properties": ["error", { object: "window", property: "localStorage", message: "Use src/app/prefs.ts (UI prefs only)." },
        { object: "window", property: "sessionStorage", message: "Do not persist identity or case state in sessionStorage." }],
      "no-restricted-syntax": ["error", { selector: "JSXAttribute[name.name='dangerouslySetInnerHTML']", message: "Never inject HTML; render text." }],
    },
  },
  {
    // The single, try/catch-wrapped home of harmless UI preferences.
    files: ["src/app/prefs.ts"],
    rules: { "no-restricted-properties": "off" },
  },
  {
    files: ["src/test/**", "**/*.test.{ts,tsx}"],
    languageOptions: { globals: { ...globals.browser, ...globals.node } },
    rules: { "react-refresh/only-export-components": "off" },
  },
);
