import { ref, watchEffect } from 'vue'
import { initialLang, initialTheme } from '../lib'
import type { Theme } from '../lib'
import { translator } from '../i18n'
import type { Key, Lang } from '../i18n'

export const lang = ref<Lang>(initialLang(localStorage.getItem('lang'), navigator.language))
export const theme = ref<Theme>(initialTheme(localStorage.getItem('theme'), matchMedia('(prefers-color-scheme: dark)').matches))
// Reads lang.value, so anything rendered with it follows a language switch.
export const t = (key: Key, params?: Record<string, string | number>) => translator(lang.value)(key, params)

watchEffect(() => {
  document.documentElement.lang = lang.value
  document.documentElement.dataset.theme = theme.value
  localStorage.setItem('lang', lang.value)
  localStorage.setItem('theme', theme.value)
})
