import {test} from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import vm from 'node:vm';

const source = ['manage.js', 'value-management.js', 'task-management.js', 'storage.js'].map(name =>
  readFileSync(new URL('../internal/dashboard/assets/' + name, import.meta.url), 'utf8')).join('\n');

function browser() {
  class Element {
    constructor(tag) { this.tag = tag; this.children = []; this.type = ''; this.disabled = false; this.open = false; this.listeners = []; }
    set value(value) {
      const text = String(value);
      this.content = this.tag === 'textarea' ? text.replace(/\r\n?/g, '\n') :
        this.tag === 'input' && ['text', 'password'].includes(this.type) ? text.replace(/[\r\n]/g, '') : text;
    }
    get value() { return this.content ?? (this.tag === 'select' ? this.children[0]?.value ?? '' : ''); }
    append(child) { this.children.push(child); child.parentElement = this; }
    replaceChildren() { this.children = []; }
    querySelectorAll(selector) {
      return this.children.flatMap(child => [...(selector.split(',').includes(child.tag) ? [child] : []), ...child.querySelectorAll(selector)]);
    }
    querySelector(selector) { return this.querySelectorAll(selector)[0]; }
    setAttribute() {}
    focus() {}
    showModal() { this.open = true; }
    addEventListener(name, fn) { if (name === 'close') this.listeners.push(fn); }
    close() {
      if (!this.open) return;
      this.open = false;
      queueMicrotask(() => {
        this.onclose?.();
        const listeners = this.listeners; this.listeners = [];
        listeners.forEach(fn => fn());
      });
    }
  }
  const nodes = new Map();
  const $ = id => {
    if (!nodes.has(id)) { const element = new Element(id === 'editor' ? 'dialog' : id === 'edit-form' ? 'form' : id.includes('save') || id.includes('cancel') ? 'button' : 'div'); element.id = id; nodes.set(id, element); }
    return nodes.get(id);
  };
  $('edit-form').append($('editor-fields')); $('edit-form').append($('editor-save')); $('edit-form').append($('editor-cancel'));
  const calls = [];
  const context = vm.createContext({ $, document: {createElement: tag => new Element(tag)},
    text: (tag, value, parent) => { const element = new Element(tag); element.textContent = value; parent?.append(element); return element; },
    profile: 'app', generation: 1, detailGeneration: 1, scope: 'values', ws: '', revision: 1,
    crypto: {randomUUID: () => 'request'}, URLSearchParams, TextEncoder,
    load: () => calls.push('load'), notice: value => calls.push(value), saveNavigation: () => {},
    api: async (path, body) => { calls.push(body); return {data: {}}; },
  });
  vm.runInContext(source, context);
  return { $, context, calls, run: code => vm.runInContext(code, context), submit: () => $('edit-form').onsubmit({preventDefault() {}}) };
}

for (const response of ['success', 'error', 'preview']) test(`late ${response} from a closed editor cannot affect its replacement`, async () => {
  const b = browser(); let resolve, reject;
  b.context.api = () => new Promise((done, fail) => { resolve = done; reject = fail; });
  b.run(`edit('Old', p => {field(p, 'Old value', 'old'); return () => ({});}, () => ({action:'${response === 'preview' ? 'workstream.edited' : 'task.add'}'}), () => load())`);
  const pending = b.submit();
  b.$('editor').close();
  await Promise.resolve();
  b.run(`edit('New', p => {field(p, 'New value', 'keep me'); return () => ({});}, () => ({action:'task.add'}))`);
  if (response === 'error') reject(Error('old request failed'));
  else resolve({data: {changes: [], effects: [], impact: {affected_ids: []}, issues: []}});
  await pending;
  assert.equal(b.$('editor').open, true);
  assert.equal(b.$('editor-fields').querySelector('input').value, 'keep me');
  assert.equal(b.$('editor-error').textContent, '');
  assert.equal(b.$('editor-save').textContent, 'Save');
  assert.deepEqual(b.calls, []);
});

test('closing and reopening before the queued close event preserves new fields', async () => {
  const b = browser();
  b.run(`edit('Old', p => {field(p, 'Value', 'old'); return () => ({});}, () => ({}))`);
  b.$('editor').close();
  b.run(`edit('New', p => {field(p, 'Value', 'new'); return () => ({});}, () => ({}))`);
  await Promise.resolve();
  assert.equal(b.$('editor-fields').querySelector('input').value, 'new');
  assert.equal(typeof b.$('edit-form').onsubmit, 'function');
});

test('late success after closing settles without running the old continuation', async () => {
  const b = browser(); let resolve, settled = false;
  b.context.api = () => new Promise(done => { resolve = done; });
  b.run(`edit('Old', p => () => ({}), () => ({action:'task.add'}), () => load())`);
  const pending = b.submit().then(() => { settled = true; });
  b.$('editor').close(); await Promise.resolve();
  resolve({data: {}});
  await new Promise(done => setImmediate(done));
  assert.equal(settled, true);
  await pending;
  assert.deepEqual(b.calls, []);
});

for (const value of ['first\n두 번째\n', 'first\r\nsecond\rthird']) test(`inline variable edit preserves unchanged value ${JSON.stringify(value)}`, async () => {
  const b = browser(); b.context.original = value;
  b.run(`inlineValue($('cell'), {key:'TEXT',kind:'variable',source:'common',value:original}, {profile:'app',revision:1,env:''}, 1)`);
  const form = b.$('cell').querySelector('form');
  await form.onsubmit({preventDefault() {}});
  assert.equal(b.calls[0].change.value, value);
});

test('override keeps the unchanged multiline value from the target environment', async () => {
  const b = browser(); const original = 'target\r\n한글\rline';
  b.context.api = async (path, body) => body ? {data: {}} :
    {profile: 'app', revision: 2, env: 'local', items: [{key: 'TEXT', kind: 'variable', source: 'env', value: original}]};
  b.run(`overrideValue({key:'TEXT',kind:'variable',value:'common'}, {profile:'app',revision:1,envs:['local']}, 1)`);
  await b.submit();
  b.context.api = async (path, body) => { b.calls.push(body); return {data: {}}; };
  await b.submit();
  assert.equal(b.calls[0].change.value, original);
});

test('pending override lookup cannot open a modal over a newer editor', async () => {
  const b = browser(); let resolve;
  b.context.api = () => new Promise(done => { resolve = done; });
  b.run(`overrideValue({key:'TEXT',kind:'variable',value:'common'}, {profile:'app',revision:1,envs:['local']}, 1)`);
  const pending = b.submit();
  await new Promise(done => setImmediate(done));
  b.run(`edit('New', p => {field(p, 'Value', 'keep me'); return () => ({});}, () => ({}))`);
  resolve({profile: 'app', revision: 2, env: 'local', items: []});
  await pending;
  assert.equal(b.$('editor-title').textContent, 'New');
  assert.equal(b.$('editor-fields').querySelector('input').value, 'keep me');
});

test('adding a variable accepts multiple lines and leaves inputs editable after a rejected request', async () => {
  const b = browser();
  b.context.api = async () => ({profile:'app',revision:1,env:'',envs:[],items:[]});
  await b.run('loadValues(1)');
  const add = b.$('values-panel').querySelectorAll('button').find(button => button.textContent === 'Add value');
  add.onclick();
  const kind = b.$('editor-fields').querySelector('select');
  kind.value = 'variable'; kind.onchange();
  const input = b.$('editor-fields').querySelector('textarea');
  input.value = 'first\n한글\n';
  b.context.api = async (path, body) => { b.calls.push(body); const error = Error('invalid key'); error.responded = true; throw error; };
  await b.submit();
  assert.equal(b.calls[0].change.value, 'first\n한글\n');
  assert.equal(input.disabled, false);
  assert.equal(input.value, 'first\n한글\n');
});

for (const action of ['Dependencies', 'Restore excluded tasks', 'Cancel', 'Preview cleanup'])
  for (const closeNew of [false, true]) test(`deferred ${action} preparation cannot replace a newer ${closeNew ? 'closed' : 'open'} editor`, async () => {
    const b = browser(); let resolve;
    if (action === 'Preview cleanup') {
      b.context.api = async () => ({items: []});
      await b.run('loadStorage(1)');
    } else {
      b.run(`itemActions({id:'workstream',kind:'workstream'}, {revision:1,item:{id:'workstream',title:'Work',state:'draft'}, tasks:[]}, $('panel'))`);
    }
    b.context.api = () => new Promise(done => { resolve = done; });
    const parent = b.$(action === 'Preview cleanup' ? 'values-panel' : 'panel');
    const trigger = parent.querySelectorAll('button').find(button => button.textContent === action);
    const pending = trigger.onclick();
    b.run(`edit('New', p => {field(p, 'Value', 'keep me'); return () => ({});}, () => ({}))`);
    if (closeNew) { b.$('editor').close(); await Promise.resolve(); }
    resolve({revision:1,items:[],next_cursor:null,id:'plan',running_ids:[],completed_ids:[]});
    await pending;
    assert.equal(b.$('editor-title').textContent, 'New');
    assert.equal(b.$('editor').open, !closeNew);
    if (!closeNew) assert.equal(b.$('editor-fields').querySelector('input').value, 'keep me');
    assert.deepEqual(b.calls, []);
  });
