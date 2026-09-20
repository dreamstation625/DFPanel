// 与服务端一致的密码字符集：剔除 0/O/1/l/I 等易混淆字符
const CHARSET = 'abcdefghijkmnpqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789'

/** 生成随机密码，默认 8 位 */
export function genPassword(len = 8): string {
  const buf = new Uint32Array(len)
  if (typeof crypto !== 'undefined' && crypto.getRandomValues) {
    crypto.getRandomValues(buf)
  } else {
    for (let i = 0; i < len; i++) buf[i] = Math.floor(Math.random() * 0xffffffff)
  }
  return Array.from(buf, (v) => CHARSET[v % CHARSET.length]).join('')
}
