/** @type {import('tailwindcss').Config} */
module.exports = {
  content: ['./app/**/*.{js,ts,jsx,tsx,mdx}', './components/**/*.{js,ts,jsx,tsx,mdx}'],
  darkMode: 'class',
  theme: {
    extend: {
      colors: {
        // Map severity → tailwind palette so dashboards can color rows.
        severity: {
          critical: '#dc2626',
          warning: '#f59e0b',
          info: '#3b82f6',
        },
        status: {
          open: '#dc2626',
          acknowledged: '#f59e0b',
          suppressed: '#6b7280',
          maintenance: '#8b5cf6',
          resolved: '#10b981',
        },
      },
    },
  },
  plugins: [],
};