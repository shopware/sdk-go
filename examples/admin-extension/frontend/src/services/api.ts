import axios, { type AxiosInstance, type InternalAxiosRequestConfig } from 'axios'
import {
  currentAccessToken,
  getShopwareContext,
  isSessionExpired,
  refreshAccessToken,
} from './shopware'

// _sentToken records which token the request went out with, so a 401 can tell a
// stale token apart from one that is genuinely rejected. _swapped and _retried
// bound the retries: at most one retry with an already-refreshed token, then at
// most one refresh, so a failing refresh cannot start an endless
// 401 -> refresh -> 401 loop.
type RetriableConfig = InternalAxiosRequestConfig & {
  _sentToken?: string
  _swapped?: boolean
  _retried?: boolean
}

export interface ShopInfo {
  shopId: string
  shopUrl: string
  status: string
}

export interface DashboardStats {
  totalOrders: number
  totalRevenue: number
  activeProducts: number
  lastSync: number
}

class ApiService {
  private client: AxiosInstance

  constructor() {
    this.client = axios.create({
      baseURL: import.meta.env.VITE_API_URL || window.location.origin,
      timeout: 10000,
    })

    // Add auth token to all requests
    this.client.interceptors.request.use((config) => {
      try {
        const context = getShopwareContext()
        ;(config as RetriableConfig)._sentToken = context.accessToken
        config.headers.Authorization = `Bearer ${context.accessToken}`
      } catch (error) {
        console.error('Failed to get auth token:', error)
      }
      return config
    })

    // The token is normally renewed ahead of time by the proactive refresh in
    // shopware.ts. This is the fallback for the cases that beats: a throttled
    // background timer, a suspended machine, or a clock jump. In those cases a
    // proactive refresh may already have landed while the request was in flight,
    // so a stale token is retried before a new refresh is attempted.
    //
    // Retries go through this.client.request(), which re-runs the request
    // interceptor above and stamps the current token — neither branch has to set
    // the header itself.
    this.client.interceptors.response.use(
      (response) => response,
      async (error) => {
        const config = error.config as RetriableConfig | undefined

        if (error.response?.status !== 401 || !config || config._retried || isSessionExpired()) {
          return Promise.reject(error)
        }

        // A refresh that finished while this request was in flight already
        // produced a usable token. Rotating again would cost a round trip and
        // mint a token nobody asked for.
        const current = currentAccessToken()
        if (current && current !== config._sentToken && !config._swapped) {
          config._swapped = true
          return this.client.request(config)
        }

        config._retried = true

        try {
          await refreshAccessToken()
          return this.client.request(config)
        } catch (refreshError) {
          // The session is over; only reopening the module yields a new
          // signed URL, so surface it instead of retrying forever.
          console.error('Session expired, reload the module', refreshError)
          return Promise.reject(error)
        }
      }
    )
  }

  async getShopInfo(): Promise<ShopInfo> {
    const response = await this.client.get<ShopInfo>('/api/shop/info')
    return response.data
  }

  async getDashboardStats(): Promise<DashboardStats> {
    const response = await this.client.get<DashboardStats>('/api/dashboard/stats')
    return response.data
  }
}

export const api = new ApiService()

