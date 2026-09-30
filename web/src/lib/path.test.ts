import assert from 'node:assert/strict'
import { test } from 'node:test'
import { matchPath } from './path.ts'

test('matchPath: パラメーターを取り出す', () => {
  assert.deepEqual(matchPath('/scenarios/:sid/activities/:aid', '/scenarios/3/activities/12'), { sid: '3', aid: '12' })
  assert.deepEqual(matchPath('/activities', '/activities/'), {})
  assert.deepEqual(matchPath('/', '/'), {})
})

test('matchPath: 一致しない', () => {
  assert.equal(matchPath('/activities/:id', '/activities'), null)
  assert.equal(matchPath('/activities/:id', '/scenarios/1'), null)
  assert.equal(matchPath('/activities', '/activities/1'), null)
})

test('matchPath: エンコードされた値を戻す', () => {
  assert.deepEqual(matchPath('/x/:name', '/x/%E6%96%BD%E7%AD%96'), { name: '施策' })
})
