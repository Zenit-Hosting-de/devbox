// ESLint flat config for all TypeScript and JavaScript in the repository.

import js from "@eslint/js";
import { defineConfig } from "eslint/config";
import globals from "globals";
import tseslint from "typescript-eslint";

export default defineConfig(
  {
    ignores: [
      "**/dist/**",
      "**/node_modules/**",
      "**/gen/**",
      "graphify-out/**",
      "bin/**",
      "web/public/**",
    ],
  },
  js.configs.recommended,
  tseslint.configs.strict,
  {
    languageOptions: {
      globals: { ...globals.node },
    },
    rules: {
      "@typescript-eslint/no-explicit-any": "error",
    },
  },
  {
    // The web UI runs in the browser and uses zenit-ui only.
    files: ["web/src/**/*.ts"],
    languageOptions: {
      globals: { ...globals.browser },
    },
    rules: {
      "no-restricted-imports": [
        "error",
        {
          patterns: [
            {
              group: ["@angular/material", "@angular/material/*"],
              message: "Angular Material is not allowed; use zenit-ui.",
            },
          ],
        },
      ],
    },
  },
);
