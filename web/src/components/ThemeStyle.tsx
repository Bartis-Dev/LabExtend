import { useEffect } from 'react';
import { useTheme } from '@/store/theme';

// ThemeStyle injects the active theme's palette as CSS variables on :root
// followed by the theme's custom_css. Updating the store triggers a
// re-render that rewrites the <style> tag, giving a live preview.
export function ThemeStyle() {
  const active = useTheme((s) => s.active);

  useEffect(() => {
    const id = 'active-theme';
    let style = document.getElementById(id) as HTMLStyleElement | null;
    if (!style) {
      style = document.createElement('style');
      style.id = id;
      document.head.appendChild(style);
    }
    if (!active) {
      style.textContent = '';
      return;
    }
    const vars = Object.entries(active.palette)
      .map(([k, v]) => `  ${k}: ${v};`)
      .join('\n');
    style.textContent = `:root {\n${vars}\n}\n${active.customCss}`;
  }, [active]);

  return null;
}
