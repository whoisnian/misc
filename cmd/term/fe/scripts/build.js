import { resolve } from 'path'
import { build } from 'esbuild'
import { htmlTemplatePlugin, copyPlugin } from './plugin.js'
import packageJson from '../package.json' with { type: 'json' }

const { version } = packageJson
const isProduction = process.env.NODE_ENV === 'production'

const PATH_ROOT = resolve(import.meta.dirname, '..')
const PATH_OUTPUT = resolve(import.meta.dirname, '../dist')
const fromRoot = (...args) => resolve(PATH_ROOT, ...args)
const fromOutput = (...args) => resolve(PATH_OUTPUT, ...args)

build({
  platform: 'browser',
  bundle: true,
  minify: isProduction,
  define: {
    __PACKAGE_VERSION__: `"${version}"`,
    __DEBUG__: `${!isProduction}`
  },
  entryPoints: [fromRoot('src/app.js')],
  entryNames: '[name]-[hash]',
  outdir: fromOutput('static'),
  logLevel: 'info',
  metafile: true,
  plugins: [
    htmlTemplatePlugin(fromOutput()),
    copyPlugin(fromRoot('public'), fromOutput())
  ]
}).catch((err) => {
  console.error(err)
  process.exit(1)
})
