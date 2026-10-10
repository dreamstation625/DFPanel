const { test } = require('node:test');
const assert = require('node:assert/strict');
const { resolveVersion, checkTag } = require('./release-version.cjs');
const sha = '0123456789abcdef0123456789abcdef01234567';

test('dev 只能生成预发布，面板和 Agent 保留各自目标版本', () => {
  assert.deepEqual(resolveVersion('refs/heads/dev', sha, '0.0.3', '0.0.4', '1791590400'), {
    version: '0.0.3-pre.1791590400.g0123456789ab', agent_version: '0.0.4-pre.1791590400.g0123456789ab', prerelease: 'true', latest: 'false',
  });
});
test('main 只生成正式版本', () => {
  assert.deepEqual(resolveVersion('refs/heads/main', sha, '0.0.3'), {
    version: '0.0.3', agent_version: '0.0.3', prerelease: 'false', latest: 'true',
  });
});
test('拒绝 tag、其它分支、非正式目标版本及无效 SHA', () => {
  for (const ref of ['refs/tags/v0.0.3', 'refs/heads/feature', 'refs/pull/1/merge', '']) {
    assert.throws(() => resolveVersion(ref, sha, '0.0.3'));
  }
  for (const version of ['0.0.3-pre.1', '0.0.03', '1.2', '1.2.3\nother', '1.2.3+meta']) {
    assert.throws(() => resolveVersion('refs/heads/main', sha, version));
    assert.throws(() => resolveVersion('refs/heads/dev', sha, '0.0.3', version));
  }
  assert.throws(() => resolveVersion('refs/heads/dev', 'bad', '0.0.3'));
  assert.throws(() => resolveVersion('refs/heads/dev', sha, '0.0.3', '0.0.3', 'bad'));
});
test('不同提交的发布不互相覆盖；同一提交可以重跑', () => {
  checkTag('', 'v0.0.3', sha);
  checkTag(`${sha}\trefs/tags/v0.0.3\n`, 'v0.0.3', sha);
  const other = 'abcdef0123456789abcdef0123456789abcdef01';
  assert.throws(() => checkTag(`${other}\trefs/tags/v0.0.3\n`, 'v0.0.3', sha));
  checkTag(`${other}\trefs/tags/v0.0.3\n${sha}\trefs/tags/v0.0.3^{}\n`, 'v0.0.3', sha);
  assert.throws(() => checkTag(`${sha}\trefs/tags/v0.0.3\n${other}\trefs/tags/v0.0.3^{}\n`, 'v0.0.3', sha));
});
