import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  // routegate.org is an apex custom domain, so all production assets resolve
  // from the domain root rather than a repository subpath.
  base: '/',
})
