import { Alert, Card, Empty, Typography } from 'antd';

export function ChatPage() {
  return (
    <div className="zowsup-card">
      <Typography.Title level={4} style={{ marginTop: 0 }}>
        聊天
      </Typography.Title>
      <Alert
        type="info"
        showIcon
        message="M0 占位"
        description="文本 / 媒体 / 引用回复 / 编辑 / 撤回等命令将分别在 M2、M8 完成。"
        style={{ marginBottom: 24 }}
      />
      <Card bordered={false}>
        <Empty description="登录账号并完成首次同步后才会有会话列表" />
      </Card>
    </div>
  );
}
