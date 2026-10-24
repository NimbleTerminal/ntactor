// Exercise one real WASM program through booba's JS bridge in a fresh process.
// This checks startup, OSC 7501 action publishing, input state transitions, resize, and clean exit.
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import vm from 'node:vm';
import { setTimeout as delay } from 'node:timers/promises';

const demo = process.argv[2];
assert.ok(['arena', 'flock', 'nav', 'scene3d', 'planner'].includes(demo));

vm.runInThisContext(await readFile('web/wasm_exec.js', 'utf8'));
const go = new Go();
let exitCode;
go.exit = code => { exitCode = code; };
const { instance } = await WebAssembly.instantiate(await readFile(`web/demos/${demo}.wasm`), go.importObject);
const timeout = setTimeout(() => { console.error(`${demo}: timed out`); process.exit(1); }, 15000);
const running = go.run(instance);

try {
  for (let n = 0; typeof globalThis.bubbletea_read !== 'function' && n < 100; n++) await delay(20);
  assert.equal(typeof globalThis.bubbletea_read, 'function', 'booba bridge installed');
  bubbletea_resize(110, 32);

  let output = '';
  for (let n = 0; n < 100 && !output.includes('\x1b]7501;?'); n++) {
    await delay(25);
    output += bubbletea_read();
  }
  assert.ok(output.includes('\x1b]7501;?'), 'application queries action support');
  bubbletea_write('\x1b]7501;?\x1b\\');

  let report;
  for (let n = 0; n < 100 && !report; n++) {
    await delay(25);
    output += bubbletea_read();
    report = output.match(/\x1b\]7501;state=(?:idle|running):[^\x1b]*:msg=([A-Za-z0-9+/=]+)\x1b\\/);
  }
  assert.ok(report, 'application publishes OSC action snapshot');
  const payload = Buffer.from(report[1], 'base64').toString('utf8');
  const snapshot = JSON.parse(payload.split(' | booba-actions/v1 ')[1]);
  const quit = snapshot.actions.find(action => action.label === 'Quit');
  assert.ok(quit?.enabled, 'quit action enabled');

  async function expectSummaryContains(substring) {
    let received = '';
    for (let n = 0; n < 100; n++) {
      await delay(20);
      received += bubbletea_read();
      for (const match of received.matchAll(/\x1b\]7501;state=(?:idle|running):[^\x1b]*:msg=([A-Za-z0-9+/=]+)\x1b\\/g)) {
        const message = Buffer.from(match[1], 'base64').toString('utf8');
        const summary = message.split(' | booba-actions/v1 ')[0];
        if (summary.includes(substring)) return;
      }
    }
    assert.fail(`Missing summary substring: ${substring}; received ${JSON.stringify(received.slice(-300))}`);
  }

  async function send(bytes, substring) {
    bubbletea_read();
    bubbletea_write(bytes);
    await delay(30);
    bubbletea_write('\x1b]7501;?\x1b\\');
    await expectSummaryContains(substring);
  }

  bubbletea_resize(100, 30);
  if (demo === 'arena') {
    await send(' ', 'Arena [PAUSED]:');
    await send('b', 'actors=6');
    await send('i', 'actors=7');
    await send('f', 'actors=13');
  } else if (demo === 'flock') {
    await send(' ', 'Flock [PAUSED]:');
    await send('a', 'attractors=1');
    await send('p', 'predators=1');
  } else if (demo === 'nav') {
    await send(' ', 'Nav [PAUSED]:');
    await send('s', 'smooth=false');
    await send('c', 'actors=16');
  } else if (demo === 'scene3d') {
    await send(' ', 'Scene3D [PAUSED]:');
    await send('b', 'beacons=2');
    await send('i', 'intruders=1');
    await send('f', 'boids=56');
  } else if (demo === 'planner') {
    await send(' ', 'Planner [PAUSED]:');
    await send('s', 'spec=astar_collector');
    await send('e', 'spec=utility_hunter');
  }

  bubbletea_write(quit.keys[quit.preferred].key);
  await running;
  assert.equal(exitCode, 0);
  assert.equal(globalThis.bubbletea_exited, true, 'exit published to adapter');
  console.log(`${demo}: startup, actions, resize, exact demo state transitions, and clean exit passed`);
} finally {
  clearTimeout(timeout);
}
process.exit(0);
