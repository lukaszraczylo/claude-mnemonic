{
  "name": "claude-mnemonic-dashboard",
  "version": "{{ .Version }}",
  "private": true,
  "type": "module",
  "scripts": {
    "dev": "vite",
    "build": "vue-tsc -b && vite build",
    "preview": "vite preview",
    "type-check": "vue-tsc --noEmit"
  },
  "dependencies": {
    "vis-data": "^7.1.9",
    "vis-network": "^9.1.9",
    "vue": "^3.5.13"
  },
  "devDependencies": {
    "@fortawesome/fontawesome-free": "^6.7.2",
    "@types/node": "^22.10.2",
    "@vitejs/plugin-vue": "^5.2.1",
    "autoprefixer": "^10.4.20",
    "postcss": "^8.4.49",
    "tailwindcss": "^3.4.17",
    "typescript": "~5.7.2",
    "vite": "^6.0.5",
    "vue-tsc": "^2.2.0"
  }
}
