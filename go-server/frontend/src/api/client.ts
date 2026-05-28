// Tiny axios wrapper for the dashboard. All endpoints live under /api on
// the same origin as the page (Vite proxies /api → :8080 in dev, the Go
// binary serves both in production).
import axios from 'axios';

export const api = axios.create({
  baseURL: '/api',
  timeout: 30_000,
  headers: { 'Content-Type': 'application/json' },
});

export interface Account {
  id: number;
  phone: string;
  device_id: number;
  push_name: string;
  platform: string;
  status: string;
  running: boolean;
  proxy_id?: number;
  last_seen_at?: number;
  created_at: number;
  updated_at: number;
}

export interface Proxy {
  ID: number;
  Name: string;
  Scheme: string;
  Host: string;
  Port: number;
  Username: string;
  Password: string;
  Template: string;
  CreatedAt: string;
  UpdatedAt: string;
}

export const accountsApi = {
  list: async (): Promise<Account[]> => (await api.get('/accounts')).data,
  create: async (phone: string, proxyId?: number) =>
    (await api.post('/accounts', { phone, proxy_id: proxyId })).data,
  remove: async (id: number) => api.delete(`/accounts/${id}`),
  setProxy: async (id: number, proxyId: number | null) =>
    api.patch(`/accounts/${id}/proxy`, { proxy_id: proxyId }),
  start: async (id: number) => api.post(`/accounts/${id}/start`),
  stop: async (id: number) => api.post(`/accounts/${id}/stop`),
};

export const proxiesApi = {
  list: async (): Promise<Proxy[]> => (await api.get('/proxies')).data,
  create: async (body: Partial<Proxy> & { template?: string }) =>
    (await api.post('/proxies', body)).data,
  remove: async (id: number) => api.delete(`/proxies/${id}`),
};
