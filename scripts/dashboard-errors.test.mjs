import {test} from 'node:test';
import assert from 'node:assert/strict';
import {readFileSync} from 'node:fs';
import vm from 'node:vm';

const app = readFileSync(new URL('../internal/dashboard/assets/app.js', import.meta.url), 'utf8');
const source = app.slice(app.indexOf('function storageFailureDetails('), app.indexOf('function query('));

test('API errors retain safe storage facts and display original OS failure', async () => {
  const context = vm.createContext({
    generation: 0, detailGeneration: 0, sessionToken: 'private-session',
    fetch: async () => ({ok: false, status: 500, text: async () => JSON.stringify({error: {
      code: 'storage_error', message: 'Cannot access private task storage.', details: {
        domain: 'tasks', operation: 'create_directory', resource: 'data_root', reason: 'access_denied',
        os_error: 5, os_error_domain: 'win32', path: 'SECRET-PATH', remedies: [{message:'SECRET-REMEDY'}],
      },
    }})}),
  });
  vm.runInContext(source, context);
  await assert.rejects(vm.runInContext('api("/api/query")', context), error => {
    assert.equal(error.code, 'storage_error');
    assert.equal(error.details.os_error, 5);
    assert.match(error.message, /create directory: access denied; Windows error 5/);
    assert.doesNotMatch(error.message + JSON.stringify(error.details), /SECRET/);
    return true;
  });
});

test('unknown or malformed storage metadata leaves domain message usable', () => {
  const context = vm.createContext({});
  vm.runInContext(source, context);
  assert.equal(vm.runInContext('storageFailureMessage(storageFailureDetails({reason:"unknown",operation:"bad/path",os_error:-1}))', context), '');
});
