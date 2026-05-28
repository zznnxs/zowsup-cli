import { Alert, Card, Steps, Typography } from 'antd';

// LoginPage is a placeholder for M1's QR + pair-code flow. The shape of the
// final screen — phone input, QR canvas, pair-code display, progress steps
// — is sketched here so M1 only has to fill the live data in.
export function LoginPage() {
  return (
    <div className="zowsup-card">
      <Typography.Title level={4} style={{ marginTop: 0 }}>
        登录 / 配对
      </Typography.Title>
      <Alert
        type="info"
        showIcon
        message="M0 占位"
        description="QR 扫码与 pair code 登录将在 M1 实现。"
        style={{ marginBottom: 24 }}
      />
      <Card title="待实现：companion 配对流程" bordered={false}>
        <Steps
          direction="vertical"
          current={-1}
          items={[
            { title: '账号选择', description: '从账号列表中挑选一个未登录账号' },
            { title: 'Noise XX 握手 + 注册请求', description: '与 e1.whatsapp.net 协商密钥' },
            { title: '展示 QR / pair code', description: '前端绘制 QR 或显示配对码' },
            { title: '主设备扫描 / 输入', description: '主设备完成配对后服务器下发 success' },
            { title: '保存身份信息', description: 'identity / signed_prekey / device_signature 入库' },
          ]}
        />
      </Card>
    </div>
  );
}
