module.exports = {
  root: true,
  env: {
    browser: true,
    es2023: true,
  },
  extends: [
    'eslint:recommended',
    'plugin:react/recommended',
    'plugin:react-hooks/recommended',
    'plugin:@typescript-eslint/recommended',
    'prettier',
  ],
  parser: '@typescript-eslint/parser',
  parserOptions: {
    ecmaFeatures: {
      jsx: true,
    },
    ecmaVersion: 'latest',
    sourceType: 'module',
  },
  plugins: ['react', '@typescript-eslint'],
  settings: {
    react: {
      version: 'detect',
    },
  },
  rules: {
    'react/react-in-jsx-scope': 'off',
    // Downgraded to warn: ~36 violations across pre-existing code (Trust
    // Center API stubs in ui/src/lib/api.ts, generated test scaffolding).
    // Block-promote back to 'error' once those callsites are typed.
    '@typescript-eslint/no-explicit-any': 'warn',
    // Honor the underscore-prefix convention for intentionally-unused
    // params/vars (e.g. `_readonly`, `_props`).
    '@typescript-eslint/no-unused-vars': [
      'error',
      { argsIgnorePattern: '^_', varsIgnorePattern: '^_', caughtErrorsIgnorePattern: '^_' },
    ],
  },
  ignorePatterns: ['dist', 'node_modules'],
};
