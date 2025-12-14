/** @type {import('tailwindcss').Config} */
export default {
  content: [
    "./index.html",
    "./src/**/*.{vue,js,ts,jsx,tsx}",
  ],
  darkMode: 'class',
  theme: {
    extend: {
      colors: {
        claude: {
          50: '#fef7ee',
          100: '#fdedd3',
          200: '#fad7a6',
          300: '#f6b96e',
          400: '#f19235',
          500: '#ee7410',
          600: '#df5a07',
          700: '#b94109',
          800: '#93350e',
          900: '#772d0f',
        }
      },
      animation: {
        'pulse-slow': 'pulse 2s infinite',
        'fade-in': 'fadeIn 0.3s ease-in',
      },
      keyframes: {
        fadeIn: {
          from: { opacity: '0', transform: 'translateY(-10px)' },
          to: { opacity: '1', transform: 'translateY(0)' },
        }
      }
    }
  },
  plugins: [],
}
