import { Layout, Menu, Typography } from 'antd';
import { Route, Routes, useLocation, useNavigate } from 'react-router-dom';
import {
  ApiOutlined,
  ContactsOutlined,
  MessageOutlined,
  ThunderboltOutlined,
  UserOutlined,
} from '@ant-design/icons';

import { AccountsPage } from './pages/AccountsPage';
import { LoginPage } from './pages/LoginPage';
import { ProxiesPage } from './pages/ProxiesPage';
import { ChatPage } from './pages/ChatPage';
import { ContactsPage } from './pages/ContactsPage';
import { EventStreamIndicator } from './components/EventStreamIndicator';

const { Header, Sider, Content } = Layout;
const { Title } = Typography;

const NAV = [
  { key: '/', label: '账号', icon: <UserOutlined /> },
  { key: '/login', label: '登录', icon: <ThunderboltOutlined /> },
  { key: '/contacts', label: '联系人', icon: <ContactsOutlined /> },
  { key: '/chat', label: '聊天', icon: <MessageOutlined /> },
  { key: '/proxies', label: '代理', icon: <ApiOutlined /> },
];

export function App() {
  const navigate = useNavigate();
  const location = useLocation();
  const selectedKey = NAV.find((item) =>
    item.key === '/' ? location.pathname === '/' : location.pathname.startsWith(item.key),
  )?.key ?? '/';

  return (
    <Layout style={{ minHeight: '100vh' }}>
      <Sider breakpoint="lg" collapsedWidth="64" theme="dark" width={220}>
        <div style={{ padding: '18px 24px' }}>
          <Title level={4} style={{ color: '#25D366', margin: 0, letterSpacing: 1 }}>
            Zowsup-Go
          </Title>
          <div style={{ color: '#888', fontSize: 12, marginTop: 4 }}>纯 Go · M0</div>
        </div>
        <Menu
          mode="inline"
          theme="dark"
          selectedKeys={[selectedKey]}
          items={NAV.map((item) => ({
            key: item.key,
            icon: item.icon,
            label: item.label,
          }))}
          onClick={({ key }) => navigate(key)}
        />
      </Sider>
      <Layout>
        <Header
          style={{
            display: 'flex',
            justifyContent: 'space-between',
            alignItems: 'center',
            background: '#141414',
            paddingInline: 24,
            borderBottom: '1px solid #222',
          }}
        >
          <Title level={5} style={{ color: '#ddd', margin: 0 }}>
            {NAV.find((item) => item.key === selectedKey)?.label}
          </Title>
          <EventStreamIndicator />
        </Header>
        <Content style={{ padding: 24 }}>
          <Routes>
            <Route path="/" element={<AccountsPage />} />
            <Route path="/login" element={<LoginPage />} />
            <Route path="/contacts" element={<ContactsPage />} />
            <Route path="/chat" element={<ChatPage />} />
            <Route path="/proxies" element={<ProxiesPage />} />
          </Routes>
        </Content>
      </Layout>
    </Layout>
  );
}
