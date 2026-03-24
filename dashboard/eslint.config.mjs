import tsParser from "@typescript-eslint/parser";
import jsxA11y from "eslint-plugin-jsx-a11y";

export default [
  {
    ignores: ["node_modules/", "dist/", "*.config.*"],
  },
  {
    files: ["src/**/*.{ts,tsx}"],
    plugins: {
      "jsx-a11y": jsxA11y,
    },
    languageOptions: {
      parser: tsParser,
      parserOptions: {
        ecmaFeatures: { jsx: true },
        ecmaVersion: "latest",
        sourceType: "module",
      },
    },
    rules: {
      // Critical: every interactive element needs an accessible name.
      "jsx-a11y/alt-text": "warn",
      "jsx-a11y/aria-props": "error",
      "jsx-a11y/aria-role": "error",
      "jsx-a11y/aria-unsupported-elements": "error",
      "jsx-a11y/role-has-required-aria-props": "error",
      "jsx-a11y/role-supports-aria-props": "error",
      "jsx-a11y/tabindex-no-positive": "warn",

      // Relaxed: too many warnings for first pass. Enable incrementally.
      "jsx-a11y/anchor-is-valid": "off",
      "jsx-a11y/click-events-have-key-events": "off",
      "jsx-a11y/no-static-element-interactions": "off",
      "jsx-a11y/no-noninteractive-element-interactions": "off",
    },
  },
];
