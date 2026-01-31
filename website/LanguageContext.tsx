import React, { createContext, useContext, useState, useEffect, ReactNode } from 'react';

export type Language = 'en' | 'zh';

const translations = {
  en: {
    nav: {
      demo: "Demo",
      features: "Features",
      install: "Install",
      docs: "Docs"
    },
    hero: {
      version: "v0.1.4 Release",
      titleStart: "The Simplest",
      titleHighlight: "Human-in-the-Loop",
      titleEnd: "Service",
      subtitle: "Send an HTTP request. We notify you via your favorite channel. You click a button. The original request gets the response.",
      tags: "Self-hosted. Open Source. Minimalist.",
      getStarted: "Get Started",
      viewGithub: "View on GitHub",
      demo: {
        waiting: "Waiting for response...",
        notificationTitle: "Ask4Me Request",
        notificationBody: "Production Deploy Approval?",
        appTitle: "Deploy?",
        appBody: "Pipeline #824 is ready for production.",
        buttonApprove: "Approve (OK)",
        buttonReject: "Reject",
        submitted: "Submitted: OK",
        systemReady: "System Ready",
        sending: "Sending HTTP Request...",
        waitingUser: "Waiting for user...",
        notified: "User received notification",
        interacting: "User interacting...",
        responseDelivered: "Response delivered synchronously",
        phoneWaiting: "Waiting for notification..."
      }
    },
    features: {
      title: "Why Ask4Me?",
      subtitle: "Built for developers who need to pause their scripts, ask a question to a human, and continue execution based on the answer.",
      cards: [
        { title: "Just HTTP", desc: "Extremely minimalist. If you can make an HTTP request (curl, fetch, requests), you can use Ask4Me. No complex SDKs required." },
        { title: "Omni-Channel", desc: "Reach yourself anywhere. Built-in support for ServerChan and Apprise, connecting you to Telegram, Discord, Slack, and hundreds more." },
        { title: "Progressive Streams", desc: "Need real-time updates? Switch to SSE (Server-Sent Events) mode to receive the interaction URL and status updates instantly." },
        { title: "Resume & Reconnect", desc: "Network flaky? Pre-generate a Request ID. If your connection drops, reconnect with the same ID to retrieve the result seamlessly." },
        { title: "Fully Open Source", desc: "Licensed under MIT. Audit the code, host it on your own server, modify it to fit your needs. No vendor lock-in." },
        { title: "Zero Friction Setup", desc: "Install via NPM in seconds. We provide a CLI wrapper that downloads the correct binary for your OS automatically." }
      ]
    },
    install: {
      titleStart: "Up and running in",
      titleHighlight: "seconds",
      desc: "We provide a Node.js wrapper that handles the heavy lifting. It automatically fetches the correct Go binary for your platform (Linux, macOS, Windows) and helps you generate the configuration.",
      steps: [
        { title: "Install the Server", desc: "Global installation via NPM is recommended for easy access." },
        { title: "Configure & Run", desc: "Use the interactive setup tool to define your notification channels (ServerChan or Apprise)." },
        { title: "Send Request", desc: "Use curl, Python, or our JS SDK to trigger your first Human-in-the-Loop event." }
      ],
      cmdInstall: "Install via NPM",
      cmdRun: "Start the server (Interactive)",
      cmdTest: "Test with cURL"
    },
    footer: {
      madeWith: "Made with",
      by: "by EasyChen",
      rights: "Ask4Me. Released under the MIT License."
    }
  },
  zh: {
    nav: {
      demo: "演示",
      features: "特性",
      install: "安装",
      docs: "文档"
    },
    hero: {
      version: "v0.1.4 发布",
      titleStart: "极简的",
      titleHighlight: "Human-in-the-Loop",
      titleEnd: "服务",
      subtitle: "发送 HTTP 请求。我们通过你喜欢的渠道通知你。你点击按钮。原始请求同步获得响应。",
      tags: "自建服务 · 开源 · 极简主义",
      getStarted: "开始使用",
      viewGithub: "GitHub 源码",
      demo: {
        waiting: "等待响应...",
        notificationTitle: "Ask4Me 请求",
        notificationBody: "生产环境部署审批？",
        appTitle: "部署？",
        appBody: "流水线 #824 已准备好发布。",
        buttonApprove: "批准 (OK)",
        buttonReject: "拒绝",
        submitted: "已提交: OK",
        systemReady: "系统就绪",
        sending: "正在发送 HTTP 请求...",
        waitingUser: "等待用户操作...",
        notified: "用户收到通知",
        interacting: "用户交互中...",
        responseDelivered: "响应已同步送达",
        phoneWaiting: "等待通知..."
      }
    },
    features: {
      title: "为什么选择 Ask4Me?",
      subtitle: "专为开发者打造：暂停脚本，询问人类，根据答案继续执行。",
      cards: [
        { title: "纯 HTTP", desc: "极度极简。只要能发 HTTP 请求 (curl, fetch, requests) 就能用。无需复杂 SDK。" },
        { title: "全渠道支持", desc: "触达无处不在。内置支持 ServerChan 和 Apprise，连接 Telegram, Discord, Slack 等数百种渠道。" },
        { title: "渐进式流", desc: "需要实时更新？切换到 SSE (Server-Sent Events) 模式，即时接收交互链接和状态更新。" },
        { title: "断点重连", desc: "网络不稳定？预生成 Request ID。如果连接断开，使用相同 ID 重连即可无缝获取结果。" },
        { title: "完全开源", desc: "MIT 协议。代码可审计，支持私有化部署，按需修改。无厂商锁定。" },
        { title: "零门槛安装", desc: "NPM 秒级安装。我们提供 CLI 包装器，自动下载适合你系统的二进制文件。" }
      ]
    },
    install: {
      titleStart: "数秒内",
      titleHighlight: "启动运行",
      desc: "我们提供 Node.js 包装器处理繁重工作。它自动获取适合你平台 (Linux, macOS, Windows) 的 Go 二进制文件，并协助生成配置。",
      steps: [
        { title: "安装服务端", desc: "推荐使用 NPM 全局安装以便快速访问。" },
        { title: "配置与运行", desc: "使用交互式设置工具定义通知渠道 (ServerChan 或 Apprise)。" },
        { title: "发送请求", desc: "使用 curl, Python 或 JS SDK 触发你的第一个人机交互事件。" }
      ],
      cmdInstall: "通过 NPM 安装",
      cmdRun: "启动服务器 (交互式)",
      cmdTest: "使用 cURL 测试"
    },
    footer: {
      madeWith: "制作：",
      by: "EasyChen",
      rights: "Ask4Me. 基于 MIT 协议发布。"
    }
  }
};

interface LanguageContextType {
  language: Language;
  setLanguage: (lang: Language) => void;
  t: typeof translations.en;
}

const LanguageContext = createContext<LanguageContextType | undefined>(undefined);

export const LanguageProvider: React.FC<{ children: ReactNode }> = ({ children }) => {
  const [language, setLanguage] = useState<Language>('en');

  useEffect(() => {
    // Detect browser language
    if (typeof navigator !== 'undefined') {
      const browserLang = navigator.language.toLowerCase();
      if (browserLang.startsWith('zh')) {
        setLanguage('zh');
      }
    }
  }, []);

  const value = {
    language,
    setLanguage,
    t: translations[language]
  };

  return (
    <LanguageContext.Provider value={value}>
      {children}
    </LanguageContext.Provider>
  );
};

export const useLanguage = () => {
  const context = useContext(LanguageContext);
  if (!context) {
    throw new Error('useLanguage must be used within a LanguageProvider');
  }
  return context;
};