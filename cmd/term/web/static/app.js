// import { Terminal } from '@xterm/xterm'
// import { FitAddon } from '@xterm/addon-fit'
// import { AttachAddon } from '@xterm/addon-attach'
// import { Unicode11Addon } from '@xterm/addon-unicode11'

const terminal = new Terminal({
  allowProposedApi: true,
  fontFamily: 'ui-monospace, Menlo, Consolas, Hack, Liberation Mono, Microsoft Yahei, Noto Sans Mono CJK SC, sans-serif',
  fontWeightBold: 'normal',
  drawBoldTextInBrightColors: true,
  theme: {
    background: "#232627",
    black: "#232627",
    blue: "#1D99F3",
    brightBlack: "#7F8C8D",
    brightBlue: "#3DAEE9",
    brightCyan: "#16A085",
    brightGreen: "#1CDC9A",
    brightMagenta: "#8E44AD",
    brightRed: "#C0392B",
    brightWhite: "#FFFFFF",
    brightYellow: "#FDBC4B",
    cursor: "#FCFCFC",
    cursorAccent: "#232627",
    cyan: "#1ABC9C",
    foreground: "#FCFCFC",
    green: "#11D116",
    magenta: "#9B59B6",
    red: "#ED1515",
    selectionBackground: "#FCFCFC",
    selectionForeground: "#232627",
    selectionInactiveBackground: "#FCFCFC80",
    white: "#FCFCFC",
    yellow: "#F67400"
  }
})

const fitAddon = new FitAddon.FitAddon()
terminal.loadAddon(fitAddon)
terminal.open(document.getElementById('terminal'))
fitAddon.fit()

const unicode11Addon = new Unicode11Addon.Unicode11Addon()
terminal.loadAddon(unicode11Addon)
terminal.unicode.activeVersion = '11'

const webSocket = new WebSocket(`ws://${window.location.host}/ws?${new URLSearchParams({ w: terminal.cols, h: terminal.rows })}`)
const attachAddon = new AttachAddon.AttachAddon(webSocket)
terminal.loadAddon(attachAddon)
