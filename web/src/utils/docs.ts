// frp 官方文档链接（锚点取自官方文档小节标题）
//
// ⚠️ 官网会重构目录结构：2026-03-30 前后把原扁平的 `/docs/features/<name>/` 拆成了
// `/docs/features/<分类>/<页>/`，导致旧的 virtual-hosts / encryption-compression /
// health-check / dashboard 四个地址全部 404。当前全部链接已于 2026-09-21 逐条实测
// （200 且锚点存在），**改动这里后请重新实测**，不要凭印象写路径。
const BASE = 'https://gofrp.org/zh-cn/docs'

export const DOC = {
  home: `${BASE}/`,

  // 服务端配置参考
  server: `${BASE}/reference/server-configures/`,
  serverConfig: `${BASE}/reference/server-configures/#serverconfig`,
  serverAuth: `${BASE}/reference/server-configures/#authserverconfig`,
  serverOidc: `${BASE}/reference/server-configures/#authoidcserverconfig`,
  serverTransport: `${BASE}/reference/server-configures/#servertransportconfig`,
  serverTls: `${BASE}/reference/server-configures/#tlsserverconfig`,
  serverSshGateway: `${BASE}/reference/server-configures/#sshtunnelgateway`,

  // 客户端配置参考
  client: `${BASE}/reference/client-configures/`,
  clientConfig: `${BASE}/reference/client-configures/#clientconfig`,
  clientCommon: `${BASE}/reference/client-configures/#clientcommonconfig`,
  clientTransport: `${BASE}/reference/client-configures/#clienttransportconfig`,
  clientTls: `${BASE}/reference/client-configures/#tlsclientconfig`,
  clientAuth: `${BASE}/reference/client-configures/#authclientconfig`,

  // 通用配置
  common: `${BASE}/reference/common/`,
  commonLog: `${BASE}/reference/common/#logconfig`,
  commonWebServer: `${BASE}/reference/common/#webserverconfig`,
  commonTls: `${BASE}/reference/common/#tlsconfig`,
  commonQuic: `${BASE}/reference/common/#quicoptions`,
  commonPorts: `${BASE}/reference/common/#portsrange`,

  // 代理（隧道）配置
  proxy: `${BASE}/reference/proxy/`,
  proxyTcp: `${BASE}/reference/proxy/#tcpproxyconfig`,
  proxyUdp: `${BASE}/reference/proxy/#udpproxyconfig`,
  proxyHttp: `${BASE}/reference/proxy/#httpproxyconfig`,
  proxyHttps: `${BASE}/reference/proxy/#httpsproxyconfig`,
  proxyStcp: `${BASE}/reference/proxy/#stcpproxyconfig`,
  proxySudp: `${BASE}/reference/proxy/#sudpproxyconfig`,
  proxyXtcp: `${BASE}/reference/proxy/#xtcpproxyconfig`,

  // 功能说明（路径已按重构后的目录校对）
  features: `${BASE}/features/`,
  /** HTTP & HTTPS 代理：虚拟主机端口 vhostHTTPPort / vhostHTTPSPort */
  featureVirtualHost: `${BASE}/features/http-https/`,
  /** 自定义二级域名：subdomain / subDomainHost */
  featureSubdomain: `${BASE}/features/http-https/subdomain/`,
  /** 加密与压缩：useEncryption / useCompression */
  featureEncryption: `${BASE}/features/common/network/network/#加密与压缩`,
  /** 负载均衡与健康检查 */
  featureHealthCheck: `${BASE}/features/common/load-balancer/#健康检查`,
  /** Web 界面：服务端 Dashboard */
  featureAdminUI: `${BASE}/features/common/ui/#服务端-dashboard`,
}

/** 菜单中的官方文档入口（文案保持简短，避免侧边栏折行过多） */
export const DOC_MENU: { label: string; url: string }[] = [
  { label: '文档首页', url: DOC.home },
  { label: '服务端配置', url: DOC.server },
  { label: '客户端配置', url: DOC.client },
  { label: '通用配置', url: DOC.common },
  { label: '代理配置', url: DOC.proxy },
  { label: '功能说明', url: DOC.features },
]

/** 隧道类型对应的官方文档锚点 */
export function proxyDoc(type: string): string {
  switch (type) {
    case 'udp':
      return DOC.proxyUdp
    case 'http':
      return DOC.proxyHttp
    case 'https':
      return DOC.proxyHttps
    case 'stcp':
      return DOC.proxyStcp
    case 'sudp':
      return DOC.proxySudp
    case 'xtcp':
      return DOC.proxyXtcp
    default:
      return DOC.proxyTcp
  }
}
