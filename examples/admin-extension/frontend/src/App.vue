<template>
  <div class="admin-extension">
    <header class="header">
      <h1>🚀 Admin Extension Demo</h1>
      <p class="subtitle">Shopware Go SDK Example</p>
    </header>

    <!-- Replaces the dashboard rather than sitting above it: once the session is
         over the interceptor short-circuits on isSessionExpired(), so Retry and
         Refresh Data can only fail. -->
    <div v-if="sessionEnded" class="session-expired">
      <h2>🔒 Session ended</h2>
      <p>
        Reopen the module in the Administration to continue. Reloading this frame
        is not enough — Shopware has to sign a new URL.
      </p>
    </div>

    <template v-else>
      <div v-if="loading" class="loading">
        <div class="spinner"></div>
        <p>Loading dashboard...</p>
      </div>

      <div v-else-if="error" class="error">
        <h2>❌ Error</h2>
        <p>{{ error }}</p>
        <button @click="loadData" class="btn-retry">Retry</button>
      </div>

      <div v-else class="content">
        <!-- Shop Info Card -->
        <div class="card">
          <h2>🏪 Shop Information</h2>
          <div class="info-grid" v-if="shopInfo">
            <div class="info-item">
              <span class="label">Shop ID:</span>
              <span class="value">{{ shopInfo.shopId }}</span>
            </div>
            <div class="info-item">
              <span class="label">Shop URL:</span>
              <span class="value">{{ shopInfo.shopUrl }}</span>
            </div>
            <div class="info-item">
              <span class="label">Status:</span>
              <span class="badge" :class="shopInfo.status">{{ shopInfo.status }}</span>
            </div>
          </div>
        </div>

        <!-- Dashboard Stats -->
        <div class="stats-grid" v-if="stats">
          <div class="stat-card">
            <div class="stat-icon">📦</div>
            <div class="stat-value">{{ stats.totalOrders }}</div>
            <div class="stat-label">Total Orders</div>
          </div>

          <div class="stat-card">
            <div class="stat-icon">💰</div>
            <div class="stat-value">${{ stats.totalRevenue.toFixed(2) }}</div>
            <div class="stat-label">Total Revenue</div>
          </div>

          <div class="stat-card">
            <div class="stat-icon">🛍️</div>
            <div class="stat-value">{{ stats.activeProducts }}</div>
            <div class="stat-label">Active Products</div>
          </div>

          <div class="stat-card">
            <div class="stat-icon">🔄</div>
            <div class="stat-value">{{ formatTime(stats.lastSync) }}</div>
            <div class="stat-label">Last Sync</div>
          </div>
        </div>

        <!-- Actions -->
        <div class="card">
          <h2>⚡ Quick Actions</h2>
          <div class="actions">
            <button @click="refreshData" class="btn-action">
              🔄 Refresh Data
            </button>
            <button @click="openNotification" class="btn-action">
              🔔 Test Notification
            </button>
          </div>
        </div>
      </div>
    </template>
  </div>
</template>

<script setup lang="ts">
import { ref, onMounted, onUnmounted } from 'vue'
import { notification } from '@shopware-ag/meteor-admin-sdk'
import { api, type ShopInfo, type DashboardStats } from './services/api'
import {
  isSessionExpired,
  onSessionExpired,
  stopTokenRefresh,
  updateIframeHeight,
} from './services/shopware'

const loading = ref(true)
const error = ref<string | null>(null)
const shopInfo = ref<ShopInfo | null>(null)
const stats = ref<DashboardStats | null>(null)
const sessionEnded = ref(isSessionExpired())

let unsubscribeSessionExpired: (() => void) | null = null

onMounted(async () => {
  // The token is refreshed in the background, so the session can end without
  // the user doing anything. Surface it instead of failing silently.
  unsubscribeSessionExpired = onSessionExpired(() => {
    sessionEnded.value = true
  })

  await loadData()
  // Update iframe height after content loads
  setTimeout(() => updateIframeHeight(document.body.scrollHeight), 100)
})

onUnmounted(() => {
  unsubscribeSessionExpired?.()
  stopTokenRefresh()
})

async function loadData() {
  loading.value = true
  error.value = null

  try {
    const [shopData, statsData] = await Promise.all([
      api.getShopInfo(),
      api.getDashboardStats(),
    ])

    shopInfo.value = shopData
    stats.value = statsData
  } catch (err) {
    error.value = err instanceof Error ? err.message : 'Failed to load data'
    console.error('Error loading data:', err)
  } finally {
    loading.value = false
  }
}

async function refreshData() {
  await loadData()
  notification.dispatch({
    message: 'Data refreshed successfully! ✅',
    variant: 'success',
  })
}

function openNotification() {
  notification.dispatch({
    message: 'This is a test notification from the Admin Extension! 👋',
    variant: 'info',
  })
}

function formatTime(timestamp: number): string {
  const date = new Date(timestamp * 1000)
  const now = new Date()
  const diff = Math.floor((now.getTime() - date.getTime()) / 1000 / 60)

  if (diff < 1) return 'Just now'
  if (diff < 60) return `${diff}m ago`
  if (diff < 1440) return `${Math.floor(diff / 60)}h ago`
  return `${Math.floor(diff / 1440)}d ago`
}
</script>

<style scoped>
.admin-extension {
  font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
  padding: 24px;
  background: #f3f4f6;
  min-height: 100vh;
}

.header {
  margin-bottom: 32px;
}

.header h1 {
  font-size: 28px;
  font-weight: 700;
  color: #1f2937;
  margin: 0 0 8px 0;
}

.subtitle {
  color: #6b7280;
  font-size: 14px;
  margin: 0;
}

.loading {
  text-align: center;
  padding: 60px 20px;
}

.spinner {
  border: 4px solid #e5e7eb;
  border-top: 4px solid #3b82f6;
  border-radius: 50%;
  width: 48px;
  height: 48px;
  animation: spin 1s linear infinite;
  margin: 0 auto 16px;
}

@keyframes spin {
  0% { transform: rotate(0deg); }
  100% { transform: rotate(360deg); }
}

.session-expired {
  background: #fffbeb;
  border: 1px solid #fde68a;
  border-radius: 8px;
  padding: 20px 24px;
  margin-bottom: 20px;
}

.session-expired h2 {
  color: #92400e;
  margin: 0 0 8px 0;
}

.session-expired p {
  color: #78350f;
  margin: 0;
}

.error {
  background: #fef2f2;
  border: 1px solid #fecaca;
  border-radius: 8px;
  padding: 24px;
  text-align: center;
}

.error h2 {
  color: #dc2626;
  margin: 0 0 12px 0;
}

.error p {
  color: #991b1b;
  margin: 0 0 16px 0;
}

.btn-retry {
  background: #dc2626;
  color: white;
  border: none;
  padding: 10px 24px;
  border-radius: 6px;
  font-size: 14px;
  font-weight: 500;
  cursor: pointer;
  transition: background 0.2s;
}

.btn-retry:hover {
  background: #b91c1c;
}

.content {
  display: flex;
  flex-direction: column;
  gap: 24px;
}

.card {
  background: white;
  border-radius: 12px;
  padding: 24px;
  box-shadow: 0 1px 3px rgba(0, 0, 0, 0.1);
}

.card h2 {
  font-size: 18px;
  font-weight: 600;
  color: #1f2937;
  margin: 0 0 16px 0;
}

.info-grid {
  display: grid;
  gap: 16px;
}

.info-item {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding-bottom: 12px;
  border-bottom: 1px solid #e5e7eb;
}

.info-item:last-child {
  border-bottom: none;
  padding-bottom: 0;
}

.label {
  color: #6b7280;
  font-size: 14px;
  font-weight: 500;
}

.value {
  color: #1f2937;
  font-size: 14px;
  font-weight: 600;
  font-family: 'Monaco', 'Courier New', monospace;
}

.badge {
  padding: 4px 12px;
  border-radius: 12px;
  font-size: 12px;
  font-weight: 600;
  text-transform: uppercase;
}

.badge.active {
  background: #d1fae5;
  color: #065f46;
}

.stats-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
  gap: 16px;
}

.stat-card {
  background: white;
  border-radius: 12px;
  padding: 20px;
  text-align: center;
  box-shadow: 0 1px 3px rgba(0, 0, 0, 0.1);
  transition: transform 0.2s, box-shadow 0.2s;
}

.stat-card:hover {
  transform: translateY(-2px);
  box-shadow: 0 4px 6px rgba(0, 0, 0, 0.1);
}

.stat-icon {
  font-size: 32px;
  margin-bottom: 8px;
}

.stat-value {
  font-size: 24px;
  font-weight: 700;
  color: #1f2937;
  margin-bottom: 4px;
}

.stat-label {
  color: #6b7280;
  font-size: 13px;
  font-weight: 500;
}

.actions {
  display: flex;
  gap: 12px;
  flex-wrap: wrap;
}

.btn-action {
  background: #3b82f6;
  color: white;
  border: none;
  padding: 12px 24px;
  border-radius: 8px;
  font-size: 14px;
  font-weight: 500;
  cursor: pointer;
  transition: background 0.2s;
}

.btn-action:hover {
  background: #2563eb;
}
</style>

