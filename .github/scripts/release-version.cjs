// 分支决定发布渠道；两个 workflow 对同一提交生成相同版本。
const fs = require('node:fs');
const { execFileSync } = require('node:child_process');

function resolveVersion(ref, sha, panel, agent = panel, timestamp) {
  if (!['refs/heads/dev', 'refs/heads/main'].includes(ref)) {
    throw new Error('仅允许从 dev 或 main 分支发布；tag 和其它分支不允许发布');
  }
  if (!/^[0-9a-f]{40}$/.test(sha)) throw new Error('提交 SHA 无效');
  const stable = /^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$/;
  for (const version of [panel, agent]) {
    if (!stable.test(version)) throw new Error('VERSION 和 VERSION.agent 必须使用正式目标版本 x.y.z');
  }
  const pre = ref === 'refs/heads/dev';
  if (pre && !/^[1-9]\d*$/.test(timestamp)) throw new Error('提交时间戳无效');
  // 时间戳支持现有版本比较，SHA 区分同一秒内的不同提交。
  const suffix = pre ? `-pre.${timestamp}.g${sha.slice(0, 12)}` : '';
  return {
    version: panel + suffix,
    agent_version: agent + suffix,
    prerelease: String(pre),
    latest: String(!pre),
  };
}

function checkTag(refs, tag, sha) {
  // ls-remote 输出是 SHA 在前，引用在后。
  const byRef = new Map(refs.trim().split(/\r?\n/).filter(Boolean).map(line => {
    const [hash, ref] = line.split(/\s+/);
    return [ref, hash];
  }));
  const existing = byRef.get(`refs/tags/${tag}^{}`) || byRef.get(`refs/tags/${tag}`);
  if (existing && existing !== sha) throw new Error(`${tag} 已属于其它提交，请提升版本号后再发布`);
}

if (require.main === module) {
  try {
    const panel = fs.readFileSync('VERSION', 'utf8').trim();
    const agent = fs.existsSync('VERSION.agent') ? fs.readFileSync('VERSION.agent', 'utf8').trim() : panel;
    const timestamp = execFileSync('git', ['show', '-s', '--format=%ct', 'HEAD'], { encoding: 'utf8' }).trim();
    const output = resolveVersion(process.env.GITHUB_REF, process.env.GITHUB_SHA, panel, agent, timestamp);
    const tag = `v${output.version}`;
    const refs = execFileSync('git', ['ls-remote', '--tags', 'origin', `refs/tags/${tag}`, `refs/tags/${tag}^{}`], { encoding: 'utf8' });
    checkTag(refs, tag, process.env.GITHUB_SHA);
    fs.appendFileSync(process.env.GITHUB_OUTPUT, Object.entries(output).map(([key, value]) => `${key}=${value}\n`).join(''));
    console.log(`面板 ${output.version} / Agent ${output.agent_version}；预发布=${output.prerelease}，latest=${output.latest}`);
  } catch (error) {
    console.error(error.message);
    process.exitCode = 1;
  }
}

module.exports = { resolveVersion, checkTag };
