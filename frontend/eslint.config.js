import pluginVue from 'eslint-plugin-vue'
import vueTsEslintConfig from '@vue/eslint-config-typescript'
import skipFormatting from '@vue/eslint-config-prettier/skip-formatting'

export default [
  { name: 'app/files-to-lint', files: ['**/*.{ts,mts,tsx,vue}'] },
  {
    name: 'app/files-to-ignore',
    ignores: ['**/dist/**', '**/coverage/**', 'src/shared/api/schema.d.ts'],
  },
  ...pluginVue.configs['flat/essential'],
  ...vueTsEslintConfig(),
  skipFormatting,
  {
    rules: {
      // Seguridad: nunca HTML sin sanear (docs/phase-1/08 §1).
      'vue/no-v-html': 'error',
    },
  },
]
