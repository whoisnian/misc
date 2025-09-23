// source: https://github.com/xtermjs/xterm.js/blob/ab43a3bd22082d2dc3045672df9a62473d9cec2f/addons/addon-attach/src/AttachAddon.ts
// * Transformed from typescript to javascript
// * Add message type byte for data and resize messages
// * Send resize message on terminal resize event

const MSG_TYPE_DATA = '0'
const MSG_TYPE_RESIZE = '1'

export class AttachAddon {
  constructor (socket, options) {
    this._socket = socket
    this._disposables = []
    // always set binary type to arraybuffer, we do not handle blobs
    this._socket.binaryType = 'arraybuffer'
    this._bidirectional = !(options && options.bidirectional === false)
  }

  activate (terminal) {
    this._disposables.push(
      addSocketListener(this._socket, 'message', ev => {
        const data = ev.data
        if (typeof data === 'string') {
          if (data.length > 1 && data[0] === MSG_TYPE_DATA) {
            terminal.write(data.substring(1))
          } else if (data.length > 1 && data[0] === MSG_TYPE_RESIZE) {
            const cols = data.charCodeAt(1) | (data.charCodeAt(2) << 8)
            const rows = data.charCodeAt(3) | (data.charCodeAt(4) << 8)
            terminal.resize(cols, rows)
          } else {
            console.warn('Attach addon received invalid text message:', data)
          }
        } else {
          const byteArray = new Uint8Array(data)
          if (byteArray.length > 1 && byteArray[0] === MSG_TYPE_DATA.charCodeAt(0)) {
            terminal.write(byteArray.subarray(1))
          } else if (byteArray.length > 1 && byteArray[0] === MSG_TYPE_RESIZE.charCodeAt(0)) {
            const cols = byteArray[1] | (byteArray[2] << 8)
            const rows = byteArray[3] | (byteArray[4] << 8)
            terminal.resize(cols, rows)
          } else {
            console.warn('Attach addon received invalid binary message:', data)
          }
        }
      })
    )

    if (this._bidirectional) {
      this._disposables.push(terminal.onData(data => this._sendData(data)))
      this._disposables.push(terminal.onBinary(data => this._sendBinary(data)))
      this._disposables.push(terminal.onResize(ev => this._sendResize(ev.cols, ev.rows)))
    }

    this._disposables.push(addSocketListener(this._socket, 'close', () => this.dispose()))
    this._disposables.push(addSocketListener(this._socket, 'error', () => this.dispose()))
  }

  dispose () {
    for (const d of this._disposables) {
      d.dispose()
    }
  }

  _sendData (data) {
    if (!this._checkOpenSocket()) {
      return
    }
    this._socket.send(MSG_TYPE_DATA + data)
  }

  _sendBinary (data) {
    if (!this._checkOpenSocket()) {
      return
    }
    const buffer = new Uint8Array(data.length + 1)
    buffer[0] = MSG_TYPE_DATA.charCodeAt(0)
    for (let i = 0; i < data.length; ++i) {
      buffer[i + 1] = data.charCodeAt(i) & 255
    }
    this._socket.send(buffer)
  }

  _sendResize (cols, rows) {
    if (!this._checkOpenSocket()) {
      return
    }
    const buffer = new Uint8Array(1 + 2 + 2)
    buffer[0] = MSG_TYPE_RESIZE.charCodeAt(0)
    buffer[1] = cols & 255
    buffer[2] = (cols >> 8) & 255
    buffer[3] = rows & 255
    buffer[4] = (rows >> 8) & 255
    this._socket.send(buffer)
  }

  _checkOpenSocket () {
    switch (this._socket.readyState) {
      case WebSocket.OPEN:
        return true
      case WebSocket.CONNECTING:
        throw new Error('Attach addon was loaded before socket was open')
      case WebSocket.CLOSING:
        console.warn('Attach addon socket is closing')
        return false
      case WebSocket.CLOSED:
        throw new Error('Attach addon socket is closed')
      default:
        throw new Error('Unexpected socket state')
    }
  }
}

function addSocketListener (socket, type, handler) {
  socket.addEventListener(type, handler)
  return {
    dispose: () => {
      if (!handler) {
        // Already disposed
        return
      }
      socket.removeEventListener(type, handler)
    }
  }
}
