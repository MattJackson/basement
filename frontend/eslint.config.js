import js from '@eslint/js'
import globals from 'globals'
import reactHooks from 'eslint-plugin-react-hooks'
import reactRefresh from 'eslint-plugin-react-refresh'
import tseslint from 'typescript-eslint'
import { defineConfig, globalIgnores } from 'eslint/config'

export default defineConfig([
  globalIgnores(['dist', 'src/routeTree.gen.ts', 'src/shared/api/types.gen.ts']),
  {
    files: ['**/*.{ts,tsx}'],
    extends: [
      js.configs.recommended,
      tseslint.configs.recommended,
      reactHooks.configs.flat.recommended,
      reactRefresh.configs.vite,
    ],
    languageOptions: {
      globals: globals.browser,
    },
    rules: {
      // Honor the `_` prefix as "intentionally unused" (canonical
      // TypeScript convention).
      '@typescript-eslint/no-unused-vars': ['error', {
        argsIgnorePattern: '^_',
        varsIgnorePattern: '^_',
        caughtErrorsIgnorePattern: '^_',
      }],
      // TanStack Router file-based routes legitimately export
      // `Route` (a non-component value) alongside the component.
      // react-refresh's "only-export-components" rule is too strict
      // for this convention.
      'react-refresh/only-export-components': 'off',
    },
  },
  {
    // shadcn UI primitives ship with mild lint debt (mostly empty
    // prop interfaces extending Radix). Don't bikeshed their source.
    files: ['src/components/ui/**/*.{ts,tsx}'],
    rules: {
      '@typescript-eslint/no-empty-object-type': 'off',
    },
  },
  {
    // Same allowance for layout shells whose Props extends a base
    // type and adds nothing yet.
    files: ['src/shared/layout/**/*.{ts,tsx}', 'src/routes/**/*.{ts,tsx}'],
    rules: {
      '@typescript-eslint/no-empty-object-type': 'off',
    },
  },
])
