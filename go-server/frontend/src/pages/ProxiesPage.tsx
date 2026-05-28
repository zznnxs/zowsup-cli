import { useEffect, useState } from 'react';
import { App as AntApp, Button, Form, Input, Modal, Select, Space, Table, Tag, Typography } from 'antd';

import { Proxy, proxiesApi } from '../api/client';

const SCHEME_OPTIONS = [
  { value: 'socks5', label: 'SOCKS5' },
  { value: 'http', label: 'HTTP' },
  { value: 'https', label: 'HTTPS' },
];

export function ProxiesPage() {
  const { message, modal } = AntApp.useApp();
  const [rows, setRows] = useState<Proxy[]>([]);
  const [loading, setLoading] = useState(false);
  const [creating, setCreating] = useState(false);
  const [form] = Form.useForm();

  const reload = async () => {
    setLoading(true);
    try {
      setRows((await proxiesApi.list()) ?? []);
    } catch (e) {
      message.error('加载代理列表失败');
      console.error(e);
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    void reload();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const onCreate = async () => {
    const values = await form.validateFields();
    await proxiesApi.create(values);
    message.success('代理已创建');
    setCreating(false);
    form.resetFields();
    await reload();
  };

  const onDelete = (id: number) =>
    modal.confirm({
      title: '删除代理？',
      content: '该代理被任何账号引用时也会被解除关联。',
      okText: '删除',
      okButtonProps: { danger: true },
      cancelText: '取消',
      onOk: async () => {
        await proxiesApi.remove(id);
        await reload();
      },
    });

  const columns = [
    {
      title: '名称',
      dataIndex: 'Name',
      key: 'Name',
      render: (text: string) => <Typography.Text strong>{text}</Typography.Text>,
    },
    { title: '协议', dataIndex: 'Scheme', key: 'Scheme', render: (s: string) => <Tag color="blue">{s}</Tag> },
    {
      title: '主机:端口',
      key: 'addr',
      render: (_: unknown, row: Proxy) => `${row.Host}:${row.Port}`,
    },
    {
      title: '凭据',
      key: 'auth',
      render: (_: unknown, row: Proxy) => (row.Username ? `${row.Username} / ••••` : '匿名'),
    },
    {
      title: '操作',
      key: 'ops',
      render: (_: unknown, row: Proxy) => (
        <Button size="small" danger onClick={() => onDelete(row.ID)}>
          删除
        </Button>
      ),
    },
  ];

  return (
    <div className="zowsup-card">
      <Space style={{ marginBottom: 16, justifyContent: 'space-between', width: '100%' }}>
        <Typography.Title level={4} style={{ margin: 0 }}>
          代理池
        </Typography.Title>
        <Button type="primary" onClick={() => setCreating(true)}>
          新增代理
        </Button>
      </Space>
      <Table rowKey="ID" loading={loading} dataSource={rows} columns={columns} pagination={false} />
      <div className="zowsup-empty-hint">
        支持 SOCKS5 / HTTP / HTTPS。`username` 和 `password` 字段允许 <code>{'{session_id}'}</code>、
        <code>{'{location}'}</code> 占位符,运行时按账号上下文展开。
      </div>
      <Modal
        title="新增代理"
        open={creating}
        onCancel={() => setCreating(false)}
        onOk={onCreate}
        okText="创建"
        cancelText="取消"
      >
        <Form form={form} layout="vertical" initialValues={{ scheme: 'socks5' }}>
          <Form.Item name="name" label="名称" rules={[{ required: true }]}>
            <Input placeholder="hk-1" />
          </Form.Item>
          <Form.Item name="scheme" label="协议" rules={[{ required: true }]}>
            <Select options={SCHEME_OPTIONS} />
          </Form.Item>
          <Form.Item name="host" label="主机" rules={[{ required: true }]}>
            <Input placeholder="proxy.example.com" />
          </Form.Item>
          <Form.Item name="port" label="端口" rules={[{ required: true }]}>
            <Input type="number" placeholder="1080" />
          </Form.Item>
          <Form.Item name="username" label="用户名（可空）">
            <Input placeholder="user-{session_id}" />
          </Form.Item>
          <Form.Item name="password" label="密码（可空）">
            <Input.Password placeholder="•••" />
          </Form.Item>
        </Form>
      </Modal>
    </div>
  );
}
