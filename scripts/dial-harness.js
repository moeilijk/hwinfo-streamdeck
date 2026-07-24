// Minimal Stream Deck WS host that drives the real plugin and captures the
// dial carousel catalog + feedback over the actual SDK protocol — without the
// full DeckBridge daemon. Usage: node dial-harness.js <pluginBinary> <companionUrl>
const { WebSocketServer } = require('ws')
const { spawn } = require('child_process')

const pluginBin = process.argv[2]
const companionUrl = process.argv[3] || 'http://127.0.0.1:8085/data.json'
const m = companionUrl.match(/^https?:\/\/([^:/]+):(\d+)/)
const host = m ? m[1] : '127.0.0.1'
const port = m ? parseInt(m[2], 10) : 8085

const PORT = 38123
const PLUGIN_UUID = 'harness-plugin'
const DIAL_CTX = 'dial-ctx-0'
const DIAL_ACTION = 'com.exension.hwinfo.dial'
const DEVICE = 'harness-plus-0'

const globalSettings = {
  pollInterval: 1000,
  sourceProfiles: [{ id: 'companion', name: 'companion', host, port }],
  defaultSourceProfileId: 'companion',
}

let gotCatalog = false, gotFeedback = false
const seen = []
const wss = new WebSocketServer({ port: PORT })
let sock = null

function send(obj) { if (sock && sock.readyState === 1) sock.send(JSON.stringify(obj)) }

wss.on('connection', (ws) => {
  sock = ws
  ws.on('message', (raw) => {
    let msg; try { msg = JSON.parse(raw.toString()) } catch { return }
    if (msg.event === 'registerPlugin') {
      // bring the dial up exactly like the Stream Deck software does
      send({ event: 'didReceiveGlobalSettings', payload: { settings: globalSettings } })
      send({ event: 'deviceDidConnect', device: DEVICE, deviceInfo: { name: 'Stream Deck +', type: 7, size: { columns: 4, rows: 2 } } })
      send({
        event: 'willAppear', action: DIAL_ACTION, context: DIAL_CTX, device: DEVICE,
        payload: { settings: {}, controller: 'Encoder', coordinates: { column: 0, row: 0 } },
      })
      setTimeout(() => send({ event: 'propertyInspectorDidAppear', action: DIAL_ACTION, context: DIAL_CTX, device: DEVICE }), 800)
      return
    }
    if (msg.event === 'getGlobalSettings') {
      send({ event: 'didReceiveGlobalSettings', payload: { settings: globalSettings } })
      return
    }
    const p = msg.payload || {}
    if (msg.event === 'setFeedbackLayout') seen.push('setFeedbackLayout')
    if (msg.event === 'setFeedback') { gotFeedback = true; seen.push('setFeedback(' + Object.keys(p).join(',') + ')') }
    if (msg.event === 'sendToPropertyInspector') {
      if (p.error === true) console.log('  PI error:', JSON.stringify(p))
      if (p.catalog) {
        gotCatalog = true
        console.log('  CATALOG: sensors=' + (p.catalog.sensors || []).length + ' readings=' + (p.catalog.readings || []).length)
        ;(p.catalog.readings || []).slice(0, 8).forEach((r) =>
          console.log('     ' + (r.sensorName || '') + ' / ' + r.label + (r.unit ? ' (' + r.unit + ')' : '')))
      }
    }
  })
})

const child = spawn(pluginBin, [
  '-port', String(PORT), '-pluginUUID', PLUGIN_UUID,
  '-registerEvent', 'registerPlugin',
  '-info', JSON.stringify({ application: { platform: 'linux', version: '7.0' }, devices: [] }),
], { stdio: ['ignore', 'inherit', 'inherit'] })

setTimeout(() => {
  console.log('---')
  console.log('events:', JSON.stringify(seen))
  console.log('RESULT catalog=' + gotCatalog + ' feedback=' + gotFeedback)
  child.kill()
  wss.close()
  process.exit(gotCatalog && gotFeedback ? 0 : 2)
}, 9000)
