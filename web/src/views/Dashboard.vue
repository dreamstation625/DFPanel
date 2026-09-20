<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import {
  agentApi,
  nodeApi,
  serverApi,
  statusLabel,
  statusType,
  type AgentInfo,
  type FrpsServer,
  type NodeInfo,
} from '@/api'

const loading = ref(false)
const servers = ref<FrpsServer[]>([])
const agents = ref<AgentInfo[]>([])
const nodes = ref<NodeInfo[]>([])

const serverRunning = computed(() => servers.value.filter((s) => s.status === 'running').length)
const agentOnline = computed(() => agents.value.filter((a) => a.online).length)
const nodeRunning = computed(() => nodes.value.filter((n) => n.status === 'running').length)
const nodeUnbound = computed(() => nodes.value.filter((n) => !n.agentId).length)
const notInstalled = computed(
  () => servers.value.some((s) => s.deployMode !== 'agent') && servers.value.every((s) => s.status === 'not_installed'),
)

const agentName = (id: number) => agents.value.find((a) => a.id === id)?.name ?? '未绑定'
const serverName = (id: number) => servers.value.find((s) => s.id === id)?.name ?? '未关联'

async function load() {
  loading.value = true
  try {
    const [s, a, n] = await Promise.all([serverApi.list(), agentApi.list(), nodeApi.list()])
    servers.value = s
    agents.value = a
    nodes.value = n
  } finally {
    loading.value = false
  }
}

onMounted(load)
</script>

<template>
  <div class="page" v-loading="loading">
    <div class="page-header">
      <div class="page-title">概览</div>
      <div class="actions">
        <el-button @click="load">刷新</el-button>
      </div>
    </div>

    <el-alert
      v-if="notInstalled"
      type="warning"
      show-icon
      :closable="false"
      title="frps 二进制未就绪"
      description="本机托管模式需要在面板 bin 目录下放置 frps；如需在其它服务器运行，请改用“远端 Agent 托管”。"
      style="margin-bottom: 16px"
    />

    <el-row :gutter="16">
      <el-col :span="6">
        <el-card shadow="hover">
          <div class="stat">
            <div class="stat-value">
              {{ servers.length }}
              <span class="stat-sub">/ {{ serverRunning }} 运行</span>
            </div>
            <div class="stat-label">frps 服务端</div>
          </div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card shadow="hover">
          <div class="stat">
            <div class="stat-value success">
              {{ agents.length }}
              <span class="stat-sub">/ {{ agentOnline }} 在线</span>
            </div>
            <div class="stat-label">托管 Agent</div>
          </div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card shadow="hover">
          <div class="stat">
            <div class="stat-value">
              {{ nodes.length }}
              <span class="stat-sub">/ {{ nodeRunning }} 运行</span>
            </div>
            <div class="stat-label">客户端节点</div>
          </div>
        </el-card>
      </el-col>
      <el-col :span="6">
        <el-card shadow="hover">
          <div class="stat">
            <div class="stat-value" :class="{ warn: nodeUnbound > 0 }">{{ nodeUnbound }}</div>
            <div class="stat-label">未绑定 Agent 的节点</div>
          </div>
        </el-card>
      </el-col>
    </el-row>

    <el-card style="margin-top: 16px">
      <template #header>frps 服务端状态</template>
      <el-table v-if="servers.length" :data="servers" style="width: 100%">
        <el-table-column prop="name" label="名称" />
        <el-table-column label="部署位置">
          <template #default="{ row }">
            <el-tag v-if="row.deployMode === 'agent'" size="small" type="primary">
              Agent：{{ agentName(row.agentId) }}
            </el-tag>
            <el-tag v-else size="small" type="info">面板本机</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="监听端口">
          <template #default="{ row }">{{ row.bindPort }}</template>
        </el-table-column>
        <el-table-column label="Dashboard">
          <template #default="{ row }">
            {{ row.dashboardEnabled && row.dashboardPort ? row.dashboardPort : '未启用' }}
          </template>
        </el-table-column>
        <el-table-column label="状态">
          <template #default="{ row }">
            <el-tag :type="statusType(row.status)" effect="dark">{{ statusLabel(row.status) }}</el-tag>
          </template>
        </el-table-column>
      </el-table>
      <el-empty v-else description="还没有 frps 服务端，请到“frps 服务端”页面创建" />
    </el-card>

    <el-card style="margin-top: 16px">
      <template #header>客户端节点状态</template>
      <el-table v-if="nodes.length" :data="nodes" style="width: 100%">
        <el-table-column prop="name" label="节点" />
        <el-table-column label="托管 Agent">
          <template #default="{ row }">
            {{ agentName(row.agentId) }}
            <el-tag v-if="row.agentId" size="small" :type="agents.find((a) => a.id === row.agentId)?.online ? 'success' : 'info'">
              {{ agents.find((a) => a.id === row.agentId)?.online ? '在线' : '离线' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column label="服务端">
          <template #default="{ row }">{{ serverName(row.serverId) }}</template>
        </el-table-column>
        <el-table-column label="状态">
          <template #default="{ row }">
            <el-tag :type="statusType(row.status)" effect="dark">{{ statusLabel(row.status) }}</el-tag>
          </template>
        </el-table-column>
      </el-table>
      <el-empty v-else description="还没有客户端节点，请到“客户端节点”页面创建" />
    </el-card>
  </div>
</template>

<style scoped>
.actions {
  display: flex;
  align-items: center;
}

.stat {
  text-align: center;
  padding: 8px 0;
}

.stat-value {
  font-size: 28px;
  font-weight: 700;
}

.stat-value.success {
  color: #67c23a;
}

.stat-value.warn {
  color: #e6a23c;
}

.stat-sub {
  font-size: 13px;
  font-weight: 400;
  color: #909399;
}

.stat-label {
  margin-top: 6px;
  color: #909399;
  font-size: 13px;
}
</style>
