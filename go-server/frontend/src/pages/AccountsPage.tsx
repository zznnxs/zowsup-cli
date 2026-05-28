import { useEffect, useState } from 'react';
import {
  App as AntApp,
  Button,
  Form,
  Input,
  Modal,
  Select,
  Space,
  Table,
  Tag,
  Tooltip,
  Typography,
} from 'antd';
import { PlayCircleOutlined, PoweroffOutlined, ReloadOutlined } from '@ant-design/icons';

import { Account, Proxy, accountsApi, proxiesApi } from '../api/client';

const STATUS_COLOURS: Record<string, string> = {
  created: 'default',
  pairing: 'processing',
  registration: 'processing',
  connected: 'success',
  disconnected: 'warning',
  banned: 'error',
};

export function AccountsPage() {
  const { message } = AntApp.useApp();
  const [accounts, setAccounts] = useState<Account[]>([]);
  const [proxies, setProxies] = useState<Proxy[]>([]);
  const [creating, setCreating] = useState(false);
  const [loading, setLoading] = useState(false);
  const [form] = Form.useForm<{ phone: string; proxy_id?: number }>();

  const reload = async () => {
    setLoading(true);
    try {
      const [a, p] = await Promise.all([accountsApi.list(), proxiesApi.list()]);
      setAccounts(a ?? []);
      setProxies(p ?? []);
    } catch (e) {
      message.error('加载账号列表失败');
      console.error(e);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    void reload();
    const t = window.setInterval(reload, 5000);
    return () => window.clearInterval(t);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const onCreate = async () => {
    const values = await form.validateFields();
    await accountsApi.create(values.phone, values.proxy_id);
    message.success(`账号 ${values.phone} 已创建`);
    setCreating(false);
    form.resetFields();
    await reload();
  };

  const onStart = async (id: number) => {
    try {
      await accountsApi.start(id);
      message.success('启动指令已发送');
    } catch (e) {
      message.error('启动失败');
      console.error(e);
    }
  };

  const onStop = async (id: number) => {
    try {
      await accountsApi.stop(id);
      message.success('停止指令已发送');
    } catch (e) {
      message.error('停止失败');
      console.error(e);
    }
  };

  const onChangeProxy = async (id: number, proxyId: number | null) => {
    await accountsApi.setProxy(id, proxyId);
    await reload();
  };

  const columns = [
    {
      title: '手机号',
      dataIndex: 'phone',
      key: 'phone',
      render: (text: string, row: Account) => (
        <Space direction="vertical" size={0}>
          <Typography.Text strong>{text}</Typography.Text>
          <Typography.Text type="secondary" style={{ fontSize: 12 }}>
            #{row.id} · {row.platform}
          </Typography.Text>
        </Space>
      ),
    },
    {
      title: '状态',
      key: 'status',
      render: (_: unknown, row: Account) => (
        <Space>
          <Tag color={STATUS_COLOURS[row.status] ?? 'default'}>{row.status}</Tag>
          {row.running ? <Tag color="success">运行中</Tag> : <Tag>已停止</Tag>}
        </Space>
      ),
    },
    {
      title: '代理',
      key: 'proxy',
      render: (_: unknown, row: Account) => (
        <Select
          size="small"
          style={{ minWidth: 180 }}
          value={row.proxy_id ?? null}
          allowClear
          placeholder="直连"
          onChange={(v) => onChangeProxy(row.id, v ?? null)}
          options={proxies.map((p) => ({
            value: p.ID,
            label: `${p.Name} (${p.Scheme}://${p.Host}:${p.Port})`,
          }))}
        />
      ),
    },
    {
      title: '操作',
      key: 'ops',
      render: (_: unknown, row: Account) => (
        <Space>
          {row.running ? (
            <Tooltip title="停止">
              <Button
                danger
                icon={<PoweroffOutlined />}
                size="small"
                onClick={() => onStop(row.id)}
              >
                停止
              </Button>
            </Tooltip>
          ) : (
            <Tooltip title="启动">
              <Button
                type="primary"
                icon={<PlayCircleOutlined />}
                size="small"
                onClick={() => onStart(row.id)}
              >
                启动
              </Button>
            </Tooltip>
          )}
        </Space>
      ),
    },
  ];

  return (
    <div className="zowsup-card">
      <Space style={{ marginBottom: 16, justifyContent: 'space-between', width: '100%' }}>
        <Typography.Title level={4} style={{ margin: 0 }}>
          账号
        </Typography.Title>
        <Space>
          <Button icon={<ReloadOutlined />} onClick={reload} loading={loading}>
            刷新
          </Button>
          <Button type="primary" onClick={() => setCreating(true)}>
            新增账号
          </Button>
        </Space>
      </Space>
      <Table
        rowKey="id"
        loading={loading}
        dataSource={accounts}
        columns={columns}
        pagination={false}
      />
      <div className="zowsup-empty-hint">
        提示：M0 仅提供占位运行器（启动后会广播心跳事件）。真实的 WhatsApp 连接将在 M1 落地。
      </div>
      <Modal
        title="新增账号"
        open={creating}
        onCancel={() => setCreating(false)}
        onOk={onCreate}
        okText="创建"
        cancelText="取消"
      >
        <Form form={form} layout="vertical">
          <Form.Item
            name="phone"
            label="手机号（含国家区号，不含 +）"
            rules={[{ required: true }]}
          >
            <Input placeholder="628111000000" />
          </Form.Item>
          <Form.Item name="proxy_id" label="代理（可空表示直连）">
            <Select
              allowClear
              options={proxies.map((p) => ({
                value: p.ID,
                label: `${p.Name} (${p.Scheme}://${p.Host}:${p.Port})`,
              }))}
            />
          </Form.Item>
        </Form>
      </Modal>
    </div>
  );
}
