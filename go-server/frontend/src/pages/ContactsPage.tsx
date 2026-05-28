import { Alert, Card, Empty, Typography } from 'antd';

export function ContactsPage() {
  return (
    <div className="zowsup-card">
      <Typography.Title level={4} style={{ marginTop: 0 }}>
        联系人
      </Typography.Title>
      <Alert
        type="info"
        showIcon
        message="M0 占位"
        description="contact.list / contact.sync / contact.getprofile 等命令将在 M3 提供。"
        style={{ marginBottom: 24 }}
      />
      <Card bordered={false}>
        <Empty description="选择一个已登录账号后才能加载联系人" />
      </Card>
    </div>
  );
}
