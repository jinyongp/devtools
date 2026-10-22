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

test('common diagnostics retain failed checks, actions and partial process identifiers', async () => {
  const context = vm.createContext({
    generation: 0, detailGeneration: 0, sessionToken: 'private-session',
    fetch: async () => ({ok:false, status:409, text:async () => JSON.stringify({error:{
      code:'requirements_failed', message:'Some commands could not be started.', details:{
        operation:'services up', phase:'preflight', reason:'prerequisites', retry:'after_dependency_change', effects:'partial',
        target:{kind:'command',name:'api'}, facts:{stage:'prepare',path:'PRIVATE-PATH',outcomes:[{kind:'command',name:'web',id:'sample-process',state:'started',changed:true,token:'PRIVATE-TOKEN'}]},
        checks:[{id:'tool:node',status:'fail',reason:'not_found',output:'PRIVATE-OUTPUT'}],
        remedies:[{argv:['devtools','doctor','api'],required_inputs:[],effect:'read',message:'Inspect this command.'}],
      },
    }})}),
  });
  vm.runInContext(source, context);
  await assert.rejects(vm.runInContext('api("/api/query")', context), error => {
    assert.equal(error.details.effects, 'partial');
    assert.match(error.message, /tool:node: not_found/);
    assert.match(error.message, /web: started \(sample-process\)/);
    assert.match(error.message, /Inspect this command/);
    assert.doesNotMatch(JSON.stringify(error.details), /PRIVATE-/);
    return true;
  });
});
