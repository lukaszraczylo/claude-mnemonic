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
    "vis-data": "^8.0.4",
    "vis-network": "^10.1.0",
    "vue": "^3.5.34"
  },
  "devDependencies": {
    "@fortawesome/fontawesome-free": "^7.2.0",
    "@tailwindcss/postcss": "^4.3.0",
    "@types/node": "^25.9.1",
    "@vitejs/plugin-vue": "^6.0.7",
    "postcss": "^8.5.15",
    "tailwindcss": "^4.3.0",
    "typescript": "~6.0.3",
    "vite": "^8.0.14",
    "vue-tsc": "^3.3.1"
  }
}
